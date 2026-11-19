# MDM — Fase 3: software y parches (diseño)

Fecha: 2026-11-16 · Estado: pendiente de revisión · Depende de: fase 2

## 1. Objetivo

Instalar, mantener y retirar software en los equipos, y mantener al día los parches del sistema operativo, con ventanas de mantenimiento y sin saturar el servidor.

Criterios de éxito:

- Un administrador sube el MSI del programa de contabilidad, lo despliega como obligatorio en «Contabilidad» y en una hora los 14 equipos lo tienen instalado; si alguien lo desinstala, vuelve a instalarse.
- Firefox (winget) y nginx (apt) se mantienen en la última versión del repositorio en los grupos asignados.
- Los equipos de un grupo instalan las actualizaciones de seguridad de Windows aprobadas solo entre las 22:00 y las 05:00, con 7 días de espera desde su publicación, y la consola muestra cuántos equipos necesitan cada KB.

## 2. Alcance

Dentro: catálogo de aplicaciones (winget, apt, dnf e instaladores subidos), almacenamiento de ficheros, despliegues obligatorios y de desinstalación, búsqueda e instalación de actualizaciones de Windows y Linux, políticas de parches, aprobaciones manuales de KB, reinicios controlados.

Fuera: portal de autoservicio en el equipo, actualizaciones de versión de Windows (23H2 → 24H2), aplicaciones Android (fase 4 reutiliza este catálogo), distribución entre equipos (P2P) o servidores de caché.

## 3. Almacenamiento de ficheros

- Nueva variable `MDM_FILES_DIR` (por defecto `/data/files`). Docker Compose añade el volumen `files`. Las copias de seguridad deben incluirlo; la guía de despliegue lo explica.
- Ficheros por contenido: `<dir>/<sha256[0:2]>/<sha256>`. Tabla `files`: `sha256` (primaria), `size`, `original_name`, `content_type`, `uploaded_by`, `uploaded_at`. Un fichero sin referencias se borra 7 días después (tarea `cleanup`).
- Subida: `POST /api/v1/files`, multipart en streaming (no se carga en memoria), hasta 4 GB, calculando el SHA-256 mientras se escribe en un temporal del mismo directorio y renombrando al final. Si el fichero ya existe, se reutiliza. El servidor rechaza la subida si el disco quedaría con menos de 1 GB libre.
- Descarga para agentes: `GET /agent/v1/files/{sha256}` con la credencial del equipo, con soporte de `Range` (reanudación) y `ETag`. El agente verifica el SHA-256 antes de usar el fichero.
- El proxy y el servidor deben permitir cuerpos grandes: la ruta de subida tiene límite propio (4 GB) y tiempos de lectura de 30 minutos; el resto de la API conserva sus límites.

## 4. Catálogo de aplicaciones

`apps`: `id`, `name`, `publisher`, `description`, `platform` (`windows`, `linux`), `source` (`winget`, `apt`, `dnf`, `upload`), `package_id` (winget/apt/dnf), `created_by`, `archived_at`.

`app_versions` (solo `upload`): `id`, `app_id`, `version`, `file_sha256`, `installer_type` (`msi`, `exe`, `deb`, `rpm`), `install_command`, `uninstall_command`, `success_codes` (por defecto `[0, 3010, 1641]`; 3010 y 1641 significan «reinicio necesario»), `detection` (JSON), `is_current`, `created_at`.

Órdenes por defecto, con `{file}` sustituido por la ruta local del instalador:

| Tipo | Instalar | Desinstalar |
|---|---|---|
| `msi` | `msiexec /i "{file}" /qn /norestart` | `msiexec /x {product_code} /qn /norestart` (código leído del MSI al subirlo) |
| `exe` | obligatorio indicarla | obligatorio indicarla |
| `deb` | `apt-get install -y "{file}"` | `apt-get remove -y {package}` |
| `rpm` | `dnf install -y "{file}"` | `dnf remove -y {package}` |

Reglas de detección (`detection`), una por versión:

- `inventory`: el software del inventario contiene `name_contains` con versión ≥ `min_version` (por defecto la de la versión). Es la opción por defecto.
- `file`: existe `path` (con variables de entorno) y, opcionalmente, su versión de fichero ≥ `min_version` (solo Windows).
- `registry`: existe la clave o el valor indicado (solo Windows).
- `script`: script de detección; código 0 = instalada.

Para winget, apt y dnf la detección es la propia del gestor (`winget list --id`, `dpkg-query -W`, `rpm -q`) y no se configura.

**winget**: el agente localiza `winget.exe` del paquete App Installer para todo el sistema y ejecuta `winget install --id <id> --exact --silent --scope machine --accept-package-agreements --accept-source-agreements --disable-interactivity`. Si no existe (habitual en Windows Server), la aplicación es `not_applicable` en ese equipo, con el motivo.

## 5. Despliegues

`deployments`: `id`, `app_id`, `intent` (`required`, `uninstall`), `version_policy` (`current`: la versión vigente; `latest`: solo para gestores de paquetes, mantenerla actualizada), `start_at`, `respect_window` (bool), `reboot` (`never`, `notify`, `in_window`), `created_by`. Asignaciones con inclusión y exclusión, como las políticas (`deployment_assignments`).

La política efectiva de un equipo (fase 2) gana la sección `apps`: lista de `{deployment_id, app, version, intent, source, package_id, file_sha256, size, install_command, uninstall_command, success_codes, detection, respect_window, reboot}`. Su huella forma parte de `policy_hash`, de modo que un cambio llega en el siguiente check-in.

En el agente, en cada evaluación (cada 15 min y al cambiar la política):

1. Para cada despliegue, detecta. Si el estado coincide con la intención, informa `installed` (o `absent`).
2. Si no coincide y `start_at` ya pasó (y está dentro de la ventana si `respect_window`), lo pone en cola. Una instalación a la vez.
3. Antes de la primera descarga de un fichero, espera un retraso aleatorio de 0 a 30 minutos (calculado una vez por despliegue y equipo) para repartir la carga.
4. Descarga con reanudación, verifica, ejecuta con tiempo límite de 60 minutos y vuelve a detectar. Instalación correcta = código de éxito **y** detección positiva.
5. Si falla, reintenta a los 15 min, 1 h y 4 h; después queda `failed` hasta que cambie el despliegue o un operador pulse «Reintentar».

Estado por equipo (`deployment_status`): `device_id`, `deployment_id`, `state` (`pending`, `waiting_window`, `downloading`, `installing`, `installed`, `absent`, `failed`, `not_applicable`), `attempts`, `last_exit_code`, `last_output` (últimos 4 KB), `reboot_required`, `updated_at`. Informe: `POST /agent/v1/apps/status`.

Desinstalar se trata igual con la orden de desinstalación (o `winget uninstall`, `apt-get remove`, `dnf remove`).

## 6. Parches del sistema

### 6.1 Búsqueda

Una vez al día (a una hora aleatoria fijada por equipo) y bajo la orden `scan_updates`, el agente busca actualizaciones y envía `POST /agent/v1/updates`:

```json
{
  "scanned_at": "…",
  "reboot_pending": false,
  "updates": [
    { "id": "…", "kb": "KB5046617", "title": "…", "classification": "security",
      "severity": "critical", "size_bytes": 0, "published_at": "…", "security": true }
  ]
}
```

- **Windows**: script de PowerShell incluido en el agente que usa la API de Windows Update (`Microsoft.Update.Session`, búsqueda `IsInstalled=0 and IsHidden=0`). `id` es el `UpdateID`; `classification` se normaliza a `security`, `critical`, `definition`, `feature_pack`, `update`, `driver`, `other`. Reinicio pendiente: claves de registro estándar de Windows Update y CBS.
- **Linux**: apt (`apt-get -s upgrade`, seguridad = origen `*-security`) o dnf (`dnf check-update`, `dnf updateinfo list --security`). `id` = nombre del paquete; `kb` vacío. Reinicio pendiente: `/var/run/reboot-required` o `dnf needs-restarting -r`.

Tablas: `updates` (catálogo global por `id` y plataforma, con título, KB, clasificación, gravedad y fecha de publicación) y `device_updates` (`device_id`, `update_id`, `state`: `missing`, `installed`, `failed`, `first_seen_at`, `installed_at`, `last_error`). `device_patch_state`: `device_id`, `last_scan_at`, `last_install_at`, `reboot_pending`.

### 6.2 Políticas de parches

`patch_policies`: `id`, `name`, `platform`, `auto_approve` (lista de clasificaciones), `defer_days` (0–60), `window` (`days`: lista de días de la semana; `start` y `end` en `HH:MM` en hora local del equipo; si `end` < `start`, cruza la medianoche), `reboot` (`never`, `notify`, `in_window`), `notify_minutes` (aviso previo al reinicio, 5–120), `excluded_kbs`. Asignación con prioridad, inclusión y exclusión; un equipo usa una sola política de parches (la de mayor prioridad).

Aprobaciones manuales (`update_approvals`): `update_id`, `decision` (`approved`, `declined`), `decided_by`, `decided_at`. Una decisión manual prevalece sobre la regla automática. Solo para Windows (en Linux se aprueban por clasificación: seguridad o todo).

La política efectiva gana la sección `patches`: `{window, reboot, notify_minutes, linux_scope: "security" | "all", approved_update_ids: [...]}`. El servidor calcula `approved_update_ids` para cada equipo a partir de su última búsqueda: actualizaciones que le faltan, aprobadas manual o automáticamente, con `published_at + defer_days` ya cumplido, no rechazadas ni excluidas.

### 6.3 Instalación y reinicio

- Dentro de la ventana, el agente instala las aprobadas (Windows: API de Windows Update con la lista de `UpdateID`; Linux: `apt-get upgrade` o `dnf upgrade`, limitado a seguridad si `linux_scope` lo indica). Informa por actualización.
- Si hace falta reiniciar: `never` lo deja pendiente; `notify` avisa al usuario con el mensaje «El equipo se reiniciará en N minutos para terminar de instalar actualizaciones» (`shutdown /r /t` en Windows; `shutdown -r +N` con mensaje en Linux) y reinicia; `in_window` hace lo mismo pero solo dentro de la ventana. Un reinicio nunca se lanza fuera de la ventana salvo con la orden `reboot` explícita.
- Las aplicaciones con despliegue y reinicio `in_window` también esperan a la ventana.

### 6.4 Cumplimiento

Nueva regla del servidor (ajuste global `compliance.max_security_update_age_days`, por defecto 30, 0 = desactivada): no cumple si tiene una actualización de seguridad aprobada pendiente desde hace más de N días (contados desde `first_seen_at`).

## 7. Consola

- **Software**: catálogo con su origen, versiones y despliegues; asistente para añadir (buscar por identificador de winget o nombre de paquete, o subir un instalador con barra de progreso); editor de reglas de detección.
- **Despliegue**: intención, versión, asignaciones, opciones; vista de estado con recuento por estado y tabla por equipo (código y salida de error); «Reintentar» en un equipo o en todos los fallidos.
- **Parches**: actualizaciones conocidas con cuántos equipos las necesitan, instaladas y fallidas; filtros por clasificación y gravedad; aprobar o rechazar; políticas de parches.
- Ficha del equipo: pestañas **Software gestionado** (despliegues que le aplican y su estado) y **Actualizaciones** (pendientes, aprobadas, instaladas recientes, última búsqueda, reinicio pendiente, botón «Buscar ahora»).
- Resumen: despliegues con fallos y equipos con actualizaciones de seguridad pendientes.

## 8. API (novedades)

Agentes: `GET /files/{sha256}`, `POST /apps/status`, `POST /updates`, `POST /updates/results`; órdenes nuevas `scan_updates` e `install_updates_now` (operador; ignora la ventana).

Consola: `/files`, `/apps`, `/apps/{id}/versions`, `/deployments`, `/deployments/{id}/status`, `/deployments/{id}/retry`, `/updates`, `/updates/{id}/approval`, `/patch-policies`, `/devices/{id}/deployments`, `/devices/{id}/updates`.

Auditoría: `file.uploaded`, `app.created|updated|archived`, `deployment.created|updated|deleted`, `update.approved|declined`, `patch_policy.created|updated|deleted`.

## 9. Roles

| Acción | Solo lectura | Operador | Administrador |
|---|:-:|:-:|:-:|
| Ver catálogo, despliegues, parches y estados | ✓ | ✓ | ✓ |
| Buscar actualizaciones ahora, instalar ahora, reintentar | | ✓ | ✓ |
| Catálogo, subidas, despliegues, políticas de parches, aprobaciones | | | ✓ |

## 10. Comportamiento ante fallos

| Situación | Comportamiento |
|---|---|
| Descarga cortada | Se reanuda con `Range` en el siguiente intento |
| SHA-256 distinto | Se borra la descarga, estado `failed` con «fichero dañado» |
| Instalador que no termina | Se mata a los 60 min; cuenta como intento fallido |
| Código de éxito pero detección negativa | `failed` con «el instalador terminó pero la aplicación no se detecta»: suele indicar una regla de detección mal definida |
| Disco del equipo lleno | `failed` con el espacio libre en el mensaje |
| Disco del servidor casi lleno | La subida se rechaza con un mensaje claro |
| La búsqueda de Windows Update falla | Se informa el error en el estado de parches; se reintenta al día siguiente o con «Buscar ahora» |
| Ventana que cruza la medianoche | Se interpreta como un único tramo |

## 11. Pruebas

- Servidor: subida en streaming (fichero de varios cientos de MB sin crecer la memoria), deduplicación, descarga con `Range`, cálculo de `approved_update_ids` (diferimiento, exclusiones, aprobaciones manuales), sección `apps` de la política efectiva, cumplimiento por antigüedad de parches, roles.
- Agente: cola de instalación, reintentos, retraso aleatorio estable, ventanas (incluida la medianoche y el cambio de horario), detección por inventario, fichero y script, interpretación de salidas de winget, apt, dnf y del script de Windows Update (con salidas grabadas).
- Windows: lista de comprobación ampliada (MSI, EXE con parámetros, winget como SYSTEM, búsqueda e instalación de actualizaciones, aviso de reinicio).
- Extremo a extremo (Docker): DEB subido e instalado en el contenedor; paquete apt desplegado y desinstalado; búsqueda de actualizaciones con apt.

## 12. Decisiones

- **Almacenamiento local por contenido** en un volumen, no S3: un servidor, menos piezas; la interfaz permite añadir S3 más adelante.
- **Retraso aleatorio** en lugar de un planificador central de descargas: suficiente para 1.000 equipos y sin coordinación.
- **PowerShell incluido para Windows Update** en vez de COM desde Go: más sencillo y es el mismo mecanismo que usan otras herramientas.
- **Aprobación por KB solo en Windows**: en Linux, aprobar paquete a paquete no aporta y complica.
