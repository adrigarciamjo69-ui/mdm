# Despliegue del servidor

El servidor se despliega con Docker Compose: PostgreSQL, el servidor MDM y Caddy, que publica el servicio por HTTPS y obtiene y renueva el certificado del dominio por su cuenta. Solo Caddy queda expuesto.

> ¿Usas Dokploy? Sigue [docs/dokploy.md](dokploy.md): Dokploy ya trae su proxy (Traefik) y no se usa el Caddy de este fichero.

## Requisitos

Para 100 a 1.000 equipos basta una máquina Linux con 2 vCPU, 4 GB de RAM y 20 GB de disco, con Docker Engine 24 o posterior y el plugin de Compose. Hace falta además un nombre de dominio (por ejemplo `mdm.empresa.es`) que apunte a la IP pública de la máquina, y los puertos 80 y 443 abiertos desde Internet: Caddy usa el 80 para validar el dominio con Let's Encrypt y los agentes se conectan por el 443.

Si el servidor solo debe ser accesible desde la red interna, Caddy puede usar un certificado de la autoridad interna de la empresa; en ese caso sustituye el bloque del `Caddyfile` por uno con la directiva `tls` apuntando al certificado y su clave, y distribuye la autoridad a los equipos (ver la guía del agente).

## Puesta en marcha

Desde la carpeta `deploy/` del repositorio:

```sh
cp .env.example .env
openssl rand -hex 24          # pega el resultado en POSTGRES_PASSWORD
openssl rand -base64 32       # pega el resultado en MDM_SECRET_KEY
nano .env                     # y pon tu dominio en MDM_DOMAIN
docker compose up -d --build
```

La primera vez tarda unos minutos en compilar. Comprueba que todo está en marcha con `docker compose ps` y que el servidor responde en `https://<tu dominio>/healthz`.

## Primer administrador

Las cuentas de administrador se crean desde la consola, pero la primera hay que crearla desde la línea de órdenes:

```sh
docker compose exec server /mdm-server admin create --email tu.correo@empresa.es
```

Te pedirá la contraseña dos veces (12 caracteres como mínimo). Al entrar por primera vez en la consola tendrás que configurar el segundo factor con una aplicación de autenticación; es obligatorio para todas las cuentas.

Las cuentas tienen uno de tres roles. Solo lectura consulta el parque y exporta listados. Operador, además, organiza los equipos en grupos y crea tokens de inscripción. Administrador, además, revoca y elimina equipos, gestiona las cuentas y consulta la auditoría.

## Actualización

```sh
git pull
cd deploy
docker compose up -d --build
```

Las migraciones de la base de datos se aplican solas al arrancar. Los agentes siguen funcionando durante la actualización: si no pueden contactar con el servidor, reintentan con esperas crecientes.

## Copias de seguridad

Todo el estado está en PostgreSQL; el servidor no guarda nada en disco. Una copia diaria basta:

```sh
docker compose exec -T db pg_dump -U mdm -Fc mdm > mdm-$(date +%F).dump
```

Para restaurarla en una instalación nueva, arranca solo la base de datos, restaura y arranca el resto:

```sh
docker compose up -d db
docker compose exec -T db pg_restore -U mdm -d mdm --clean --if-exists < mdm-2026-11-13.dump
docker compose up -d
```

Los instaladores que subas a la consola (catálogo de software) están en el volumen `files`, no en la base de datos. Cópialo también, por ejemplo con el servidor parado:

```sh
docker run --rm -v mdm_files:/datos -v "$PWD":/copia alpine tar czf /copia/mdm-files.tgz -C /datos .
```

El servidor no acepta subidas si el disco quedaría con menos de 1 GB libre, y borra los instaladores que nada usa a los 7 días de subirlos.

Guarda también el fichero `.env`. Sin la contraseña de PostgreSQL no podrás usar la copia con esta configuración, y sin `MDM_SECRET_KEY` los datos cifrados (claves de recuperación de BitLocker, credenciales de Google, contraseña del correo) no se pueden leer: el resto funciona, pero esos datos se pierden.

## Actualizar desde la fase 1

La fase 2 añade la variable obligatoria `MDM_SECRET_KEY`. Antes de actualizar, añádela al `.env` (`openssl rand -base64 32`). Sin ella, el servidor no arranca y explica por qué.

## Contraseñas

Cada persona cambia la suya desde la consola con **Cambiar mi contraseña** (bajo su correo, en el menú lateral). Le pide la actual y cierra sus demás sesiones.

Un administrador restablece la de otra cuenta en **Administradores › Restablecer contraseña**. Las sesiones abiertas de esa cuenta se cierran; hazle llegar la contraseña nueva de forma segura. Ambas acciones quedan en la auditoría.

Si nadie puede entrar en la consola, se cambia desde el servidor:

```sh
docker compose exec server /mdm-server admin reset-password --email persona@empresa.es
```

## Recuperar el acceso

Si un administrador pierde el teléfono con el segundo factor, otro administrador no puede restablecerlo desde la consola; se hace desde el servidor:

```sh
docker compose exec server /mdm-server admin reset-totp --email persona@empresa.es
```

Esto borra su segundo factor y cierra sus sesiones; al volver a entrar lo configurará de nuevo. Si se ha perdido el acceso de todos los administradores, crea uno nuevo con `admin create`.

## Registros

```sh
docker compose logs -f server
```

El servidor escribe en formato JSON. Cada error interno queda con la ruta y el motivo. Las acciones de los administradores y las inscripciones de equipos no van al registro sino a la auditoría, que se consulta en la consola.
