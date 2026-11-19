# MDM propio — Fase 1: núcleo (diseño)

Fecha: 2026-11-12 · Estado: pendiente de revisión

## 1. Objetivo

Construir la base de una plataforma de gestión de dispositivos propia y autoalojada, para una empresa de entre 100 y 1.000 equipos. Esta fase entrega lo mínimo que hace falta para que todo lo demás se apoye en ella: equipos Windows y Linux inscritos, inventariados y visibles en una consola web, con administración segura y auditada.

Criterio de éxito: un administrador instala el agente en un equipo Windows o Linux con un solo comando, el equipo aparece en la consola en menos de dos minutos con su inventario completo, y su estado de conexión se mantiene al día sin intervención.

## 2. Alcance

### Dentro de la fase 1

- Servidor con API para la consola y API para los agentes.
- Agente para Windows (10/11, Server 2016+) y Linux (Debian/Ubuntu, RHEL y derivados), en amd64 y arm64.
- Inscripción con tokens, check-in periódico e inventario de hardware, sistema y software.
- Consola web: resumen, listado, ficha de dispositivo, grupos, tokens de inscripción, administradores y auditoría.
- Administradores con contraseña, segundo factor y roles.
- Despliegue con Docker Compose y scripts de instalación del agente.

### Fuera de la fase 1

| Queda para | Qué |
|---|---|
| Fase 2 | Políticas y perfiles, cumplimiento, grupos dinámicos, acciones remotas, scripts |
| Fase 3 | Catálogo de software, despliegues, parches del sistema |
| Fase 4 | Android (Android Management API) |
| Fase 5 | iOS y iPadOS (protocolo MDM de Apple) |
| Fase 6 | Alertas, informes, automatizaciones, paneles |

En esta fase el agente no ejecuta nada que le ordene el servidor. Solo informa.

## 3. Arquitectura

```
 Navegador ──HTTPS──┐
                    ▼
               ┌─────────┐      ┌────────────┐      ┌────────────┐
 Agentes ─────▶│  Caddy  │─────▶│ mdm-server │─────▶│ PostgreSQL │
 (solo salida, │  (TLS)  │      │    (Go)    │      │            │
  HTTPS 443)   └─────────┘      └────────────┘      └────────────┘
```

- **Caddy** termina TLS y renueva el certificado. Es el único servicio expuesto.
- **mdm-server** es un único binario de Go que sirve las dos API y los ficheros estáticos de la consola (incrustados con `go:embed`).
- **PostgreSQL 16** es el único almacén de estado. El servidor no guarda nada en disco, así que se puede reiniciar o sustituir sin perder datos.
- **mdm-agent** es un binario estático de Go por plataforma. Solo abre conexiones salientes.

Dimensionamiento: 1.000 agentes con check-in cada 60 s suponen unas 17 peticiones por segundo. Un servidor con 2 vCPU y 4 GB de RAM es suficiente.

### Tecnología

| Pieza | Elección |
|---|---|
| Servidor y agente | Go 1.23 o superior |
| Enrutador HTTP | `chi` |
| Acceso a PostgreSQL | `pgx` v5 |
| Migraciones | `goose`, incrustadas en el binario y aplicadas al arrancar |
| Contraseñas | Argon2id (`golang.org/x/crypto/argon2`) |
| TOTP | `pquerna/otp` |
| Servicio de Windows | `golang.org/x/sys/windows/svc` |
| Inventario en Windows | WMI (`yusufpapurcu/wmi`) y registro |
| Consola | React 18, TypeScript, Vite |

## 4. Componentes

### 4.1 Servidor

Paquetes internos, cada uno con una responsabilidad:

| Paquete | Responsabilidad | Depende de |
|---|---|---|
| `protocol` | Tipos JSON compartidos entre servidor y agente | — |
| `store` | Consultas a PostgreSQL; única pieza que conoce SQL | `pgx` |
| `auth` | Primitivas sin estado: contraseñas, TOTP, secretos aleatorios y su hash, roles, formato de la credencial de dispositivo | — |
| `httpx` | Respuestas JSON, formato de error, IP del cliente, límite de cuerpo | — |
| `audit` | Escritura y lectura del registro de auditoría | `store` |
| `agentapi` | Manejadores de `/agent/v1` | `auth`, `store`, `audit`, `protocol`, `httpx` |
| `adminapi` | Manejadores de `/api/v1`, sesiones y control de roles | `auth`, `store`, `audit`, `httpx` |
| `ratelimit` | Límite de intentos en memoria por clave | — |

Subcomandos del binario:

- `mdm-server serve` arranca el servicio.
- `mdm-server admin create --email <correo>` crea un administrador pidiendo la contraseña por terminal. Es la única forma de crear el primero.
- `mdm-server admin reset-totp --email <correo>` borra el segundo factor de un administrador que lo ha perdido.

Configuración por variables de entorno: `MDM_DATABASE_URL`, `MDM_LISTEN_ADDR` (por defecto `:8080`), `MDM_PUBLIC_URL`, `MDM_DOWNLOADS_DIR` (por defecto `/downloads`) y `MDM_TRUSTED_PROXY` (`true` cuando hay un proxy inverso delante; entonces la IP del cliente se toma del último valor de `X-Forwarded-For`). Sin esta última, todos los equipos compartirían la IP del proxy y el límite de inscripciones se aplicaría al conjunto.

### 4.2 Agente

| Paquete | Responsabilidad |
|---|---|
| `config` | Leer y escribir el fichero de configuración con permisos restringidos |
| `client` | Cliente HTTP hacia el servidor, con reintentos |
| `enroll` | Inscripción |
| `inventory` | Recogida de inventario; una implementación por plataforma tras una interfaz común |
| `loop` | Bucle de check-in e inventario |
| `service` | Integración con el gestor de servicios de Windows y con systemd |

Subcomandos:

- `mdm-agent enroll --server <url> --token <token>` inscribe el equipo, guarda la configuración e instala y arranca el servicio.
- `mdm-agent run` ejecuta el bucle (lo invoca el gestor de servicios).
- `mdm-agent status` muestra identificador, servidor, última conexión correcta y último error.
- `mdm-agent uninstall` detiene y elimina el servicio y borra la configuración.

Ubicaciones:

| | Windows | Linux |
|---|---|---|
| Binario | `C:\Program Files\MDMAgent\mdm-agent.exe` | `/usr/local/bin/mdm-agent` |
| Configuración | `C:\ProgramData\MDMAgent\agent.json` (solo SYSTEM y Administradores) | `/etc/mdm-agent/agent.json` (root, 0600) |
| Registro | `C:\ProgramData\MDMAgent\agent.log` | journald |
| Servicio | `MDMAgent`, cuenta LocalSystem, inicio automático | `mdm-agent.service`, root |

El registro de Windows rota al llegar a 5 MB y conserva 3 ficheros.

### 4.3 Consola

| Pantalla | Contenido |
|---|---|
| Inicio de sesión | Correo y contraseña, después código TOTP; alta de TOTP en el primer acceso |
| Resumen | Totales por estado y por plataforma, versiones de sistema, equipos con poco disco (algún volumen con menos del 10 % libre), sin cifrado o sin conexión desde hace 7 días o más, y las 10 últimas inscripciones |
| Dispositivos | Tabla paginada con búsqueda, filtros, ordenación y exportación a CSV |
| Ficha de dispositivo | Pestañas: general, hardware, almacenamiento, red, software, seguridad, actividad |
| Grupos | Alta, edición y borrado de grupos estáticos |
| Inscripción | Tokens con su estado y el comando de instalación listo para copiar |
| Administradores | Alta, cambio de rol, desactivación (solo rol administrador) |
| Auditoría | Registro filtrable por autor, acción y fechas (solo rol administrador) |

## 5. Modelo de datos

Todas las fechas son `timestamptz` en UTC. Los identificadores son UUID v4 salvo que se indique.

**admins**: `id`, `email` (único), `password_hash`, `totp_secret` (nulo hasta el alta), `role` (`admin`, `operator`, `readonly`), `disabled`, `created_at`, `last_login_at`.

**sessions**: `id` (32 bytes aleatorios, se guarda su SHA-256), `admin_id`, `csrf_token`, `verified` (falso hasta superar el TOTP: es la «sesión parcial»), `created_at`, `expires_at`, `ip`.

**groups**: `id`, `name` (único), `description`, `created_at`.

**enrollment_tokens**: `id`, `name`, `token_hash` (SHA-256), `group_id` (nulo o grupo al que entran los equipos), `expires_at`, `max_uses` (nulo = sin límite), `uses`, `revoked_at`, `created_by`, `created_at`.

**devices**: `id`, `hardware_id` (único), `hostname`, `platform` (`windows`, `linux`), `os_name`, `os_version`, `arch`, `serial`, `manufacturer`, `model`, `agent_version`, `credential_hash` (SHA-256), `revoked_at`, `group_id`, `assigned_user`, `tags` (`text[]`), `enrolled_at`, `last_seen_at`, `last_ip`, `inventory_hash`, `inventory_at`.

**device_inventory**: `device_id` (clave primaria), `collected_at`, `data` (`jsonb` con la estructura del apartado 7.3 menos el software).

**device_software**: `device_id`, `name`, `version`, `publisher`, `installed_at` (nulo si no se conoce). Índice por `device_id` y por `lower(name)`.

**device_activity**: `device_id`, `hour` (inicio de la hora), `checkins` (contador). Clave primaria `(device_id, hour)`. Se conserva 90 días. Alimenta la pestaña de actividad sin guardar cada check-in.

**commands**: `id`, `device_id`, `type`, `payload` (`jsonb`), `status`, `created_at`, `created_by`. Existe desde ahora para no migrar el protocolo en la fase 2; en la fase 1 nada escribe en ella.

**audit_log**: `id` (bigserial), `at`, `actor_admin_id` (nulo si el autor es el sistema), `actor_email`, `action`, `target_type`, `target_id`, `details` (`jsonb`), `ip`. Solo se inserta; no hay API para modificar ni borrar.

Borrar un dispositivo elimina en cascada su inventario, software, actividad y órdenes. Las entradas de auditoría se conservan.

### Estado del dispositivo

Se calcula al consultar, no se almacena:

| Estado | Condición |
|---|---|
| Revocado | `revoked_at` no es nulo |
| En línea | Última conexión hace menos de 3 intervalos de check-in (180 s por defecto) |
| Inactivo | Última conexión hace 30 días o más |
| Desconectado | El resto |

## 6. Seguridad

### 6.1 Administradores

- Contraseña de 12 caracteres como mínimo, guardada con Argon2id (64 MB de memoria, 3 iteraciones, paralelismo 2).
- TOTP obligatorio. Un administrador sin TOTP solo puede darlo de alta; no accede a nada más.
- Sesión en cookie `HttpOnly`, `Secure`, `SameSite=Strict`, con 12 horas de vida y guardada en servidor, de modo que cerrar sesión o desactivar al administrador la invalida al instante.
- Toda petición que modifica datos exige la cabecera `X-CSRF-Token` con el valor de la sesión.
- Inicio de sesión limitado a 5 intentos fallidos cada 15 minutos por pareja de IP y correo. El mensaje de error no distingue entre correo inexistente y contraseña incorrecta.

### 6.2 Roles

| Acción | Solo lectura | Operador | Administrador |
|---|:-:|:-:|:-:|
| Ver resumen, dispositivos y grupos | ✓ | ✓ | ✓ |
| Exportar CSV | ✓ | ✓ | ✓ |
| Editar grupo, usuario y etiquetas de un dispositivo | | ✓ | ✓ |
| Crear, editar y borrar grupos | | ✓ | ✓ |
| Ver, crear y revocar tokens de inscripción | | ✓ | ✓ |
| Revocar y borrar dispositivos | | | ✓ |
| Gestionar administradores | | | ✓ |
| Ver auditoría | | | ✓ |

Un administrador no puede desactivarse ni degradarse a sí mismo, y el sistema impide dejar la instalación sin ningún administrador activo.

### 6.3 Dispositivos

- **Token de inscripción:** 32 bytes aleatorios en base64url. Se muestra una sola vez al crearlo; el servidor guarda solo su SHA-256.
- **Credencial de dispositivo:** 32 bytes aleatorios generados por el servidor en la inscripción y devueltos una única vez. El servidor guarda su SHA-256 (al ser un secreto de alta entropía no necesita un hash lento). El agente la envía como `Authorization: Bearer <device_id>.<secreto>` y la comparación es en tiempo constante.
- **Revocación:** marca `revoked_at`. Desde ese momento toda petición del agente recibe 401.
- **Reinscripción:** una inscripción con un `hardware_id` ya conocido y un token válido reutiliza el registro (conserva grupo, usuario asignado y etiquetas), genera una credencial nueva, invalida la anterior, limpia `revoked_at` y deja constancia en auditoría. Si el nombre de equipo ha cambiado, la entrada lo señala: es la pista de dos equipos clonados que comparten identificador.
- **Transporte:** solo HTTPS. El agente valida el certificado con el almacén del sistema y rechaza URL que no sean `https://`.

### 6.4 Auditoría

Se registran: inicios de sesión correctos y fallidos, cierre de sesión, alta de TOTP, altas y cambios de administradores, creación y revocación de tokens, inscripciones y reinscripciones, ediciones, revocaciones y borrados de dispositivos, cambios en grupos y exportaciones a CSV.

### 6.5 Límites

| Punto | Límite |
|---|---|
| Inscripción | 10 intentos fallidos por minuto por IP. Las inscripciones correctas no cuentan: en un despliegue masivo, cientos de equipos de una misma oficina salen por la misma IP pública |
| Cuerpo del check-in | 16 KB |
| Inventario comprimido | 1 MB |
| Inventario descomprimido | 8 MB |
| Resto de la API de consola | 64 KB por petición |

## 7. API de agentes (`/agent/v1`)

Todo en JSON. Salvo la inscripción, todas las llamadas llevan la credencial del dispositivo.

### 7.1 `POST /enroll`

Petición:

```json
{
  "token": "…",
  "hardware_id": "…",
  "hostname": "PC-CONTA-07",
  "platform": "windows",
  "os_name": "Windows 11 Pro",
  "os_version": "10.0.26100",
  "arch": "amd64",
  "serial": "…",
  "manufacturer": "…",
  "model": "…",
  "agent_version": "1.0.0"
}
```

Respuesta `201`:

```json
{ "device_id": "…", "credential": "…", "checkin_interval_s": 60, "inventory_interval_s": 21600 }
```

Errores: `400` cuerpo inválido, `403` token inexistente, caducado, revocado o agotado (mismo mensaje en todos los casos), `429` demasiados intentos fallidos desde esa IP.

`hardware_id` es el UUID de SMBIOS. Si falta o es un valor genérico del fabricante, se usa `MachineGuid` del registro en Windows o `/etc/machine-id` en Linux.

### 7.2 `POST /checkin`

Petición:

```json
{ "agent_version": "1.0.0", "uptime_s": 86400, "inventory_hash": "sha256:…" }
```

Respuesta `200`:

```json
{
  "config": { "checkin_interval_s": 60, "inventory_interval_s": 21600 },
  "send_inventory": false,
  "commands": []
}
```

El servidor actualiza `last_seen_at`, `last_ip` y `agent_version`, incrementa el contador de la hora en `device_activity`, y responde `send_inventory: true` si la huella no coincide con la guardada o si el inventario tiene más de `inventory_interval_s`. En la fase 1 `commands` siempre va vacío y el agente ignora su contenido.

### 7.3 `POST /inventory`

Cuerpo JSON con `Content-Encoding: gzip`. Respuesta `204`.

```json
{
  "collected_at": "2026-11-12T10:00:00Z",
  "hash": "sha256:…",
  "unavailable": ["software"],
  "system": { "hostname": "", "os_name": "", "os_version": "", "kernel": "", "arch": "",
              "boot_time": "", "timezone": "", "domain": "", "logged_in_user": "" },
  "hardware": { "manufacturer": "", "model": "", "serial": "",
                "bios_vendor": "", "bios_version": "", "bios_date": "",
                "cpu_model": "", "cpu_cores": 0, "cpu_threads": 0, "memory_bytes": 0 },
  "disks": [ { "device": "", "mount": "", "filesystem": "",
               "size_bytes": 0, "free_bytes": 0, "encrypted": false } ],
  "network": [ { "name": "", "mac": "", "ipv4": [""], "ipv6": [""] } ],
  "security": { "disk_encryption": "on", "firewall": "on", "antivirus": "on",
                "antivirus_product": "", "secure_boot": "on" },
  "software": [ { "name": "", "version": "", "publisher": "", "installed_at": null } ]
}
```

Los campos de `security` admiten `on`, `off`, `partial` o `unknown`. `unavailable` lista las secciones que no se pudieron recoger; el servidor conserva el valor anterior de esas secciones en lugar de vaciarlas.

La huella es el SHA-256 del JSON canónico sin `collected_at`, `hash`, `boot_time` ni `free_bytes`, para que el paso del tiempo o el uso normal del disco no provoquen envíos continuos. El espacio libre se actualiza igualmente en el envío periódico de cada 6 h.

Antes de guardar, el servidor elimina los caracteres nulos y recorta cada cadena a 512 caracteres (el registro de Windows contiene a veces valores corruptos y PostgreSQL rechaza el carácter nulo). El servidor sustituye el inventario y la lista de software en una única transacción y actualiza en `devices` los campos que también aparecen en el listado (nombre, sistema, serie, fabricante, modelo).

### 7.4 Fuentes del inventario

| Dato | Windows | Linux |
|---|---|---|
| Sistema y hardware | WMI: `Win32_OperatingSystem`, `Win32_ComputerSystem`, `Win32_BIOS`, `Win32_Processor` | `/etc/os-release`, `/proc/cpuinfo`, `/proc/meminfo`, `/sys/class/dmi/id` |
| Discos | `Win32_LogicalDisk` | `/proc/mounts` y `statfs`; cifrado con `lsblk` (tipo `crypt`) |
| Red | Paquete `net` de Go | Paquete `net` de Go |
| Software | Claves `Uninstall` del registro (64 y 32 bits) | `dpkg-query` o `rpm -qa` |
| Cifrado | `Win32_EncryptableVolume` (BitLocker) | LUKS vía `lsblk` |
| Cortafuegos | Perfiles en el registro | Estado de `ufw`, `firewalld` o reglas de `nftables` |
| Antivirus | `root\SecurityCenter2` (no existe en Windows Server: `unknown`) | `unknown` |
| Arranque seguro | Registro `SecureBoot\State` | Variable EFI `SecureBoot` |

## 8. API de consola (`/api/v1`)

| Método y ruta | Rol mínimo | Descripción |
|---|---|---|
| `POST /auth/login` | — | Correo y contraseña; devuelve si falta TOTP o su alta |
| `POST /auth/totp/setup` | sesión parcial | Genera secreto y URI para el código QR |
| `POST /auth/totp/verify` | sesión parcial | Valida el código y completa la sesión |
| `POST /auth/logout` | cualquiera | Cierra la sesión |
| `GET /auth/me` | cualquiera | Administrador actual, rol y token CSRF |
| `GET /summary` | lectura | Datos de la pantalla de resumen |
| `GET /devices` | lectura | Listado paginado |
| `GET /devices.csv` | lectura | Exportación con los mismos filtros; las celdas que empiezan por `=`, `+`, `-` o `@` se anteponen con un apóstrofo para que una hoja de cálculo no las interprete como fórmula |
| `GET /devices/{id}` | lectura | Ficha con inventario |
| `GET /devices/{id}/software` | lectura | Software, paginado y con búsqueda |
| `GET /devices/{id}/activity` | lectura | Check-ins por hora de los últimos 90 días |
| `PATCH /devices/{id}` | operador | Grupo, usuario asignado, etiquetas |
| `POST /devices/{id}/revoke` | administrador | Revoca la credencial |
| `DELETE /devices/{id}` | administrador | Borra el dispositivo |
| `GET /groups` · `POST /groups` | lectura · operador | Listar y crear |
| `PATCH /groups/{id}` · `DELETE /groups/{id}` | operador | Editar y borrar |
| `GET /enrollment-tokens` · `POST /enrollment-tokens` | operador | Listar y crear |
| `POST /enrollment-tokens/{id}/revoke` | operador | Revocar |
| `GET /admins` · `POST /admins` · `PATCH /admins/{id}` | administrador | Gestionar administradores |
| `GET /audit` | administrador | Registro paginado y filtrable |

Parámetros de `GET /devices`: `page` (desde 1), `per_page` (25 por defecto, 200 como máximo), `q` (busca en nombre, serie y usuario asignado), `platform`, `status`, `group_id`, `os_version`, `tag`, `sort` (`hostname`, `last_seen_at`, `enrolled_at`, `os_version`) y `order` (`asc`, `desc`).

Formato de error común: `{ "error": { "code": "…", "message": "…" } }`, con el mensaje en español y listo para mostrarse.

Borrar un grupo deja sin grupo a sus dispositivos y a los tokens que apuntaban a él; no borra equipos.

## 9. Comportamiento ante fallos

| Situación | Comportamiento |
|---|---|
| Servidor inaccesible o respuesta 5xx | El agente reintenta con espera exponencial (5 s, 10 s, 20 s… hasta 15 min) con variación aleatoria de ±20 %. Tras un éxito vuelve al intervalo normal. Nunca se detiene. |
| Respuesta 401 | El agente pasa a estado «revocado», deja de enviar datos y lo anota en su registro. El servicio sigue vivo pero inactivo hasta un nuevo `mdm-agent enroll`. |
| Respuesta 429 | El agente respeta `Retry-After`. |
| Token de inscripción rechazado | `enroll` termina con código 2 y un mensaje claro. No instala el servicio ni deja configuración. |
| Fallo de red en la inscripción | `enroll` termina con código 3. Se puede repetir sin efectos secundarios. |
| Falla una fuente de inventario | Se envía el resto y la sección figura en `unavailable`. |
| Inventario demasiado grande | El servidor responde 413; el agente lo anota y no reintenta hasta el siguiente ciclo. |
| PostgreSQL caído | El servidor responde 503 a todo. Los agentes lo tratan como servidor inaccesible; la consola muestra un aviso y reintenta. |
| Reloj del equipo incorrecto | No afecta: el servidor usa su propia hora para `last_seen_at`; `collected_at` es solo informativo. |
| Sesión caducada en la consola | La API responde 401 y la consola lleva al inicio de sesión conservando la ruta para volver. |

Todos los check-ins de un mismo intervalo se reparten con una variación aleatoria de ±10 % para que 1.000 agentes no coincidan en el mismo segundo tras un reinicio del servidor.

## 10. Pruebas

- **Unitarias (servidor):** hash y verificación de contraseñas, TOTP, tokens de inscripción (caducidad, usos, revocación), credenciales de dispositivo, matriz de roles, cálculo de estado, límite de intentos.
- **Unitarias (agente):** análisis de las fuentes de inventario con datos de ejemplo de cada plataforma, cálculo de la huella (estable ante cambios en espacio libre y hora de arranque), espera exponencial, lectura y escritura de configuración.
- **Integración:** contra un PostgreSQL real, con el servidor arrancado en el proceso de prueba y el cliente del agente real. Casos: inscripción, check-in, inventario, inventario parcial, revocación (401), reinscripción sin duplicar, token agotado, permisos por rol en cada ruta, CSRF, auditoría de cada acción.
- **Carga:** simulador de 1.000 agentes durante 10 minutos. Se acepta si el percentil 99 del check-in queda por debajo de 200 ms y no hay errores.
- **Extremo a extremo en Linux:** instalación real del agente como servicio systemd contra el despliegue de Docker Compose.
- **Windows:** el código se compila y sus pruebas unitarias se ejecutan en la integración continua. La instalación como servicio y el inventario por WMI se validan a mano en un equipo Windows real siguiendo una lista de comprobación incluida en la documentación.
- **Consola:** pruebas de componentes para la tabla, los filtros y el flujo de inicio de sesión.

## 11. Entregables y estructura

```
cmd/
  mdm-server/           punto de entrada del servidor
  mdm-agent/            punto de entrada del agente
internal/
  protocol/             tipos compartidos
  server/
    store/  auth/  audit/  agentapi/  adminapi/  ratelimit/
  agent/
    config/  client/  enroll/  inventory/  loop/  service/
migrations/             SQL de goose
web/                    consola (React + TypeScript + Vite)
deploy/
  docker-compose.yml  Caddyfile  .env.example
scripts/
  install.ps1           instalación del agente en Windows
  install.sh            instalación del agente en Linux
  loadtest/             simulador de agentes
docs/
  despliegue.md         puesta en marcha y primer administrador
  agente.md             instalación, estado y desinstalación
  validacion-windows.md lista de comprobación manual
  superpowers/specs/    este documento
```

Los scripts de instalación descargan el binario desde el propio servidor (`/downloads/mdm-agent-<plataforma>-<arquitectura>`), comprueban su SHA-256 contra el que publica el servidor y ejecutan `mdm-agent enroll`.

## 12. Decisiones tomadas y descartadas

- **Credencial por equipo en lugar de certificados de cliente (mTLS).** Es más simple de desplegar y de depurar y permite un proxy inverso estándar. Se planteó mTLS y se descartó para esta fase.
- **Sondeo periódico en lugar de conexión permanente.** Basta para inventario y estado. Si la fase 2 necesita acciones inmediatas, se añadirá un canal persistente sin cambiar el check-in.
- **Agente propio en lugar de osquery o Fleet.** Se prioriza el control completo del producto y un único lenguaje.
- **Estado calculado en lugar de almacenado.** Evita un proceso que marque equipos como desconectados y no puede quedar desfasado.
- **Actividad agregada por hora.** Guardar cada check-in supondría más de un millón de filas al día con 1.000 equipos.
