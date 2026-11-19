# MDM fase 3 (software y parches) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Instalar, mantener y desinstalar software (winget, apt, dnf e instaladores subidos) y mantener al día los parches del sistema con ventanas de mantenimiento, aprobaciones y reinicios controlados.

**Architecture:** Todo es estado deseado sobre la política efectiva de la fase 2: gana las secciones `apps` y `patches`, incluidas en su huella, así que un cambio llega en el siguiente check-in. El servidor guarda los instaladores en un volumen nombrado por SHA-256 y los sirve con reanudación. El agente gana un gestor de instalaciones (una a la vez, con retraso aleatorio estable y reintentos) y un gestor de parches (búsqueda diaria, instalación en ventana).

**Tech Stack:** Lo de las fases 1 y 2. Sin dependencias nuevas.

**Spec:** `docs/superpowers/specs/2026-11-16-mdm-fase3-software-design.md`

## Global Constraints

- Todo lo de las Global Constraints de los planes anteriores sigue vigente. Migraciones desde `0011_…`.
- Ficheros: `MDM_FILES_DIR` (por defecto `/data/files`), ruta `<dir>/<sha[0:2]>/<sha>`, máximo 4 GB por fichero, subida rechazada si quedaría menos de 1 GB libre, ficheros sin referencias borrados a los 7 días, tiempo de lectura de la subida 30 min.
- Instalaciones: una a la vez; tiempo máximo 60 min; reintentos a los 15 min, 1 h y 4 h, después `failed`; retraso aleatorio 0–30 min por despliegue y equipo (estable: derivado de hash(device_id, deployment_id)); salida guardada: últimos 4 KB; códigos de éxito por defecto `[0, 3010, 1641]` (3010 y 1641 = reinicio necesario).
- Parches: búsqueda diaria a una hora fija por equipo (derivada de su id) y bajo orden; `defer_days` 0–60; `notify_minutes` 5–120; ventana `HH:MM`–`HH:MM` en hora local del equipo (si fin < inicio cruza la medianoche); `compliance.max_security_update_age_days` por defecto 30 (0 = desactivada).
- Capacidades nuevas: `apps.v1`, `patches.v1`. Órdenes nuevas `scan_updates` e `install_updates_now` (requieren `patches.v1`).
- Un reinicio automático nunca se lanza fuera de la ventana salvo con la orden `reboot` explícita.

## Review Focus

1. **Instalador que termina con 0 pero la detección falla**: estado `failed` con el mensaje «el instalador terminó pero la aplicación no se detecta», sin bucle de reinstalación infinito (cuenta como intento). Prueba en la tarea 7.
2. **Ventana que cruza la medianoche y cambio de horario**: 22:00–05:00 contiene la 01:30 del día siguiente, también la noche del cambio de hora. Prueba en la tarea 7.
3. **Subida grande cortada a mitad**: no queda fichero a medias en el almacén ni fila en `files`. Prueba en la tarea 2.
4. **Descarga reanudada con `Range` tras corte**: el fichero final verifica su SHA-256. Prueba en la tarea 6.
5. **Aprobación manual frente a regla automática**: una KB rechazada a mano nunca aparece en `approved_update_ids` aunque su clasificación esté autoaprobada; una aprobada a mano aparece aunque no lo esté, respetando `defer_days`. Prueba en la tarea 5.

---

### Task 1: Protocolo de la fase 3

**Files:** Create `internal/protocol/software.go`; Test `internal/protocol/software_test.go`.

**Interfaces — Produces:**
- `EffectivePolicy` gana `Apps []AppAssignment` (`apps`) y `Patches *PatchPolicy` (`patches`, omitido si nulo); `PolicyHash` pasa a incluir ambos (firma nueva `PolicyHashFull(p EffectivePolicy) string`; `PolicyHash` sigue existiendo para los ajustes).
- `AppAssignment{DeploymentID, AppName, Version, Intent ("required"|"uninstall"), Source ("winget"|"apt"|"dnf"|"upload"), PackageID, FileSHA256 string; Size int64; InstallCommand, UninstallCommand string; SuccessCodes []int; Detection *Detection; KeepUpdated, RespectWindow bool; Reboot string ("never"|"notify"|"in_window")}`.
- `Detection{Type ("inventory"|"file"|"registry"|"script"), NameContains, MinVersion, Path, RegistryKey, RegistryValue, Interpreter, Script string}`.
- `PatchPolicy{Window Window; Reboot string; NotifyMinutes int; LinuxScope ("security"|"all"); ApprovedUpdateIDs []string}`; `Window{Days []int (0=domingo); Start, End string}`.
- `AppStatusReport{Items []AppStatus}`, `AppStatus{DeploymentID, State, Message string; Attempts int; ExitCode *int; RebootRequired bool; UpdatedAt time.Time}`; estados `AppPending`, `AppWaitingWindow`, `AppDownloading`, `AppInstalling`, `AppInstalled`, `AppAbsent`, `AppFailed`, `AppNotApplicable`.
- `UpdateScan{ScannedAt time.Time; RebootPending bool; Updates []UpdateInfo}`, `UpdateInfo{ID, KB, Title, Classification, Severity string; SizeBytes int64; PublishedAt *time.Time; Security bool}`; `UpdateResults{Results []UpdateResult; RebootPending bool}`, `UpdateResult{ID, Status ("installed"|"failed"), Error string}`.
- `CmdScanUpdates`, `CmdInstallUpdatesNow`; `CapApps`, `CapPatches`; `RequiredCapability` actualizada.
- `(w Window) Contains(t time.Time) bool` (en hora local de `t`).

- [ ] **Step 1: Pruebas que fallan**
```go
func TestPolicyHashFull_IncludesAppsAndPatches(t *testing.T) // cambiar una app, su versión o la ventana cambia la huella; reordenar apps no
func TestWindow_Contains(t *testing.T)                        // Review Focus 2: 22:00–05:00 con días [1..5]; lunes 23:00 sí; martes 01:30 sí (pertenece a la ventana del lunes); sábado 01:30 no si el viernes no está; 09:00 no; ventana vacía (sin días) nunca
func TestWindow_DSTNight(t *testing.T)                        // Europe/Madrid, noche del 29 al 30 de marzo de 2026: 02:30 no existe; 03:15 local sí está dentro de 22:00–05:00
func TestRequiredCapability_Phase3(t *testing.T)
func TestJSONNames_Phase3(t *testing.T)
```
- [ ] **Step 2–4:** Run `go test ./internal/protocol/` — FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat: protocolo de la fase 3`.

---

### Task 2: Almacenamiento de ficheros

**Files:** Create `migrations/0011_files.sql`, `internal/server/files/files.go`, `internal/server/store/files.go`, `internal/server/adminapi/files.go`, `internal/server/agentapi/files.go`; Modify `internal/server/app/config.go` (`FilesDir`), `internal/server/app/janitor.go`, `deploy/docker-compose.yml` (volumen `files`), `deploy/Caddyfile` (sin límite de cuerpo en `/api/v1/files`), `docs/despliegue.md` (copias de seguridad del volumen); Test `internal/server/files/files_test.go`, `internal/server/adminapi/files_test.go`, `internal/server/agentapi/files_test.go`.

**Interfaces — Produces:**
- `files.Store`: `files.New(dir string, minFree int64) *Store`; `(*Store).Put(ctx, r io.Reader, maxSize int64) (sha256 string, size int64, err error)` (escribe en un temporal del mismo directorio calculando el SHA-256, renombra al final; borra el temporal ante cualquier error o cancelación); `(*Store).Open(sha) (*os.File, os.FileInfo, error)`; `(*Store).Remove(sha) error`; `ErrTooLarge`, `ErrDiskFull`.
- Tabla `files` (`sha256`, `size`, `original_name`, `content_type`, `uploaded_by`, `uploaded_at`); `(*Store).SaveFile`, `(*Store).UnreferencedFiles(before time.Time)` (la referencia la ponen las versiones de app en la tarea 3; hasta entonces todos cuentan como sin referencias).
- `POST /api/v1/files` (administrador, multipart en streaming con el campo `file`, límite propio de 4 GB y lectura de 30 min) → `{sha256, size, original_name}`; auditoría `file.uploaded`.
- `GET /agent/v1/files/{sha256}` con la credencial del equipo, `http.ServeContent` (Range, ETag = sha).
- Tarea `cleanup` amplía: ficheros sin referencias de hace más de 7 días.

- [ ] **Step 1: Pruebas que fallan**
```go
func TestPut_StreamsAndHashes(t *testing.T)            // 300 MB de un lector sintético: sha correcto; memoria del proceso no crece > 50 MB (runtime.MemStats)
func TestPut_InterruptedLeavesNothing(t *testing.T)    // Review Focus 3: lector que falla a mitad → error, ningún fichero en el directorio (ni temporal)
func TestPut_DedupesSameContent(t *testing.T)
func TestPut_TooLargeAndDiskFull(t *testing.T)         // maxSize superado → ErrTooLarge; minFree inalcanzable → ErrDiskFull antes de escribir
func TestUploadAPI_AdminOnlyAndAudited(t *testing.T)
func TestUploadAPI_ClientDisconnect(t *testing.T)      // el cliente cierra a mitad: 0 filas en files
func TestAgentDownload_RangeAndAuth(t *testing.T)      // sin credencial 401; Range: bytes=100- devuelve 206 con el resto; sha desconocido 404
```
- [ ] **Step 2–4:** FAIL, implementar, PASS (`go test -race`).
- [ ] **Step 5:** Commit `feat: almacenamiento de instaladores`.

---

### Task 3: Catálogo de aplicaciones

**Files:** Create `migrations/0012_apps.sql`, `internal/server/store/apps.go`, `internal/server/adminapi/apps.go`; Test `internal/server/adminapi/apps_test.go`.

**Interfaces — Produces:**
- Tablas `apps` y `app_versions` según el apartado 4 de la especificación (con `file_sha256` referenciando `files`).
- API (lectura para todos, escritura administrador): `GET/POST /apps`, `GET/PATCH /apps/{id}`, `POST /apps/{id}/archive`, `GET/POST /apps/{id}/versions`, `POST /apps/{id}/versions/{vid}/current`.
- Validación: `source` coherente con `platform` (winget solo Windows; apt y dnf solo Linux; `upload` con versión obligatoria); `package_id` con formato (winget `^[A-Za-z0-9][A-Za-z0-9.\-_+]{0,127}$`; apt/dnf `^[a-z0-9][a-z0-9.+\-_:]{0,127}$`); `installer_type` coherente con la plataforma; EXE exige `install_command` y `uninstall_command`; órdenes por defecto de la tabla del apartado 4 (MSI desinstala con `msiexec /x "{file}" /qn /norestart`, que acepta la ruta del paquete y evita leer el código de producto del MSI); `detection` validada por tipo (`inventory` exige `name_contains`; `file` y `registry` solo en Windows; `script` con intérprete de la plataforma); `success_codes` 1–20 enteros.
- `UnreferencedFiles` deja de contar los ficheros referenciados por `app_versions`.
- Auditoría `app.created|updated|archived`.

- [ ] **Step 1: Pruebas que fallan**
```go
func TestApps_SourcePlatformRules(t *testing.T)     // winget en Linux 400; apt en Windows 400; upload sin versión 400
func TestApps_PackageIDFormat(t *testing.T)         // "Mozilla.Firefox" ok; "rm -rf /" 400; "nginx" ok
func TestAppVersions_DefaultsAndExe(t *testing.T)   // msi sin órdenes → valores por defecto; exe sin órdenes → 400; versión vigente única
func TestAppVersions_DetectionValidation(t *testing.T)
func TestApps_RolesAndAudit(t *testing.T)
func TestFiles_ReferencedNotPurged(t *testing.T)
```
- [ ] **Step 2–4:** FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat: catálogo de aplicaciones`.

---

### Task 4: Despliegues en el servidor

**Files:** Create `migrations/0013_deployments.sql`, `internal/server/store/deployments.go`, `internal/server/adminapi/deployments.go`, `internal/server/agentapi/apps.go`; Modify `internal/server/policy/effective.go`, `internal/server/store/policies.go` (`EffectivePolicyFor` incluye `apps`), `internal/server/agentapi/agentapi.go` (rutas); Test `internal/server/store/deployments_test.go`, `internal/server/adminapi/deployments_test.go`, `internal/server/agentapi/apps_test.go`.

**Interfaces — Produces:**
- Tablas `deployments`, `deployment_assignments` (como las de políticas), `deployment_status` (apartado 5).
- `policy.EffectiveApps(deployments []AssignedDeployment, groups []uuid.UUID, platform string) []protocol.AppAssignment` (misma regla de inclusión y exclusión; si dos despliegues de la misma app chocan, gana `uninstall` sobre `required` y queda constancia como conflicto).
- API: `GET/POST /deployments`, `GET/PATCH/DELETE /deployments/{id}`, `PUT /deployments/{id}/assignments`, `GET /deployments/{id}/status` (recuento por estado y filas por equipo), `POST /deployments/{id}/retry` (`{device_id}` opcional: todos los fallidos si se omite; operador), `GET /devices/{id}/deployments`.
- `POST /agent/v1/apps/status` (≤ 256 KB; ignora despliegues que no son del equipo; recorta mensajes a 4 KB).
- Reintentar = pone `attempts` a 0 y estado `pending` en el servidor y cambia un contador `retry_epoch` en la asignación del equipo, que forma parte de `AppAssignment` (`RetryEpoch int`) para que la huella cambie y el agente olvide sus reintentos.
- Auditoría `deployment.created|updated|deleted|retried`.

- [ ] **Step 1: Pruebas que fallan**
```go
func TestEffectiveApps_IncludeExcludeAndConflict(t *testing.T)
func TestEffectivePolicy_HashChangesWithDeployment(t *testing.T)
func TestAppsStatus_StoredAndIgnoresForeign(t *testing.T)
func TestDeploymentStatus_Counts(t *testing.T)
func TestRetry_ResetsAndChangesHash(t *testing.T)
func TestDeployments_RolesAndValidation(t *testing.T) // keep_updated solo con winget/apt/dnf; app archivada 400; app de otra plataforma que el grupo: se acepta (no aplica a esos equipos)
```
- [ ] **Step 2–4:** FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat: despliegues de software`.

---

### Task 5: Parches en el servidor

**Files:** Create `migrations/0014_patches.sql`, `internal/server/patches/approve.go`, `internal/server/store/patches.go`, `internal/server/agentapi/updates.go`, `internal/server/adminapi/patches.go`; Modify `internal/server/store/policies.go` (sección `patches`), `internal/server/compliance/compliance.go` (regla de antigüedad), `internal/server/settings/settings.go` (`MaxSecurityUpdateAgeDays`); Test `internal/server/patches/approve_test.go`, `internal/server/adminapi/patches_test.go`, `internal/server/agentapi/updates_test.go`.

**Interfaces — Produces:**
- Tablas `updates`, `device_updates`, `device_patch_state`, `patch_policies` (+ asignaciones con prioridad como las políticas), `update_approvals`.
- `patches.Approved(missing []store.UpdateRow, pol PatchPolicy, decisions map[string]string, now time.Time) []string` (lógica pura).
- `POST /agent/v1/updates` (≤ 1 MB; sustituye la lista de pendientes del equipo; da de alta en `updates` las desconocidas) y `POST /agent/v1/updates/results`.
- API de consola: `GET /updates` (filtros clasificación, gravedad, plataforma; con número de equipos que la necesitan), `PUT /updates/{id}/approval` (`approved`, `declined` o `null` para volver a la regla; administrador), `GET/POST/PATCH/DELETE /patch-policies`, `PUT /patch-policies/{id}/assignments`, `GET /devices/{id}/updates`, `POST /patch-policies/reorder`.
- Política efectiva: `patches` = política de parches de mayor prioridad aplicable + `ApprovedUpdateIDs` calculados con la última búsqueda del equipo.
- Cumplimiento: no cumple si tiene una actualización de seguridad aprobada pendiente con `first_seen_at` anterior a `now − max_security_update_age_days`.
- Órdenes `scan_updates` e `install_updates_now` aceptadas en `POST /batches` (operador).

- [ ] **Step 1: Pruebas que fallan**
```go
func TestApproved_AutoByClassification(t *testing.T)
func TestApproved_DeferDays(t *testing.T)             // publicada hace 6 días con defer 7 → no; hace 8 → sí; sin fecha de publicación → cuenta first_seen_at
func TestApproved_ManualOverrides(t *testing.T)        // Review Focus 5
func TestApproved_ExcludedKBs(t *testing.T)
func TestApproved_LinuxScope(t *testing.T)             // Linux: security → solo security=true; all → todas; las decisiones manuales no se aplican
func TestUpdatesScan_ReplacesPending(t *testing.T)
func TestUpdatesResults_MarksInstalled(t *testing.T)
func TestCompliance_SecurityUpdateAge(t *testing.T)
func TestPatchPolicies_PriorityAndAssignment(t *testing.T)
func TestUpdatesAPI_CountsAndApprovalRoles(t *testing.T)
```
- [ ] **Step 2–4:** FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat: parches en el servidor`.

---

### Task 6: Descargas del agente

**Files:** Modify `internal/agent/client/client.go`; Create `internal/agent/fetch/fetch.go`; Test `internal/agent/fetch/fetch_test.go`.

**Interfaces — Produces:** `fetch.Get(ctx, c *client.Client, sha256 string, size int64, dst string) error`: descarga `/agent/v1/files/{sha}` a `dst.part`, reanuda con `Range: bytes=<tamaño actual>-` si existe un `.part`, verifica tamaño y SHA-256 y renombra a `dst`; si el servidor no admite el rango (200 en vez de 206), empieza de cero; si el SHA no coincide, borra y devuelve `ErrChecksum`. Comprueba antes el espacio libre del disco (tamaño + 10 %) y devuelve `ErrNoSpace` con el espacio disponible en el mensaje.

- [ ] **Step 1: Pruebas que fallan**
```go
func TestGet_Fresh(t *testing.T)
func TestGet_ResumesWithRange(t *testing.T)     // Review Focus 4: servidor que corta a mitad la primera vez; la segunda pide Range y el resultado verifica
func TestGet_ServerIgnoresRange(t *testing.T)
func TestGet_BadChecksumDeletes(t *testing.T)
func TestGet_NoSpace(t *testing.T)              // función de espacio libre sustituida
```
- [ ] **Step 2–4:** FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat(agente): descargas con reanudación`.

---

### Task 7: Gestor de instalaciones del agente

**Files:** Create `internal/agent/apps/manager.go`, `internal/agent/apps/sources.go`, `internal/agent/apps/detect.go`, `internal/agent/apps/detect_windows.go`, `internal/agent/apps/detect_other.go`, `internal/agent/apps/scripts/winget.ps1`; Modify `internal/agent/policy/engine.go` (entrega la sección `apps` al gestor), `cmd/mdm-agent/main.go` (capacidad `apps.v1`); Test `internal/agent/apps/manager_test.go`, `internal/agent/apps/detect_test.go`, `internal/agent/apps/sources_test.go`.

**Interfaces — Produces:**
- `apps.Manager`: `apps.New(d Deps) *Manager` con `Deps{API (SendAppStatus), Fetch, Exec, Run, Inventory func() []protocol.Software, Window func() *protocol.Window, Now, DeviceID string, StatePath, CacheDir string, Log}`; `(*Manager).Desired(list []protocol.AppAssignment)`; `(*Manager).Run(ctx)`.
- Estado persistido (`apps.json`): intentos, próximo intento, retraso aleatorio ya cumplido, `retry_epoch` visto, por despliegue.
- Fuentes: `winget` (localiza `winget.exe` del paquete App Installer en `%ProgramFiles%\WindowsApps\Microsoft.DesktopAppInstaller_*_x64__8wekyb3d8bbwe`, la versión más alta; sin él → `not_applicable`), `apt` (`apt-get install -y <pkg>` / `apt-get remove -y`; «mantener actualizada» = `apt-get install --only-upgrade -y` cuando hay versión candidata mayor), `dnf` (análogo), `upload` (descarga y ejecuta la orden con `{file}` sustituido; variables `MDM_FILE`).
- Detección: `inventory` (busca en el inventario actual por `name_contains` sin distinguir mayúsculas y compara versión con `CompareVersions` del agente, que se mueve a `internal/protocol/version.go` para compartirla), `file` y `registry` (Windows), `script`; para winget/apt/dnf la del gestor.
- Reglas: `start` aún no alcanzado → `pending`; `respect_window` fuera de ventana → `waiting_window`; retraso aleatorio estable antes de la primera descarga; instalación correcta = código de éxito **y** detección positiva; reinicio según `reboot` (aviso con `shutdown`; `in_window` espera a la ventana).

- [ ] **Step 1: Pruebas que fallan**
```go
func TestManager_InstallsWhenMissing(t *testing.T)
func TestManager_AlreadyInstalledReportsInstalled(t *testing.T)
func TestManager_SuccessButNotDetected(t *testing.T)   // Review Focus 1
func TestManager_RetriesThenFails(t *testing.T)        // 15 min, 1 h, 4 h con reloj simulado; después failed hasta cambiar retry_epoch
func TestManager_StableJitter(t *testing.T)            // mismo equipo y despliegue → mismo retraso tras reiniciar
func TestManager_RespectsWindowAndStart(t *testing.T)
func TestManager_OneAtATime(t *testing.T)
func TestManager_Uninstall(t *testing.T)
func TestManager_RebootRequiredCodes(t *testing.T)     // 3010 → instalado con reboot_required; reinicio según política
func TestDetect_Inventory(t *testing.T)
func TestSources_CommandsAndParsing(t *testing.T)      // salidas grabadas de winget list, dpkg-query y rpm -q
func TestWingetLocate(t *testing.T)                    // elige la versión más alta de varias carpetas
```
- [ ] **Step 2–4:** FAIL, implementar, PASS; `GOOS=windows go vet` limpio; sintaxis de `winget.ps1` comprobada con `pwsh`.
- [ ] **Step 5:** Commit `feat(agente): instalación y desinstalación de software`.

---

### Task 8: Gestor de parches del agente

**Files:** Create `internal/agent/patches/manager.go`, `internal/agent/patches/linux.go`, `internal/agent/patches/windows.go`, `internal/agent/patches/scripts/scan.ps1`, `internal/agent/patches/scripts/install.ps1`; Modify `internal/agent/commands/commands.go` (órdenes nuevas), `cmd/mdm-agent/main.go` (capacidad `patches.v1`); Test `internal/agent/patches/*_test.go`.

**Interfaces — Produces:**
- `patches.Manager` con búsqueda diaria a hora estable por equipo, `ScanNow()`, `InstallNow()` (ignora la ventana) y, dentro de la ventana, instalación de `ApprovedUpdateIDs` (Windows) o de seguridad/todo (Linux), informe por actualización y reinicio según la política.
- Linux: `apt-get update` + `apt-get -s upgrade` (líneas `Inst <pkg> [<old>] (<new> <origin>)`, seguridad = origen que contiene `-security`); dnf: `dnf check-update` (código 100 = hay actualizaciones) y `dnf updateinfo list --security`. Reinicio pendiente: `/var/run/reboot-required` o `dnf needs-restarting -r` (código 1).
- Windows: `scan.ps1` (`Microsoft.Update.Session`, `IsInstalled=0 and IsHidden=0`, normaliza clasificación por el GUID de categoría, no por el nombre localizado) y `install.ps1` (por `UpdateID`, descarga e instala, devuelve resultado por actualización y `RebootRequired`); reinicio pendiente por las claves `RebootRequired` de WindowsUpdate y `RebootPending` de CBS.

- [ ] **Step 1: Pruebas que fallan**
```go
func TestParseAptSimulate(t *testing.T)             // salida grabada de Ubuntu 24.04 con paquetes de -security y de -updates
func TestParseDnfCheckUpdate(t *testing.T)          // código 100, líneas de obsoletos y continuaciones
func TestParseDnfSecurity(t *testing.T)
func TestParseWindowsScan(t *testing.T)             // JSON de scan.ps1; clasificación por GUID
func TestManager_DailyScanStableTime(t *testing.T)
func TestManager_InstallsOnlyInWindow(t *testing.T)
func TestManager_InstallNowIgnoresWindow(t *testing.T)
func TestManager_RebootPolicy(t *testing.T)         // never: no reinicia; notify: aviso y reinicio; in_window: solo dentro
```
- [ ] **Step 2–4:** FAIL, implementar, PASS; `pwsh` comprueba la sintaxis y ejecuta `scan.ps1` con un objeto COM simulado.
- [ ] **Step 5:** Commit `feat(agente): parches del sistema`.

---

### Task 9: Consola — catálogo de software

**Files:** Create `web/src/pages/Software.tsx`, `web/src/pages/AppEdit.tsx`, `web/src/components/Upload.tsx`, `web/src/components/DetectionEditor.tsx`; Modify `App.tsx`, `Layout.tsx`, `api/types.ts`, `lib/auditLabels.ts`; Test `web/src/pages/Software.test.tsx`, `web/src/components/Upload.test.tsx`.

**Produces:** menú «Software»; catálogo con origen y versiones; asistente para añadir (origen, identificador o subida, tipo de instalador, órdenes con valores por defecto visibles, regla de detección); subida con barra de progreso (`XMLHttpRequest` con `upload.onprogress`, cancelable); versiones con «Marcar como vigente».

- [ ] **Step 1: Pruebas que fallan**
```ts
it("el origen se limita según la plataforma (winget solo en Windows)")
it("un EXE exige las órdenes de instalación y desinstalación")
it("la subida muestra el progreso y, al terminar, rellena el SHA-256 de la versión")
it("cancelar la subida aborta la petición")
it("la regla de detección cambia sus campos según el tipo")
it("un operador no ve «Añadir aplicación»")
```
- [ ] **Step 2–4:** FAIL, implementar, PASS.
- [ ] **Step 5:** Revisión visual con capturas. **Step 6:** Commit `feat(consola): catálogo de software`.

---

### Task 10: Consola — despliegues

**Files:** Create `web/src/pages/Deployments.tsx`, `web/src/pages/DeploymentEdit.tsx`, `web/src/components/ManagedSoftwareTab.tsx`; Modify `DeviceDetail.tsx`; Test `web/src/pages/Deployments.test.tsx`.

**Produces:** lista de despliegues con recuento por estado; editor (aplicación, intención, versión, mantener actualizada, inicio, ventana, reinicio, asignaciones); vista de estado con tabla por equipo, salida de error y «Reintentar» (uno o todos los fallidos); pestaña «Software gestionado» en la ficha.

- [ ] **Step 1: Pruebas que fallan**
```ts
it("mantener actualizada solo se ofrece para winget, apt y dnf")
it("guardar envía la intención, las opciones y las asignaciones")
it("el estado muestra los recuentos y la salida del error de cada equipo")
it("Reintentar todos los fallidos llama a /retry sin device_id")
it("la ficha muestra los despliegues que le aplican con su estado")
```
- [ ] **Step 2–4:** FAIL, implementar, PASS. **Step 5:** Revisión visual. **Step 6:** Commit `feat(consola): despliegues`.

---

### Task 11: Consola — parches

**Files:** Create `web/src/pages/Patches.tsx`, `web/src/pages/PatchPolicyEdit.tsx`, `web/src/components/UpdatesTab.tsx`; Modify `DeviceDetail.tsx`, `Summary.tsx`, `pages/Settings.tsx` (antigüedad máxima de parches de seguridad); Test `web/src/pages/Patches.test.tsx`.

**Produces:** página «Parches» con actualizaciones (KB, título, clasificación, gravedad, equipos que la necesitan, instaladas, fallidas, decisión), filtros, aprobar/rechazar/volver a la regla; políticas de parches con editor de ventana (días y horas) y reinicio; pestaña «Actualizaciones» en la ficha con «Buscar ahora» e «Instalar ahora»; en el resumen, equipos con parches de seguridad pendientes y despliegues con fallos.

- [ ] **Step 1: Pruebas que fallan**
```ts
it("aprobar y rechazar envían la decisión; volver a la regla envía null")
it("un operador ve las actualizaciones pero no los botones de decisión")
it("el editor de ventana acepta una ventana que cruza la medianoche y la describe en texto («de 22:00 a 05:00 del día siguiente»)")
it("la pestaña Actualizaciones muestra pendientes, reinicio pendiente y lanza Buscar ahora")
```
- [ ] **Step 2–4:** FAIL, implementar, PASS. **Step 5:** Revisión visual. **Step 6:** Commit `feat(consola): parches`.

---

### Task 12: Extremo a extremo y documentación de la fase

**Files:** Modify `scripts/e2e.sh`, `docs/despliegue.md`, `README.md`; Create `docs/software.md`; Modify `docs/validacion-windows.md`.

- [ ] **Step 1:** Ampliar `e2e.sh`: sube un `.deb` mínimo construido en el momento con `dpkg-deb` (paquete vacío `mdm-e2e-demo`), lo despliega como obligatorio en todos los equipos y espera `installed` y `dpkg -s mdm-e2e-demo` en el contenedor; despliega `cowsay` (apt) como obligatorio y después como desinstalar; búsqueda de actualizaciones con apt y su llegada a la consola. `shellcheck`.
- [ ] **Step 2:** Ejecutar aquí la misma secuencia con servidor y agente reales (como en la fase 2), incluida la reanudación de una descarga cortando el proxy a mitad.
- [ ] **Step 3:** `docs/software.md` (orígenes, órdenes por defecto, detección, ventanas, aprobaciones, reinicios); despliegue: volumen `files` en las copias de seguridad y `MDM_FILES_DIR`; lista de comprobación de Windows: MSI, EXE con parámetros, winget como SYSTEM, búsqueda e instalación de Windows Update, aviso de reinicio.
- [ ] **Step 4:** Batería completa (`go vet` Linux y Windows, `go test -race ./...`, consola, `make build`, instalador, shellcheck) y prueba de carga.
- [ ] **Step 5:** Commit `docs: fase 3 (software y parches)`.
