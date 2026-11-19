#!/bin/sh
# Instala el agente MDM en un equipo Linux y lo inscribe en el servidor.
#
#   curl -fsSL https://servidor/downloads/install.sh | sudo sh -s -- \
#       --server https://servidor --token <token>
#
# Opciones:
#   --server <url>    Dirección del servidor (https://…)            obligatoria
#   --token <token>   Token de inscripción creado en la consola     obligatoria
#   --cacert <fich>   Certificado de la autoridad, si el servidor usa una CA interna
set -eu

SERVER=""
TOKEN=""
CACERT=""

die() {
    echo "Error: $1" >&2
    exit 1
}

while [ $# -gt 0 ]; do
    case "$1" in
        --server) [ $# -ge 2 ] || die "falta el valor de --server"; SERVER="$2"; shift 2 ;;
        --token)  [ $# -ge 2 ] || die "falta el valor de --token";  TOKEN="$2";  shift 2 ;;
        --cacert) [ $# -ge 2 ] || die "falta el valor de --cacert"; CACERT="$2"; shift 2 ;;
        *) die "opción desconocida: $1" ;;
    esac
done

[ -n "$SERVER" ] || die "falta la dirección del servidor (--server https://…)"
[ -n "$TOKEN" ]  || die "falta el token de inscripción (--token …)"
case "$SERVER" in
    https://*) ;;
    *) die "la dirección del servidor debe empezar por https://" ;;
esac
SERVER="${SERVER%/}"

[ "$(id -u)" -eq 0 ] || die "hace falta ejecutar como administrador (root). Usa sudo."

case "$(uname -m)" in
    x86_64|amd64)  ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) die "Arquitectura no admitida: $(uname -m). El agente está disponible para amd64 y arm64." ;;
esac

command -v sha256sum >/dev/null 2>&1 || die "no se encuentra sha256sum, necesario para verificar la descarga"

NAME="mdm-agent-linux-$ARCH"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# download <url> <destino>
download() {
    if command -v curl >/dev/null 2>&1; then
        if [ -n "$CACERT" ]; then
            curl -fsSL --cacert "$CACERT" -o "$2" "$1"
        else
            curl -fsSL -o "$2" "$1"
        fi
    elif command -v wget >/dev/null 2>&1; then
        if [ -n "$CACERT" ]; then
            wget -q --ca-certificate="$CACERT" -O "$2" "$1"
        else
            wget -q -O "$2" "$1"
        fi
    else
        die "no se encuentra curl ni wget para descargar el agente"
    fi
}

echo "Descargando el agente ($ARCH) de $SERVER…"
download "$SERVER/downloads/$NAME" "$TMP/mdm-agent" ||
    die "No se ha podido descargar el agente de $SERVER. Comprueba la dirección, la red y el certificado."
download "$SERVER/downloads/SHA256SUMS" "$TMP/SHA256SUMS" ||
    die "No se ha podido descargar la lista de sumas de comprobación de $SERVER."

# La línea de SHA256SUMS tiene la forma «<suma>  <nombre>».
EXPECTED="$(awk -v name="$NAME" '$2 == name || $2 == "*" name { print $1; exit }' "$TMP/SHA256SUMS")"
[ -n "$EXPECTED" ] || die "el servidor no publica la suma de comprobación de $NAME"
ACTUAL="$(sha256sum "$TMP/mdm-agent" | awk '{ print $1 }')"
if [ "$EXPECTED" != "$ACTUAL" ]; then
    die "La suma de comprobación no coincide: la descarga está dañada o ha sido manipulada. No se ha instalado nada."
fi

chmod 0755 "$TMP/mdm-agent"

# enroll copia el ejecutable a su ubicación definitiva e instala el servicio;
# su código de salida es el de este script (2: token rechazado, 3: fallo de red).
set +e
"$TMP/mdm-agent" enroll --server "$SERVER" --token "$TOKEN"
CODE=$?
set -e
exit "$CODE"
