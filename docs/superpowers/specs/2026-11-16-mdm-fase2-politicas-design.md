# MDM — Fase 2: políticas, cumplimiento, grupos dinámicos, acciones y scripts (diseño)

Fecha: 2026-11-16 · Estado: pendiente de revisión · Depende de: fase 1 (núcleo)

## 1. Objetivo

Que la plataforma pueda **actuar** sobre los equipos además de inventariarlos: mantener configuraciones de seguridad (estado deseado), saber qué equipos cumplen, agruparlos por reglas, enviarles órdenes puntuales y ejecutar scripts. Incluye la infraestructura que usarán las fases 3, 4 y 6.

Criterios de éxito:

- Un administrador crea una política «BitLocker obligatorio + cortafuegos», la asigna a un grupo dinámico «Portátiles Windows» y, en menos de 20 minutos, cada equipo del grupo la aplica, informa de su cumplimiento y custodia su clave de recuperación en el servidor.
- Un administrador ejecuta un script en 300 equipos y ve el progreso y la salida de cada uno; ningún equipo lo ejecuta dos veces.
- Al publicar una versión nueva del servidor, los agentes se actualizan solos y un agente defectuoso no llega a sustituir al anterior.

## 2. Alcance

Dentro: infraestructura común (cola de tareas, cifrado de secretos, ajustes globales), actualización automática del agente, políticas con catálogo de ajustes y scripts personalizados, cumplimiento, grupos dinámicos, órdenes y lotes, biblioteca de scripts, custodia de claves BitLocker, consola para todo ello.

Fuera: ejecuciones programadas (fase 6), software y parches (fase 3), Android (fase 4), control en vivo (descartado), canal persistente (descartado: las órdenes llegan en el siguiente check-in, hasta 60 s).

## 3. Infraestructura común

### 3.1 Secretos del servidor

Nueva variable obligatoria `MDM_SECRET_KEY`: 32 bytes aleatorios en base64 (`openssl rand -base64 32`). El servidor no arranca sin ella y lo explica en el mensaje de error.

Paquete `internal/server/secrets`: `Seal(plain []byte) (string, error)` y `Open(sealed string) ([]byte, error)` con AES-256-GCM, nonce aleatorio de 12 bytes y formato `v1:<base64(nonce‖texto cifrado)>`. El prefijo de versión permite rotar la clave en el futuro.

Se cifran: claves de recuperación de BitLocker (esta fase), clave de la cuenta de servicio de Google (fase 4), contraseña SMTP y secretos de webhook (fase 6).

Una instalación de la fase 1 que se actualiza debe añadir la variable al `.env`; la guía de despliegue y las notas de versión lo indican. Perder la clave hace ilegibles esos datos (no el resto de la base de datos).

### 3.2 Cola de tareas en segundo plano

Paquete `internal/server/jobs`, dentro del mismo binario. Tabla `jobs`: `id` (bigserial), `kind`, `payload` (`jsonb`), `unique_key` (nulo o texto), `run_at`, `attempts`, `max_attempts` (5 por defecto), `locked_until`, `last_error`, `created_at`, `finished_at`. Índice único parcial sobre `unique_key` donde `finished_at IS NULL`, para que no haya dos tareas pendientes iguales.

- Un trabajador por proceso toma tareas con `SELECT … FOR UPDATE SKIP LOCKED` (varias réplicas del servidor nunca ejecutan la misma), con un tiempo de bloqueo de 5 minutos que se renueva mientras trabaja.
- Fallo: reintento con espera exponencial (30 s, 2 min, 8 min…) hasta `max_attempts`; después queda con `last_error` y se ve en **Ajustes › Tareas**.
- Tareas periódicas declaradas en código (`Every(kind, intervalo)`): al arrancar y tras cada ejecución, se encola la siguiente con su `unique_key`.
- Se conservan 7 días las tareas terminadas.

Tareas de esta fase: `groups.recompute` (cada hora y bajo demanda), `commands.expire` (cada 5 min), `compliance.recompute` (cada 15 min: reglas del servidor y paso a «desconocido»), `cleanup` (sustituye al limpiador de la fase 1).

### 3.3 Ajustes globales

Tabla `settings` (`key` primaria, `value` `jsonb`, `updated_at`, `updated_by`). Esta fase define `agent.auto_update` (booleano, por defecto activado), `compliance.max_offline_days` (por defecto 14), `compliance.min_agent_version`, `compliance.min_os_version.windows` y `compliance.min_os_version.linux` (por defecto vacíos, es decir, sin regla). Página **Ajustes** en la consola, solo administradores, con auditoría `settings.updated`.

## 4. Actualización automática del agente

- El servidor conoce la versión de agente que distribuye (la suya propia: se compilan juntos) y las sumas de `SHA256SUMS` de `MDM_DOWNLOADS_DIR`, que lee al arrancar.
- El check-in añade `agent_version` y `capabilities` (lista de funciones que el agente soporta, para que el servidor no envíe órdenes que una versión antigua no entiende).
- La respuesta incluye `agent_update: {version, url, sha256}` cuando la versión del agente es distinta de la objetivo, `agent.auto_update` está activo y el grupo principal del equipo no tiene una versión fijada. Tabla `agent_pins` (`group_id`, `version`): un grupo fijado recibe esa versión, si existe en descargas.
- Proceso en el agente, sin interrumpir el bucle si algo falla:
  1. Descarga a un temporal en su directorio de configuración y comprueba el SHA-256. Si no coincide, lo borra y lo anota.
  2. **Prueba previa**: ejecuta el binario nuevo con `mdm-agent selftest`, que carga la configuración y hace un check-in real. Si no termina con código 0 en 60 s, se descarta y no se reintenta esa versión hasta el día siguiente.
  3. Sustitución: renombra el ejecutable actual a `mdm-agent.old` (Windows permite renombrar un ejecutable en uso, no sobrescribirlo), mueve el nuevo a su sitio y termina con código 75.
  4. El gestor de servicios lo reinicia (systemd `Restart=always`; en Windows, las acciones de recuperación ya configuradas en la fase 1). Al primer check-in correcto del binario nuevo, borra `mdm-agent.old`.
- Auditoría por equipo: `agent.updated` con versión anterior y nueva, registrada por el servidor al ver la versión nueva en el check-in.

## 5. Políticas

### 5.1 Modelo

Una **política** tiene nombre, descripción, plataforma (`windows`, `linux`; `android` en la fase 4), prioridad (entero único; menor número, mayor prioridad) y una lista de **ajustes**. Cada ajuste: `key` del catálogo, `mode` (`audit` o `enforce`) y `params` (JSON validado contra el esquema del ajuste).

**Asignaciones** (`policy_assignments`): `policy_id`, destino (`group_id` o «todos los equipos»), `exclude` (booleano). Un equipo recibe la política si está en algún destino incluido y en ningún destino excluido. Los grupos pueden ser estáticos (grupo principal) o dinámicos.

**Política efectiva** de un equipo: unión de los ajustes de sus políticas de su plataforma. Si dos políticas fijan la misma `key`, gana la de mayor prioridad y la consola muestra el conflicto en la ficha del equipo y en la de ambas políticas. Tiene una huella SHA-256 de su JSON canónico.

### 5.2 Catálogo inicial

Cada ajuste es código en el agente (comprobación y, si procede, aplicación) y un esquema de parámetros en el servidor. «Solo auditar» significa que `enforce` se rechaza al guardar.

| Clave | Plataforma | Parámetros | Aplicación |
|---|---|---|---|
| `windows.bitlocker` | Windows | `escrow_key` (bool) | Activa BitLocker en la unidad del sistema con TPM y contraseña de recuperación (`Enable-BitLocker -UsedSpaceOnly`); custodia la clave |
| `windows.firewall` | Windows | — | Activa los tres perfiles |
| `windows.defender_realtime` | Windows | — | Activa la protección en tiempo real (`Set-MpPreference`) |
| `windows.screen_lock` | Windows | `minutes` (1–60) | Directiva de inactividad del equipo (`InactivityTimeoutSecs`) |
| `windows.password_policy` | Windows | `min_length` (8–20), `max_age_days` (0–365) | `net accounts` |
| `windows.usb_storage` | Windows | `blocked` (bool) | Servicio `USBSTOR` deshabilitado |
| `windows.smb1` | Windows | — | Desactiva SMBv1 |
| `windows.guest_account` | Windows | — | Desactiva la cuenta Invitado |
| `windows.autologon` | Windows | — | Elimina el inicio de sesión automático |
| `windows.remote_desktop` | Windows | `allowed` (bool) | `fDenyTSConnections` |
| `linux.firewall` | Linux | — | Activa ufw o firewalld (el que exista) |
| `linux.auto_security_updates` | Linux | — | `unattended-upgrades` o `dnf-automatic` en modo seguridad |
| `linux.ssh` | Linux | `root_login` (bool), `password_auth` (bool) | Fichero en `sshd_config.d`, validado con `sshd -t` antes de recargar |
| `linux.disk_encryption` | Linux | — | Solo auditar |
| `linux.forbidden_packages` | Linux | `names` (lista) | Solo auditar |
| `custom.script` | Ambas | `interpreter`, `detect`, `remediate` (opcional), `timeout_s` | Detección: código 0 = cumple. Corrección si `enforce` y no cumple |

En Windows, cada ajuste es un pequeño script de PowerShell incluido en el agente (`go:embed`) que devuelve JSON `{"compliant": bool, "detail": "…"}`; la lógica de interpretación está en Go y tiene pruebas. Cualquier cambio que aplique el agente se registra en su registro.

### 5.3 Evaluación en el agente

- El check-in devuelve `policy_hash`. Si difiere del que tiene el agente, este pide `GET /agent/v1/policy` y evalúa al momento; después, cada 15 minutos.
- Para cada ajuste: comprobar; si no cumple y está en `enforce`, aplicar y volver a comprobar. Estados: `compliant`, `noncompliant`, `error`, `not_applicable` (p. ej. BitLocker sin TPM, o un ajuste de Windows Home que no lo soporta).
- Envía `POST /agent/v1/compliance` con la huella evaluada y el resultado de cada ajuste (mensaje de hasta 1 KB).
- Un ajuste no puede tardar más de 5 minutos (scripts personalizados: su `timeout_s`, máximo 60 min). Se ejecutan en serie.

### 5.4 Claves de BitLocker

`POST /agent/v1/recovery-keys` con `volume`, `key_id` (identificador del protector) y `recovery_password`. Tabla `recovery_keys`: `device_id`, `volume`, `key_id`, `secret` (cifrado), `reported_at`. El agente la envía tras activar BitLocker y, en cada evaluación de `windows.bitlocker` con `escrow_key`, si el protector actual no figura como custodiado (el servidor devuelve la lista de `key_id` custodiados en la política efectiva).

En la consola, la ficha muestra que hay clave custodiada; **«Mostrar clave»** (solo administradores) la descifra, la muestra 60 s y registra `recovery_key.viewed`.

## 6. Cumplimiento

Estado por equipo, calculado en el servidor al recibir un informe y en `compliance.recompute`:

- **Cumple**: todos los ajustes asignados en `compliant` o `not_applicable`, y cumple las reglas del servidor.
- **No cumple**: algún ajuste en `noncompliant` o `error`, o falla alguna regla del servidor. Se guarda `noncompliant_since`.
- **Desconocido**: sin informe para la huella vigente en 24 horas, o sin políticas asignadas.

Reglas del servidor (ajustes globales): sin conexión desde hace más de `compliance.max_offline_days`; versión del agente menor que `compliance.min_agent_version`; versión mínima del sistema por plataforma (`compliance.min_os_version.windows`, `.linux`), comparada como versión.

Tablas: `device_compliance` (`device_id`, `state`, `noncompliant_since`, `policy_hash`, `evaluated_at`) y `device_setting_results` (`device_id`, `policy_id`, `key`, `status`, `message`, `applied`, `evaluated_at`).

Consola: columna y filtro de cumplimiento en el listado, pestaña **Cumplimiento** en la ficha (ajuste, política, estado, mensaje, última evaluación, conflictos), sección en el resumen, y en cada política el recuento de equipos que cumplen y no.

## 7. Grupos dinámicos

`groups` gana `kind` (`static` por defecto, `dynamic`) y `rule` (`jsonb`). Un grupo estático sigue siendo el «grupo principal» de la fase 1 (un equipo, como mucho uno). Un grupo dinámico no se elige a mano: su pertenencia la calcula la regla.

Regla: `{"match": "all" | "any", "conditions": [{"field", "op", "value"}]}`, de 1 a 20 condiciones, sin anidamiento.

| Campo | Operadores |
|---|---|
| `platform`, `group` (grupo principal) | `eq`, `ne` |
| `os_name`, `hostname`, `manufacturer`, `model`, `assigned_user`, `domain` | `eq`, `ne`, `contains`, `starts_with` (sin distinguir mayúsculas) |
| `os_version`, `agent_version` | `eq`, `lt`, `lte`, `gt`, `gte` (comparación de versiones por segmentos numéricos) |
| `tag` | `has`, `not_has` |
| `software` | `installed`, `not_installed` (nombre, `contains`) |
| `compliance`, `status` | `eq`, `ne` |
| `last_seen_days` | `lt`, `gt` |

Evaluación en Go sobre los datos del equipo (1.000 equipos se evalúan en milisegundos). Pertenencia materializada en `group_members` (`group_id`, `device_id`), recalculada: para un equipo, al guardar su inventario, su ficha o su cumplimiento; para un grupo, al guardar su regla; para todos, cada hora. Un cambio de pertenencia cambia la política efectiva, y el equipo la recibe en su siguiente check-in.

El editor de reglas de la consola muestra en vivo cuántos equipos cumplirían la regla y los 10 primeros (`POST /api/v1/groups/preview`).

## 8. Órdenes

### 8.1 Tipos de esta fase

| Tipo | Parámetros | Rol mínimo |
|---|---|---|
| `reboot`, `shutdown` | `delay_s` (0–3600), `message` | Operador |
| `refresh_inventory` | — | Operador |
| `evaluate_policies` | — | Operador |
| `run_script` | `script_version_id`, `params` | Administrador |
| `retire` | — | Administrador |

`retire` desinstala el agente; el servidor revoca el equipo cuando recibe el resultado (o, si nunca llega, al caducar la orden, avisando en la consola).

### 8.2 Ciclo de vida

`commands` (tabla de la fase 1) gana: `batch_id`, `status` (`pending`, `sent`, `running`, `succeeded`, `failed`, `expired`, `cancelled`), `sent_at`, `started_at`, `finished_at`, `expires_at` (7 días por defecto), `exit_code`, `stdout`, `stderr` (64 KB cada uno, recortados por el principio, con aviso), `error`.

- El check-in entrega hasta 10 órdenes `pending` o `sent` no caducadas, más antiguas primero, y las marca `sent`. Una orden `sent` se reenvía en cada check-in hasta que el agente la confirme; el agente la reconoce por su id.
- El agente guarda en su estado los ids de las órdenes ya iniciadas (últimas 500). Al recibir una orden ya iniciada no la repite: reenvía su resultado si lo tiene, o informa `failed` con «interrumpida por un reinicio del agente».
- `POST /agent/v1/commands/{id}/start` y `POST /agent/v1/commands/{id}/result`. Las órdenes se ejecutan de una en una, en un hilo aparte del check-in (un script largo no detiene los check-ins).
- `commands.expire` marca `expired` las pendientes caducadas, y `failed` («sin respuesta del equipo») las `running` con más de `timeout + 10 min`.
- Cancelar es posible mientras esté `pending` o `sent`.

### 8.3 Lotes

`command_batches`: `id`, `type`, `params`, `created_by`, `created_at`, `target_description` (p. ej. «Grupo Contabilidad (14 equipos)»). Crear un lote con una selección, un grupo o una lista de ids crea una orden por equipo. Límite de 1.000 equipos por lote.

Consola: página **Actividad** con los lotes (progreso por estado), detalle de un lote con la fila de cada equipo y su salida, y pestaña **Actividad** en la ficha (sustituye a la de la fase 1, que pasa a llamarse «Conexiones»). Acciones disponibles desde la ficha, desde una selección múltiple del listado y desde un grupo.

## 9. Biblioteca de scripts

- `scripts`: `id`, `name`, `description`, `platform`, `interpreter` (`powershell`; `sh`, `bash`, `python3`), `timeout_s` (por defecto 300, máximo 3.600), `parameters` (lista de `{name, description, required, default}`; nombres `[A-Z][A-Z0-9_]*`), `created_by`, `archived_at`.
- `script_versions`: `id`, `script_id`, `version` (1, 2…), `content` (hasta 256 KB), `created_by`, `created_at`. Editar crea una versión; las anteriores se conservan.
- Ejecutar: diálogo con los parámetros, el destino y un resumen («Se ejecutará *Limpiar temporales* v3 como SYSTEM en 14 equipos»). Los parámetros llegan al proceso como variables de entorno `MDM_PARAM_<NOMBRE>`, nunca sustituidos en el texto.
- En el agente: el script se escribe en un temporal con permisos solo para root/SYSTEM, se ejecuta (`powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File`, o el intérprete indicado) con el tiempo límite, y se borra. Al agotarse el tiempo se mata el árbol completo (Windows: objeto de trabajo; Linux: grupo de procesos).
- La salida se conserva 30 días (tarea `cleanup`).

## 10. API

### 10.1 Agentes (`/agent/v1`)

| Método y ruta | Descripción |
|---|---|
| `POST /checkin` | Petición: + `capabilities`, `policy_hash` (la que tiene evaluada). Respuesta: + `policy_hash` vigente, `agent_update`, `commands` (con `id`, `type`, `payload`, `expires_at`) |
| `GET /policy` | Política efectiva: `{hash, evaluate_interval_s, settings: [{policy_id, key, mode, params}], escrowed_key_ids}` |
| `POST /compliance` | `{policy_hash, evaluated_at, results: [{policy_id, key, status, message, applied}]}` |
| `POST /recovery-keys` | Custodia de clave BitLocker |
| `POST /commands/{id}/start` · `POST /commands/{id}/result` | Ciclo de vida de una orden |

### 10.2 Consola (`/api/v1`, novedades)

Políticas (`/policies`, `/policies/{id}`, `/policies/{id}/assignments`, `/policies/{id}/devices`), catálogo (`GET /policy-catalog?platform=`), cumplimiento (`/devices/{id}/compliance`), grupos (`kind`, `rule`, `POST /groups/preview`), órdenes y lotes (`POST /batches`, `GET /batches`, `GET /batches/{id}`, `POST /commands/{id}/cancel`, `GET /devices/{id}/commands`), scripts (`/scripts`, `/scripts/{id}/versions`), claves (`POST /devices/{id}/recovery-keys/{key_id}/reveal`), ajustes (`/settings`, `/agent-pins`), tareas (`GET /jobs?state=failed`).

Nuevas acciones de auditoría: `policy.created|updated|deleted|assigned`, `script.created|updated|archived`, `batch.created`, `command.cancelled`, `recovery_key.viewed`, `settings.updated`, `agent.updated`, `device.retired`.

## 11. Roles

| Acción | Solo lectura | Operador | Administrador |
|---|:-:|:-:|:-:|
| Ver políticas, cumplimiento, lotes y resultados | ✓ | ✓ | ✓ |
| Crear y editar grupos dinámicos | | ✓ | ✓ |
| Reiniciar, apagar, actualizar inventario, reevaluar | | ✓ | ✓ |
| Crear, editar y asignar políticas | | | ✓ |
| Crear scripts y ejecutarlos; retirar equipos | | | ✓ |
| Ver claves de BitLocker; ajustes globales | | | ✓ |

## 12. Comportamiento ante fallos

| Situación | Comportamiento |
|---|---|
| Un ajuste falla al aplicarse | Estado `error` con el mensaje; los demás ajustes se evalúan igual |
| La política no se puede descargar | El agente sigue con la última que tiene y reintenta en el siguiente check-in |
| El agente se reinicia a mitad de una orden | No la repite; informa `failed` («interrumpida») |
| Una orden no llega nunca | Caduca a los 7 días; el lote lo refleja |
| El binario nuevo del agente falla la prueba previa | No se instala; se reintenta al día siguiente; se registra en el estado del agente |
| `MDM_SECRET_KEY` falta o no es válida | El servidor no arranca y explica cómo generarla |
| Una tarea en segundo plano falla 5 veces | Queda visible en Ajustes › Tareas con su error |
| Un agente antiguo sin `capabilities` | Recibe solo órdenes que entiende; el resto queda `pending` hasta que se actualice |

## 13. Pruebas

- Servidor: cifrado (ida y vuelta, clave errónea, formato), cola de tareas (concurrencia con dos trabajadores sin duplicados, reintentos, periodicidad, `unique_key`), política efectiva (prioridades, conflictos, exclusiones, huella estable), reglas dinámicas (cada campo y operador, comparación de versiones), cumplimiento (cada transición), ciclo de vida de órdenes (entrega, reentrega, no repetición, caducidad, cancelación), roles en cada ruta, auditoría.
- Agente: bucle con órdenes (no repetición tras reinicio, ejecución en paralelo al check-in), ejecutor de scripts con tiempo límite y salida recortada, ajustes de Linux con un ejecutor falso, interpretación de las salidas JSON de los scripts de Windows, actualización (suma incorrecta, prueba previa fallida, sustitución).
- Consola: editor de políticas, editor de reglas con vista previa, diálogo de ejecución, página de lotes, pestaña de cumplimiento, vista de clave BitLocker.
- Windows: ampliación de `docs/validacion-windows.md` con cada ajuste del catálogo, la custodia de claves, scripts y la actualización del agente.
- Extremo a extremo (Docker): política de Linux aplicada y cumplida; script ejecutado en el contenedor; actualización del agente a una versión compilada con otro número.

## 14. Decisiones

- **Estado deseado para lo duradero, órdenes para lo puntual** (enfoque mixto aprobado).
- **Grupo principal estático y grupos dinámicos múltiples**: se mantiene la fase 1 sin migraciones de datos.
- **Prioridad explícita** para resolver conflictos, en vez de «el más restrictivo gana», que no está definido para todos los ajustes.
- **Prueba previa del binario nuevo** en vez de un vigilante que revierta: evita instalar un agente que no arranca, sin procesos auxiliares.
- **Scripts solo para administradores**: equivalen a control total del equipo.
- **Cola en PostgreSQL** y no un servicio aparte: no añade piezas al despliegue.
