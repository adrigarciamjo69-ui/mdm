# Despliegue con Dokploy

[Dokploy](https://dokploy.com) ya trae su propio proxy (Traefik), que ocupa los puertos 80 y 443 y obtiene los certificados. Por eso, con Dokploy no se usa `deploy/docker-compose.yml` (que trae Caddy), sino **`docker-compose.dokploy.yml`** (en la raíz del repositorio): la base de datos y el servidor, sin proxy.

## 1. Sube el código a un repositorio Git

Dokploy despliega desde un repositorio: GitHub, GitLab, Gitea, Bitbucket o cualquier Git accesible.

```sh
unzip mdm-completo.zip && cd mdm
git init && git add -A && git commit -m "MDM 2.0.0"
git remote add origin git@github.com:tu-empresa/mdm.git   # un repositorio PRIVADO
git push -u origin main
```

Si el servidor de Dokploy no tiene acceso a ese repositorio, configura antes el proveedor en **Settings › Git**.

## 2. Amplía el tiempo de lectura de Traefik (necesario para subir instaladores)

Traefik corta cualquier petición cuyo cuerpo tarde más de **60 segundos** en llegar, así que la subida de un instalador grande fallaría con un error 504. En Dokploy, ve a **Web Server › Traefik** (el editor de `traefik.yml`) y, en el punto de entrada `websecure`, añade:

```yaml
entryPoints:
  websecure:
    address: :443
    transport:
      respondingTimeouts:
        readTimeout: 30m
    # … (deja el resto como está: http, tls, etc.)
```

Guarda y reinicia Traefik desde la misma pantalla. Las descargas de los agentes no tienen este problema: el tiempo de escritura de Traefik no tiene límite por defecto.

Si después de reiniciar Traefik alguna aplicación responde 404, vuelve a desplegarla (**Deploy**): es un problema conocido de algunas versiones de Dokploy, que al recrear Traefik pierde la conexión con las redes de las aplicaciones Compose.

## 3. Crea la aplicación

1. En un proyecto, **Create Service › Compose**.
2. **Provider:** tu repositorio y la rama `main`.
3. **Compose Path:** `./docker-compose.dokploy.yml`
4. Guarda.

## 4. Variables de entorno

En la pestaña **Environment**:

```env
MDM_DOMAIN=mdm.empresa.es
POSTGRES_PASSWORD=<openssl rand -hex 24>
MDM_SECRET_KEY=<openssl rand -base64 32>
MDM_VERSION=2.0.0
# Redes desde las que se puede abrir la consola (separadas por comas).
MDM_ADMIN_RANGES=10.0.0.0/8
```

Genera los dos secretos en tu ordenador con las órdenes indicadas y pégalos.

**Guarda `MDM_SECRET_KEY` fuera de Dokploy** (en el gestor de contraseñas de la empresa). Si se pierde, las claves de recuperación de BitLocker, la clave de Google, la contraseña del correo y los secretos de los webhooks no se pueden descifrar.

## 5. Dominio y acceso

**No añadas el dominio en la pestaña «Domains».** El fichero Compose ya trae las reglas de Traefik, que usan `MDM_DOMAIN`, el certificado de Let's Encrypt y estas restricciones:

| Ruta | Quién llega | Para qué |
|---|---|---|
| `/agent/v1/…`, `/downloads/…`, `/healthz` | cualquiera (Internet incluido) | los agentes de los equipos y el instalador |
| todo lo demás (la consola y su API) | solo las IP de `MDM_ADMIN_RANGES` | administrar |

Si añadieras el dominio también en **Domains**, Dokploy crearía otra ruta sin la restricción y la consola quedaría abierta a Internet.

Desde fuera de esos rangos, la consola responde **403 Forbidden**: es lo esperado. Para administrar desde casa, conéctate primero a la VPN de la empresa (sus direcciones tienen que estar dentro de `MDM_ADMIN_RANGES`).

Los móviles Android no hablan con este servidor sino con Google, así que no les afecta. La vinculación con Google la hace el navegador de un administrador, que estará dentro de la red.

Si usas Cloudflare, deja el registro en «DNS only» (nube gris). Con el proxy de Cloudflare, el servidor vería la IP de Cloudflare: la consola respondería 403 a todo el mundo y la auditoría no tendría las IP reales.

## 6. Desplegar

Pulsa **Deploy**. La primera vez tarda unos minutos, porque compila la consola, el servidor y los cuatro agentes. Cuando termine, `https://mdm.empresa.es/healthz` debe responder `{"status":"ok"}`.

Si el despliegue falla con «Falta MDM_SECRET_KEY» (o «Falta MDM_DOMAIN», «Falta POSTGRES_PASSWORD»), las variables de **Environment** no han llegado a Compose. Comprueba que están guardadas, sin espacios alrededor del `=`, y vuelve a desplegar.

## 7. Primer administrador

En la pestaña **Docker** (o **Terminal**) de la aplicación, abre una terminal en el contenedor `server` y ejecuta:

```sh
/mdm-server admin create --email tu.correo@empresa.es
```

La imagen no tiene shell. Si la terminal de Dokploy no te deja ejecutar la orden directamente, hazlo desde el servidor por SSH:

```sh
docker ps --filter name=server --format '{{.Names}}'      # el nombre del contenedor
docker exec -it <contenedor> /mdm-server admin create --email tu.correo@empresa.es
```

Entra en `https://mdm.empresa.es`. La primera vez te pide configurar el segundo factor.

## 8. Copias de seguridad

- **Base de datos.** Dokploy puede hacer copias programadas de sus propias bases de datos, pero esta va dentro del Compose. Prográmala en **Schedules** de la aplicación (o con cron en el servidor):
  ```sh
  docker exec <contenedor-db> pg_dump -U mdm -Fc mdm > /copias/mdm-$(date +%F).dump
  ```
- **Volumen `files`** (instaladores subidos): en **Volume Backups**, si tu versión de Dokploy lo tiene, o copiándolo con `docker run --rm -v <proyecto>_files:/datos …` como en [despliegue](despliegue.md).
- **Las variables** de la pestaña **Environment**, sobre todo `MDM_SECRET_KEY`.

Comprueba de vez en cuando que una copia se puede restaurar.

## Actualizar

Sube la nueva versión al repositorio, cambia `MDM_VERSION` en **Environment** y pulsa **Deploy**. Las migraciones de la base de datos se aplican solas al arrancar. Los agentes se actualizan solos en su siguiente conexión, salvo los grupos congelados en **Ajustes**.

Si activas el despliegue automático al hacer push, ten en cuenta que cada push vuelve a compilar la imagen y reinicia el servidor. Los agentes lo toleran, pero es preferible desplegar en un momento tranquilo.

## Lo demás, igual

Inscribir equipos, Android, avisos, alertas y el resto funcionan exactamente igual: sigue el [README](../README.md) y las guías de `docs/`.

## Publicar el servidor a Internet con FortiGate

Ejemplo con el servidor de Dokploy en `10.20.200.148` y la IP pública `91.126.38.246`. Sustituye `mdm.tuempresa.es` por tu dominio.

### En el FortiGate

1. **Comprueba que el puerto 443 de la IP pública está libre.** En **System › Settings › Administration Settings**, el acceso HTTPS de administración usa 443 por defecto; en **VPN › SSL-VPN Settings**, la VPN SSL a veces también. Si alguno está en el 443 de la interfaz WAN, cámbialo antes (por ejemplo, la administración al 8443, o quita la administración de la WAN, que es lo recomendable). El puerto 80 debe estar libre igualmente.
2. **Policy & Objects › Virtual IPs › Create New › Virtual IP**, dos veces:

   | Campo | `mdm-https` | `mdm-http` |
   |---|---|---|
   | Interface | la WAN (p. ej. `wan1`) | la WAN |
   | External IP address | `91.126.38.246` | `91.126.38.246` |
   | Map to IPv4 address | `10.20.200.148` | `10.20.200.148` |
   | Port Forwarding | activado, TCP | activado, TCP |
   | External / Mapped port | `443` → `443` | `80` → `80` |

3. **Policy & Objects › Firewall Policy › Create New**:
   - **Incoming interface:** la WAN.
   - **Outgoing interface:** la interfaz o VLAN donde está `10.20.200.148`.
   - **Source:** `all` (o restringe por país con un objeto de geolocalización si las delegaciones están todas en España).
   - **Destination:** `mdm-https` y `mdm-http`.
   - **Service:** `HTTP`, `HTTPS`.
   - **Action:** `ACCEPT`.
   - **NAT: desactivado.** Es importante: así Traefik ve la IP real de cada equipo. Con NAT activado, todas las conexiones parecerían venir del FortiGate y, como su IP interna está dentro de `MDM_ADMIN_RANGES`, **la consola quedaría abierta a Internet**.
   - Opcional: activa el perfil **IPS** y registra el tráfico.
4. Guarda. No abras nada más hacia esa máquina: ni el panel de Dokploy (puerto 3000) ni SSH.

### En el DNS público

Donde se gestione el dominio en Internet (el registrador, Cloudflare…), crea el registro **A** `mdm.tuempresa.es` → `91.126.38.246`.

### En el DNS del Directorio Activo

Los equipos de la red interna (y los de las delegaciones conectados por VPN de sede) deben llegar **directamente** a la IP interna. Muchos FortiGate no devuelven hacia dentro el tráfico que entra por la IP pública, y además así la consola ve la IP interna del administrador. Crea en el DNS del Directorio Activo una zona solo para ese nombre (no para todo el dominio, para no tapar el resto de registros públicos). En un controlador de dominio, en PowerShell como administrador:

```powershell
Add-DnsServerPrimaryZone -Name "mdm.tuempresa.es" -ReplicationScope "Forest"
Add-DnsServerResourceRecordA -ZoneName "mdm.tuempresa.es" -Name "@" -IPv4Address "10.20.200.148"
```

Así, dentro de la red `mdm.tuempresa.es` resuelve a `10.20.200.148`, y fuera, a `91.126.38.246`.

### Comprobaciones

- **Desde fuera** (un móvil con datos, sin wifi de la empresa):
  - `https://mdm.tuempresa.es/healthz` → `{"status":"ok"}` con candado válido.
  - `https://mdm.tuempresa.es/` → **403 Forbidden**, porque la consola no se publica.
- **Desde dentro:**
  - `nslookup mdm.tuempresa.es` → `10.20.200.148`.
  - `https://mdm.tuempresa.es/` → la consola.
- **En un equipo de una delegación sin VPN:** instálale el agente con un token y comprueba que aparece en **Dispositivos**.
