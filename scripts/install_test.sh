#!/bin/sh
# shellcheck disable=SC2015  # pass siempre devuelve 0, así que «A && pass || fail» es seguro aquí
# Pruebas de scripts/install.sh. Levanta un servidor HTTPS local con un
# certificado propio y un «agente» sustituto que anota sus argumentos.
# Uso: sh scripts/install_test.sh   (como root, igual que el instalador)
set -u

SCRIPT="$(cd "$(dirname "$0")" && pwd)/install.sh"
WORK="$(mktemp -d)"
FAILS=0
SERVER_PID=""

cleanup() {
    [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null
    rm -rf "$WORK"
}
trap cleanup EXIT

fail() { echo "FALLO: $1"; FAILS=$((FAILS + 1)); }
pass() { echo "ok: $1"; }

if [ ! -f "$SCRIPT" ]; then
    echo "FALLO: no existe $SCRIPT"
    exit 1
fi

mkdir -p "$WORK/www/downloads" "$WORK/tmp" "$WORK/fakebin"
PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')
URL="https://localhost:$PORT"

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=localhost" \
    -addext "subjectAltName=DNS:localhost" \
    -keyout "$WORK/key.pem" -out "$WORK/cert.pem" >/dev/null 2>&1

# Agente sustituto: anota sus argumentos y sale con el código pedido.
cat > "$WORK/www/downloads/mdm-agent-linux-amd64" << 'STUB'
#!/bin/sh
echo "$@" > "$MDM_TEST_ARGS_FILE"
exit "${MDM_TEST_EXIT:-0}"
STUB
cp "$WORK/www/downloads/mdm-agent-linux-amd64" "$WORK/www/downloads/mdm-agent-linux-arm64"
(cd "$WORK/www/downloads" && sha256sum mdm-agent-linux-amd64 mdm-agent-linux-arm64 > SHA256SUMS)

cat > "$WORK/serve.py" << 'PY'
import http.server, ssl, sys, os
os.chdir(sys.argv[1])
srv = http.server.HTTPServer(("127.0.0.1", int(sys.argv[2])), http.server.SimpleHTTPRequestHandler)
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain(sys.argv[3], sys.argv[4])
srv.socket = ctx.wrap_socket(srv.socket, server_side=True)
srv.serve_forever()
PY
python3 "$WORK/serve.py" "$WORK/www" "$PORT" "$WORK/cert.pem" "$WORK/key.pem" >/dev/null 2>&1 &
SERVER_PID=$!
i=0
while [ $i -lt 50 ] && ! curl -s --cacert "$WORK/cert.pem" "$URL/downloads/SHA256SUMS" >/dev/null 2>&1; do
    sleep 0.1; i=$((i + 1))
done

export MDM_TEST_ARGS_FILE="$WORK/args"
run_install() { TMPDIR="$WORK/tmp" sh "$SCRIPT" "$@" > "$WORK/out" 2>&1; }

# 1. Suma correcta: se invoca el agente con enroll, servidor y token.
rm -f "$WORK/args"
run_install --server "$URL" --token tok-123 --cacert "$WORK/cert.pem"; code=$?
if [ "$code" -eq 0 ] && [ "$(cat "$WORK/args" 2>/dev/null)" = "enroll --server $URL --token tok-123" ]; then
    pass "suma correcta: invoca enroll con servidor y token"
else
    fail "suma correcta: código $code, argumentos «$(cat "$WORK/args" 2>/dev/null)», salida: $(cat "$WORK/out")"
fi
[ -z "$(ls -A "$WORK/tmp")" ] && pass "no deja ficheros temporales" || fail "quedan temporales: $(ls -A "$WORK/tmp")"

# 2. El código de salida de enroll se propaga.
MDM_TEST_EXIT=2 run_install --server "$URL" --token tok-123 --cacert "$WORK/cert.pem"; code=$?
[ "$code" -eq 2 ] && pass "propaga el código de salida de enroll" || fail "código de salida $code, se esperaba 2"

# 3. Suma incorrecta: no se ejecuta nada y la descarga se borra.
rm -f "$WORK/args"
echo "# manipulado" >> "$WORK/www/downloads/mdm-agent-linux-amd64"
run_install --server "$URL" --token tok-123 --cacert "$WORK/cert.pem"; code=$?
if [ "$code" -ne 0 ] && grep -q "La suma de comprobación no coincide" "$WORK/out" && [ ! -f "$WORK/args" ]; then
    pass "suma incorrecta: se niega a ejecutar"
else
    fail "suma incorrecta: código $code, ejecutado=$([ -f "$WORK/args" ] && echo sí || echo no), salida: $(cat "$WORK/out")"
fi
[ -z "$(ls -A "$WORK/tmp")" ] && pass "suma incorrecta: la descarga se borra" || fail "la descarga manipulada sigue en disco"
(cd "$WORK/www/downloads" && sha256sum mdm-agent-linux-amd64 mdm-agent-linux-arm64 > SHA256SUMS)

# 4. El binario no figura en SHA256SUMS.
grep -v amd64 "$WORK/www/downloads/SHA256SUMS" > "$WORK/sums" && cp "$WORK/sums" "$WORK/www/downloads/SHA256SUMS"
rm -f "$WORK/args"
run_install --server "$URL" --token tok-123 --cacert "$WORK/cert.pem"; code=$?
[ "$code" -ne 0 ] && [ ! -f "$WORK/args" ] && pass "sin suma publicada: se niega a ejecutar" || fail "sin suma publicada: código $code"
(cd "$WORK/www/downloads" && sha256sum mdm-agent-linux-amd64 mdm-agent-linux-arm64 > SHA256SUMS)

# 5. Arquitectura no admitida.
printf '#!/bin/sh\necho mips\n' > "$WORK/fakebin/uname"; chmod +x "$WORK/fakebin/uname"
PATH="$WORK/fakebin:$PATH" run_install --server "$URL" --token tok-123 --cacert "$WORK/cert.pem"; code=$?
[ "$code" -ne 0 ] && grep -q "Arquitectura no admitida: mips" "$WORK/out" && pass "arquitectura desconocida: mensaje claro" || fail "arquitectura: código $code, salida: $(cat "$WORK/out")"

# 6. aarch64 descarga el binario arm64.
printf '#!/bin/sh\necho aarch64\n' > "$WORK/fakebin/uname"
rm -f "$WORK/args"
PATH="$WORK/fakebin:$PATH" run_install --server "$URL" --token tok-123 --cacert "$WORK/cert.pem"; code=$?
[ "$code" -eq 0 ] && [ -f "$WORK/args" ] && pass "aarch64 usa el binario arm64" || fail "aarch64: código $code, salida: $(cat "$WORK/out")"

# 7. Faltan argumentos.
run_install --server "$URL"; code=$?
[ "$code" -ne 0 ] && grep -q -- "--token" "$WORK/out" && pass "sin --token: explica qué falta" || fail "sin token: código $code, salida: $(cat "$WORK/out")"
run_install --token x; code=$?
[ "$code" -ne 0 ] && grep -q -- "--server" "$WORK/out" && pass "sin --server: explica qué falta" || fail "sin servidor: código $code"

# 8. Servidor sin https.
run_install --server "http://localhost:$PORT" --token x; code=$?
[ "$code" -ne 0 ] && grep -q "https://" "$WORK/out" && pass "rechaza un servidor sin https" || fail "http: código $code, salida: $(cat "$WORK/out")"

# 9. Servidor inaccesible.
run_install --server "https://localhost:1" --token x --cacert "$WORK/cert.pem"; code=$?
[ "$code" -ne 0 ] && grep -q "No se ha podido descargar" "$WORK/out" && pass "servidor inaccesible: mensaje claro" || fail "inaccesible: código $code, salida: $(cat "$WORK/out")"

echo
if [ "$FAILS" -eq 0 ]; then echo "Todas las pruebas del instalador pasan."; else echo "$FAILS prueba(s) fallida(s)."; exit 1; fi
