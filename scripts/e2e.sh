#!/bin/sh
# shellcheck disable=SC2016  # las órdenes entre comillas simples se evalúan dentro del contenedor, no aquí
# Prueba de extremo a extremo con Docker: levanta el despliegue completo,
# inscribe un equipo Ubuntu real con install.sh y comprueba por la API que
# aparece en línea con su inventario. Después lo revoca y comprueba que el
# agente lo detecta. Desde la fase 2 también comprueba una política aplicada,
# un script con parámetro y un grupo dinámico.
# Uso: sh scripts/e2e.sh   (desde la raíz del repositorio)
set -eu

cd "$(dirname "$0")/../deploy"
export MDM_DOMAIN=mdm.test
POSTGRES_PASSWORD="$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
export POSTGRES_PASSWORD
MDM_SECRET_KEY="$(head -c 32 /dev/urandom | base64)"
export MDM_SECRET_KEY
DC="docker compose -p mdm-e2e -f docker-compose.yml -f e2e/docker-compose.e2e.yml"
PASSWORD="prueba-e2e-$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')"
WORK="$(mktemp -d)"

cleanup() {
    if [ "${KEEP:-}" = "1" ]; then
        echo "Entorno conservado (KEEP=1). Para borrarlo: $DC down -v"
    else
        $DC down -v >/dev/null 2>&1 || true
    fi
    rm -rf "$WORK"
}
trap cleanup EXIT

step() { printf '\n== %s\n' "$1"; }
fail() { echo "FALLO: $1" >&2; exit 1; }

step "Construyendo y arrancando el despliegue"
$DC up -d --build --wait db server caddy agent

# Las peticiones a la API se hacen desde el contenedor del agente, que está en
# la misma red y resuelve mdm.test hacia Caddy.
in_agent() { $DC exec -T agent "$@"; }

step "Preparando el equipo de pruebas"
in_agent sh -c 'apt-get update -qq >/dev/null && apt-get install -y -qq curl ca-certificates python3 >/dev/null'
$DC cp caddy:/data/caddy/pki/authorities/local/root.crt "$WORK/ca.crt"
$DC cp "$WORK/ca.crt" agent:/usr/local/share/ca-certificates/mdm-e2e.crt
in_agent update-ca-certificates >/dev/null
# El contenedor no tiene systemd: systemctl se sustituye y el agente se arranca a mano.
in_agent sh -c 'printf "#!/bin/sh\nexit 0\n" > /usr/local/bin/systemctl && chmod +x /usr/local/bin/systemctl'

step "Creando el administrador"
echo "$PASSWORD" | $DC exec -T server /mdm-server admin create --email e2e@ejemplo.es >/dev/null

# Inicio de sesión con TOTP desde Python, dentro del contenedor del agente.
cat > "$WORK/api.py" << 'PY'
import base64, hashlib, hmac, json, struct, sys, time, urllib.request
B = "https://mdm.test/api/v1"
cookie, csrf = None, None
def call(method, path, body=None):
    global cookie
    h = {"Content-Type": "application/json"}
    if cookie: h["Cookie"] = cookie
    if csrf: h["X-CSRF-Token"] = csrf
    r = urllib.request.Request(B + path, method=method, headers=h, data=json.dumps(body).encode() if body is not None else None)
    with urllib.request.urlopen(r) as resp:
        c = resp.headers.get("Set-Cookie")
        if c and "mdm_session=" in c: cookie = c.split(";")[0]
        t = resp.read()
        return json.loads(t) if t else None
def code(secret):
    k = base64.b32decode(secret); m = struct.pack(">Q", int(time.time()) // 30)
    d = hmac.new(k, m, hashlib.sha1).digest(); o = d[-1] & 15
    return "%06d" % ((struct.unpack(">I", d[o:o+4])[0] & 0x7fffffff) % 1000000)
# El secreto TOTP se guarda la primera vez: en las siguientes llamadas el
# administrador ya tiene el segundo factor y solo hay que verificar el código.
SECRET_FILE = "/tmp/totp.secret"
nxt = call("POST", "/auth/login", {"email": "e2e@ejemplo.es", "password": sys.argv[1]})["next"]
if nxt == "totp_setup":
    secret = call("POST", "/auth/totp/setup")["secret"]
    open(SECRET_FILE, "w").write(secret)
else:
    secret = open(SECRET_FILE).read().strip()
csrf = call("POST", "/auth/totp/verify", {"code": code(secret)})["csrf_token"]
cmd = sys.argv[2]
if cmd == "token":
    print(call("POST", "/enrollment-tokens", {"name": "E2E", "max_uses": 5})["token"])
elif cmd == "device":
    items = call("GET", "/devices")["items"]
    if not items: print("ninguno"); sys.exit(0)
    d = items[0]
    sw = call("GET", "/devices/%s/software" % d["id"])["total"]
    print(d["id"], d["status"], sw)
elif cmd == "revoke":
    call("POST", "/devices/%s/revoke" % sys.argv[3])
elif cmd == "policy":
    # Un script propio en modo «aplicar» (crea un fichero marcador) y un ajuste
    # en modo «auditar»: prueba los dos caminos del motor de políticas.
    p = call("POST", "/policies", {"name": "E2E Linux", "platform": "linux", "settings": [
        {"key": "custom.script", "mode": "enforce", "params": {"interpreter": "sh",
            "detect": "test -f /var/tmp/mdm-e2e", "remediate": "touch /var/tmp/mdm-e2e"}},
        {"key": "linux.forbidden_packages", "mode": "audit", "params": {"names": ["telnetd"]}}]})
    call("PUT", "/policies/%s/assignments" % p["id"], {"assignments": [{"group_id": None}]})
    print(p["id"])
elif cmd == "compliance":
    print(call("GET", "/devices/%s/compliance" % sys.argv[3])["state"])
elif cmd == "script":
    s = call("POST", "/scripts", {"name": "E2E saludo", "platform": "linux", "interpreter": "sh",
        "parameters": [{"name": "SALUDO", "required": True}], "content": 'echo "hola $MDM_PARAM_SALUDO"'})
    b = call("POST", "/batches", {"type": "run_script", "device_ids": [sys.argv[3]],
        "params": {"script_id": s["id"], "params": {"SALUDO": "e2e; rm -rf /"}}})
    print(b["batch"]["id"])
elif cmd == "batch":
    c = call("GET", "/batches/%s" % sys.argv[3])["commands"][0]
    out = call("GET", "/commands/%s" % c["id"]).get("stdout", "")
    print(c["status"], out.strip().replace(" ", "_"))
elif cmd == "deploy_deb":
    # Subida multipart hecha a mano (sin dependencias).
    path = sys.argv[3]
    data = open(path, "rb").read()
    boundary = "----mdme2e" + str(int(time.time()))
    body = (("--%s\r\nContent-Disposition: form-data; name=\"file\"; filename=\"%s\"\r\n"
             "Content-Type: application/octet-stream\r\n\r\n") % (boundary, path.split("/")[-1])).encode() + data + ("\r\n--%s--\r\n" % boundary).encode()
    req = urllib.request.Request(B + "/files", data=body, method="POST",
        headers={"Content-Type": "multipart/form-data; boundary=" + boundary, "X-CSRF-Token": csrf, "Cookie": cookie})
    with urllib.request.urlopen(req) as resp:
        up = json.loads(resp.read())
    app = call("POST", "/apps", {"name": "mdm-e2e-demo", "platform": "linux", "source": "upload"})
    call("POST", "/apps/%s/versions" % app["id"], {"version": "1.0.0", "file_sha256": up["sha256"], "installer_type": "deb",
        "detection": {"type": "script", "interpreter": "sh", "script": "dpkg -s mdm-e2e-demo"}})
    d = call("POST", "/deployments", {"app_id": app["id"], "intent": "required", "reboot": "never"})
    call("PUT", "/deployments/%s/assignments" % d["id"], {"assignments": [{"group_id": None}]})
    print(d["id"])
elif cmd == "deploy_apt":
    app = call("POST", "/apps", {"name": sys.argv[3], "platform": "linux", "source": "apt", "package_id": sys.argv[3]})
    d = call("POST", "/deployments", {"app_id": app["id"], "intent": sys.argv[4], "reboot": "never"})
    call("PUT", "/deployments/%s/assignments" % d["id"], {"assignments": [{"group_id": None}]})
    print(d["id"])
elif cmd == "set_intent":
    call("PATCH", "/deployments/%s" % sys.argv[3], {"intent": sys.argv[4], "reboot": "never"})
elif cmd == "dep_state":
    devs = call("GET", "/deployments/%s/status" % sys.argv[3])["devices"]
    print(devs[0]["state"] if devs else "ninguno")
elif cmd == "scan":
    print(call("POST", "/batches", {"type": "scan_updates", "device_ids": [sys.argv[3]]})["batch"]["id"])
elif cmd == "scanned":
    print("yes" if call("GET", "/devices/%s/updates" % sys.argv[3])["state"]["last_scan_at"] else "no")
elif cmd == "dyngroup":
    g = call("POST", "/groups", {"name": "E2E Linux", "kind": "dynamic",
        "rule": {"match": "all", "conditions": [{"field": "platform", "op": "eq", "value": "linux"}]}})
    print(g["device_count"])
PY
$DC cp "$WORK/api.py" agent:/tmp/api.py
api() { in_agent python3 /tmp/api.py "$PASSWORD" "$@"; }

step "Instalando el agente con el comando de la consola"
TOKEN="$(api token)"
in_agent sh -c "curl -fsSL https://mdm.test/downloads/install.sh | sh -s -- --server https://mdm.test --token $TOKEN"
# Sin el retraso aleatorio de descarga (hasta 30 min), que una prueba no puede esperar.
in_agent sh -c 'MDM_AGENT_NO_JITTER=1 nohup /usr/local/bin/mdm-agent run >/var/log/mdm-agent.log 2>&1 &'

step "Esperando a que el equipo aparezca en línea (máximo 120 s)"
i=0
while :; do
    # shellcheck disable=SC2046  # se separa a propósito en id, estado y número de paquetes
    set -- $(api device)
    if [ "${2:-}" = "online" ] && [ "${3:-0}" -gt 0 ]; then
        echo "Equipo $1 en línea con $3 paquetes inventariados."
        DEVICE="$1"
        break
    fi
    i=$((i + 5))
    [ "$i" -ge 120 ] && fail "el equipo no aparece en línea con inventario: ${*:-ninguno}"
    sleep 5
done

step "Política: script propio en modo aplicar (máximo 180 s)"
api policy >/dev/null
i=0
until [ "$(api compliance "$DEVICE")" = "compliant" ]; do
    i=$((i + 5))
    [ "$i" -ge 180 ] && fail "el equipo no llega a cumplir la política: $(api compliance "$DEVICE")"
    sleep 5
done
in_agent test -f /var/tmp/mdm-e2e || fail "el agente no aplicó la corrección"
echo "El equipo cumple la política y el agente aplicó la corrección."

step "Script con parámetro (máximo 120 s)"
BATCH="$(api script "$DEVICE")"
i=0
while :; do
    # shellcheck disable=SC2046
    set -- $(api batch "$BATCH")
    [ "${1:-}" = "succeeded" ] && break
    i=$((i + 5))
    [ "$i" -ge 120 ] && fail "el script no termina: ${*:-}"
    sleep 5
done
[ "${2:-}" = "hola_e2e;_rm_-rf_/" ] || fail "salida inesperada del script: ${2:-}"
echo "El script se ejecutó y el parámetro llegó literal."

step "Grupo dinámico"
[ "$(api dyngroup)" = "1" ] || fail "el equipo no pertenece al grupo dinámico de Linux"
echo "El equipo es miembro del grupo dinámico."

step "Fase 3: instalador propio (.deb) subido y desplegado (máximo 300 s)"
in_agent sh -c 'mkdir -p /tmp/deb/mdm-e2e-demo/DEBIAN && printf "Package: mdm-e2e-demo\nVersion: 1.0.0\nArchitecture: all\nMaintainer: MDM <mdm@ejemplo.es>\nDescription: Paquete de prueba\n" > /tmp/deb/mdm-e2e-demo/DEBIAN/control && dpkg-deb --build /tmp/deb/mdm-e2e-demo /tmp/mdm-e2e-demo.deb >/dev/null'
DEP="$(api deploy_deb /tmp/mdm-e2e-demo.deb)"
i=0
until [ "$(api dep_state "$DEP")" = "installed" ]; do
    i=$((i + 5)); [ "$i" -ge 300 ] && fail "el paquete subido no se instala: $(api dep_state "$DEP")"
    sleep 5
done
in_agent dpkg -s mdm-e2e-demo >/dev/null || fail "dpkg no ve el paquete instalado"
echo "El instalador subido se descargó, se instaló y se detecta."

step "Fase 3: paquete de apt instalado y después desinstalado (máximo 300 s)"
APTDEP="$(api deploy_apt cowsay required)"
i=0
until [ "$(api dep_state "$APTDEP")" = "installed" ]; do
    i=$((i + 5)); [ "$i" -ge 300 ] && fail "cowsay no se instala: $(api dep_state "$APTDEP")"
    sleep 5
done
api set_intent "$APTDEP" uninstall
i=0
until [ "$(api dep_state "$APTDEP")" = "absent" ]; do
    i=$((i + 5)); [ "$i" -ge 300 ] && fail "cowsay no se desinstala: $(api dep_state "$APTDEP")"
    sleep 5
done
in_agent sh -c '! dpkg -s cowsay >/dev/null 2>&1' || fail "cowsay sigue instalado"
echo "Instalado y desinstalado desde apt."

step "Fase 3: búsqueda de actualizaciones (máximo 180 s)"
api scan "$DEVICE" >/dev/null
i=0
until [ "$(api scanned "$DEVICE")" = "yes" ]; do
    i=$((i + 5)); [ "$i" -ge 180 ] && fail "la búsqueda de actualizaciones no llega al servidor"
    sleep 5
done
echo "La búsqueda de actualizaciones llegó a la consola."

step "Revocando el equipo y reiniciando el agente"
api revoke "$DEVICE"
in_agent sh -c 'kill $(pidof mdm-agent); sleep 1; MDM_AGENT_NO_JITTER=1 nohup /usr/local/bin/mdm-agent run >>/var/log/mdm-agent.log 2>&1 & sleep 5'
in_agent /usr/local/bin/mdm-agent status | grep -q "Estado:           revocado" || fail "el agente no detecta la revocación"
echo "El agente informa «revocado»."

printf '\nPrueba de extremo a extremo correcta\n'
