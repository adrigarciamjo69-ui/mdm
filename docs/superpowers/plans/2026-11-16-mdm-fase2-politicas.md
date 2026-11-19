# MDM fase 2 (políticas, cumplimiento, grupos dinámicos, órdenes, scripts) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Que la plataforma mantenga configuraciones de seguridad, mida el cumplimiento, agrupe equipos por reglas, ejecute órdenes y scripts, custodie claves BitLocker y actualice sus agentes sola.

**Architecture:** Sobre el núcleo: el check-in sigue siendo el único canal (cada 60 s) y ahora devuelve la huella de la política efectiva, órdenes pendientes y, si procede, una actualización del agente. El servidor gana una cola de tareas en PostgreSQL, cifrado de secretos y ajustes globales. El agente gana un motor de políticas (cada 15 min), un ejecutor de órdenes en paralelo al check-in y la autoactualización con prueba previa.

**Tech Stack:** Lo del núcleo (Go 1.23, chi, pgx v5, goose, React 18 + TypeScript + Vite, Vitest). Sin dependencias nuevas salvo `golang.org/x/sys/windows` (ya presente) para objetos de trabajo en Windows.

**Spec:** `docs/superpowers/specs/2026-11-16-mdm-fase2-politicas-design.md`

## Global Constraints

- Todo lo de las Global Constraints del plan del núcleo sigue vigente (formato de errores, español, SQL solo en `store`, pruebas con `MDM_TEST_DATABASE_URL`).
- Migraciones nuevas numeradas `0002_…sql` en adelante, una por tarea que toque el esquema. Nunca se edita `0001_init.sql`.
- `MDM_SECRET_KEY`: 32 bytes en base64 estándar; obligatoria. Formato cifrado: `v1:<base64(nonce de 12 bytes ‖ texto cifrado)>`, AES-256-GCM.
- Cola: `max_attempts` 5; esperas 30 s, 2 min, 8 min, 32 min; bloqueo 5 min renovable; tareas terminadas se conservan 7 días.
- Política: evaluación al cambiar la huella y cada 900 s; cada ajuste como mucho 300 s (scripts personalizados: su `timeout_s`, máximo 3.600); mensaje por ajuste ≤ 1 KB.
- Cumplimiento: «desconocido» sin informe para la huella vigente en 24 h; `compliance.max_offline_days` por defecto 14.
- Grupos dinámicos: 1–20 condiciones, `match` `all` o `any`, sin anidamiento; recálculo completo cada hora.
- Órdenes: caducan a los 7 días; como mucho 10 por respuesta de check-in; `stdout` y `stderr` ≤ 64 KB cada uno (se conserva el final y se antepone «[salida recortada]»); lotes ≤ 1.000 equipos; salida conservada 30 días; el agente recuerda los últimos 500 ids iniciados.
- Scripts: intérpretes `powershell` (Windows) y `sh`, `bash`, `python3` (Linux); contenido ≤ 256 KB; `timeout_s` 1–3.600, por defecto 300; parámetros `^[A-Z][A-Z0-9_]{0,31}$`, variables `MDM_PARAM_<NOMBRE>`.
- Autoactualización: prueba previa `mdm-agent selftest` con 60 s de límite; código de salida tras sustituir: 75; binario anterior `mdm-agent.old` (`mdm-agent.old.exe` en Windows).
- Capacidades del agente de esta fase: `policy.v1`, `commands.v1`, `scripts.v1`, `bitlocker.v1`, `selfupdate.v1`. El servidor no entrega una orden cuyo tipo exige una capacidad que el agente no declara.

## Review Focus

1. **Exclusión e inclusión a la vez**: un equipo en «Portátiles» (incluido) y en «Servidores» (excluido) no recibe la política; la exclusión siempre gana. Prueba en la tarea 7.
2. **Agente antiguo sin capacidades** recibe una orden `run_script`: queda `pending` (no `sent`) hasta que el agente se actualice, y no caduca antes de tiempo por ello. Prueba en la tarea 9.
3. **Salida de un script binaria o enorme** (bytes no UTF-8, nulos, 50 MB): se guarda saneada y recortada con aviso, sin errores de base de datos. Pruebas en las tareas 9 y 14.
4. **La política cambia mientras el agente evalúa la anterior**: un informe con huella antigua se guarda pero no marca el equipo como «cumple»; el estado queda «desconocido» hasta el informe de la huella nueva. Prueba en la tarea 8.
5. **`MDM_SECRET_KEY` cambiada tras custodiar claves**: «Mostrar clave» responde un error claro («no se puede descifrar con la clave actual del servidor»), no un 500. Prueba en la tarea 11.

---

## File Structure

```
migrations/0002_jobs.sql … 0010_agent_pins.sql
internal/protocol/                      + policy.go, commands.go (tipos de la fase 2)
internal/server/secrets/                cifrado AES-GCM
internal/server/jobs/                   cola, trabajador, tareas periódicas
internal/server/settings/               ajustes globales tipados
internal/server/rules/                  reglas de grupos dinámicos y comparación de versiones (puro)
internal/server/policy/                 catálogo de ajustes (esquemas) y cálculo de la política efectiva (puro)
internal/server/compliance/             cálculo del estado de cumplimiento (puro)
internal/server/store/                  + groups_dynamic.go, policies.go, compliance.go, commands.go, scripts.go, recovery_keys.go, settings.go, jobs.go, agent_pins.go
internal/server/agentapi/               + policy.go, compliance.go, commands.go, recovery.go, update.go
internal/server/adminapi/               + policies.go, compliance.go, batches.go, scripts.go, recovery.go, settings.go, jobs.go
internal/server/app/                    + releases.go (versión y sumas del agente), tareas registradas
internal/agent/exec/                    ejecutor de procesos con tiempo límite y árbol de procesos
internal/agent/commands/                ejecutor de órdenes
internal/agent/policy/                  motor de políticas
internal/agent/settings/                implementaciones de ajustes: linux.go, windows.go, scripts/*.ps1
internal/agent/update/                  autoactualización
web/src/pages/                          + Policies.tsx, PolicyEdit.tsx, Activity.tsx, BatchDetail.tsx, Scripts.tsx, ScriptEdit.tsx, Settings.tsx
web/src/components/                     + RuleEditor.tsx, RunDialog.tsx, SettingForm.tsx, ComplianceBadge.tsx
```

---

### Task 1: Secretos del servidor

**Files:**
- Create: `internal/server/secrets/secrets.go`
- Modify: `internal/server/app/config.go`, `cmd/mdm-server/main.go`, `deploy/docker-compose.yml`, `deploy/.env.example`, `docs/despliegue.md`
- Test: `internal/server/secrets/secrets_test.go`, `internal/server/app/config_test.go`

**Interfaces:**
- Produces: `secrets.New(keyB64 string) (*Box, error)`; `(*Box).Seal(plain []byte) (string, error)`; `(*Box).Open(sealed string) ([]byte, error)`; `var secrets.ErrUndecryptable`; `app.Config.SecretKey string` leída de `MDM_SECRET_KEY`.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestBox_RoundTrip(t *testing.T)          // Open(Seal(x)) == x; dos Seal del mismo texto difieren; prefijo "v1:"
func TestNew_RejectsBadKeys(t *testing.T)      // "", base64 inválido, 16 bytes, 33 bytes → error que menciona "openssl rand -base64 32"
func TestOpen_WrongKey(t *testing.T)           // sellado con k1, abierto con k2 → errors.Is(err, ErrUndecryptable)
func TestOpen_Malformed(t *testing.T)          // "", "v2:…", "v1:!!", "v1:" + 10 bytes → ErrUndecryptable
func TestOpen_Tampered(t *testing.T)           // un byte cambiado en el texto cifrado → ErrUndecryptable
func TestLoadConfig_RequiresSecretKey(t *testing.T) // sin MDM_SECRET_KEY → error que lo nombra; con clave inválida → error
```

- [ ] **Step 2:** Run `go test ./internal/server/secrets/ ./internal/server/app/ -run 'Box|New_|Open_|SecretKey'` — Expected: FAIL.
- [ ] **Step 3: Implementar.** `serve` construye el `Box` al arrancar y lo pasa a quien lo necesite (tareas 11 y siguientes). `.env.example` incluye `MDM_SECRET_KEY=` con la orden para generarla; `docker-compose.yml` la pasa al servidor con `${MDM_SECRET_KEY:?…}`; la guía de despliegue explica que perderla impide leer las claves custodiadas y que debe guardarse junto a la copia de seguridad.
- [ ] **Step 4:** Run las mismas pruebas — Expected: PASS. Run `go test -short ./...` — Expected: PASS (la prueba de configuración del núcleo se ajusta para incluir la clave).
- [ ] **Step 5:** Commit `feat: cifrado de secretos del servidor`.

---

### Task 2: Cola de tareas

**Files:**
- Create: `migrations/0002_jobs.sql`, `internal/server/store/jobs.go`, `internal/server/jobs/jobs.go`
- Modify: `internal/server/app/janitor.go` (se sustituye por una tarea periódica), `cmd/mdm-server/main.go`
- Test: `internal/server/jobs/jobs_test.go`

**Interfaces:**
- Produces:
  - `store.EnqueueJob(ctx, q Querier, j NewJob) error` con `NewJob{Kind string; Payload any; RunAt time.Time; UniqueKey string; MaxAttempts int}`; `Querier` es una interfaz satisfecha por el pool y por `pgx.Tx`, para encolar dentro de otra transacción (la fase 6 la usa).
  - `(*Store).Tx(ctx, fn func(q Querier) error) error`.
  - `jobs.Runner`: `jobs.New(s *store.Store, now func() time.Time, log *slog.Logger) *Runner`; `(*Runner).Handle(kind string, h func(ctx context.Context, payload json.RawMessage) error)`; `(*Runner).Every(kind string, every time.Duration)`; `(*Runner).Run(ctx context.Context)` (bucle; consulta cada segundo si no hay trabajo); `(*Runner).RunOnce(ctx) (bool, error)` (para pruebas).
  - `(*Store).ListJobs(ctx, state string, page, perPage int) ([]Job, int, error)` con `state` `failed`, `pending` o `running`.
  - Tareas registradas en esta tarea: `cleanup` (cada hora: sesiones caducadas, actividad > 90 días, tareas terminadas > 7 días).

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestRunner_ExecutesAndFinishes(t *testing.T)     // encolar, RunOnce → handler llamado con el payload; finished_at fijado
func TestRunner_RetriesWithBackoff(t *testing.T)      // handler falla: run_at = now+30s; luego +2min, +8min; tras 5 intentos queda con last_error y sin reintentar
func TestRunner_NoTwoWorkersSameJob(t *testing.T)     // 2 Runners y 50 tareas en paralelo con -race: cada tarea se ejecuta exactamente una vez
func TestRunner_UniqueKeyDedupes(t *testing.T)        // dos Enqueue con la misma UniqueKey pendientes → una fila; tras terminar, se puede volver a encolar
func TestRunner_Every(t *testing.T)                   // Every("x", 1h): tras ejecutarse queda otra pendiente con run_at = now+1h; reiniciar el Runner no duplica
func TestRunner_LockExpiredIsRetaken(t *testing.T)    // tarea bloqueada con locked_until pasado se vuelve a tomar
func TestRunner_UnknownKindFails(t *testing.T)        // sin handler → error "tipo de tarea desconocido" y sin reintentos infinitos
func TestEnqueueInsideTransaction(t *testing.T)       // Enqueue dentro de Tx que hace rollback → no queda tarea
func TestCleanup_PurgesOldRows(t *testing.T)          // sustituye a TestJanitor_PurgesOldRows del núcleo, mismas comprobaciones + tareas terminadas de hace 8 días
```

- [ ] **Step 2:** Run `go test ./internal/server/jobs/` — Expected: FAIL.
- [ ] **Step 3: Implementar.** Tomar una tarea: `UPDATE jobs SET locked_until = now+5min, attempts = attempts+1 WHERE id = (SELECT id FROM jobs WHERE finished_at IS NULL AND run_at <= $now AND (locked_until IS NULL OR locked_until < $now) ORDER BY run_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING …`. Mientras el handler corre, una gorutina renueva `locked_until` cada minuto. Un pánico en el handler cuenta como fallo.
- [ ] **Step 4:** Run `go test -race ./internal/server/jobs/ ./internal/server/app/` — Expected: PASS.
- [ ] **Step 5:** Commit `feat: cola de tareas en segundo plano`.

---

### Task 3: Ajustes globales

**Files:**
- Create: `migrations/0003_settings.sql`, `internal/server/store/settings.go`, `internal/server/settings/settings.go`, `internal/server/adminapi/settings.go`
- Test: `internal/server/settings/settings_test.go`, `internal/server/adminapi/settings_test.go`

**Interfaces:**
- Produces: `settings.Values{AgentAutoUpdate bool; MaxOfflineDays int; MinAgentVersion string; MinOSVersion map[string]string}` con valores por defecto (`true`, `14`, `""`, vacío); `settings.Load(ctx, s *store.Store) (Values, error)` (fusiona lo guardado con los valores por defecto); `settings.Save(ctx, s, v Values, by uuid.UUID) error` con validación (días 1–365; versiones con formato de versión o vacías); rutas `GET /settings` (lectura) y `PUT /settings` (administrador, auditoría `settings.updated` con los campos cambiados). Las fases siguientes añaden campos a `Values`.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestLoad_Defaults(t *testing.T)            // tabla vacía → valores por defecto
func TestSave_Validation(t *testing.T)          // MaxOfflineDays 0 o 400 → error; MinAgentVersion "abc" → error; "1.2.0" ok
func TestSettingsAPI_Roles(t *testing.T)        // readonly GET 200, PUT 403; admin PUT 200 y auditoría con los campos cambiados
func TestSettingsAPI_PartialUpdate(t *testing.T) // PUT con un solo campo no borra los demás
```

- [ ] **Step 2:** Run — Expected: FAIL. **Step 3:** Implementar (`settings` guarda una fila por clave). **Step 4:** Run — Expected: PASS.
- [ ] **Step 5:** Commit `feat: ajustes globales`.

---

### Task 4: Protocolo de la fase 2

**Files:**
- Create: `internal/protocol/policy.go`, `internal/protocol/commands.go`
- Modify: `internal/protocol/protocol.go`
- Test: `internal/protocol/phase2_test.go`

**Interfaces:**
- Produces:
  - `CheckinRequest` gana `Capabilities []string` (`capabilities`) y `PolicyHash string` (`policy_hash`).
  - `CheckinResponse` gana `PolicyHash string` y `AgentUpdate *AgentUpdate` (`agent_update`, omitido si nulo); `AgentUpdate{Version, URL, SHA256 string}`.
  - `Command` gana `ExpiresAt time.Time`. Tipos: `CmdReboot = "reboot"`, `CmdShutdown`, `CmdRefreshInventory`, `CmdEvaluatePolicies`, `CmdRunScript`, `CmdRetire`. Cargas: `PowerPayload{DelayS int; Message string}`, `ScriptPayload{Interpreter, Content string; TimeoutS int; Env map[string]string; ScriptName string; Version int}`.
  - `EffectivePolicy{Hash string; EvaluateIntervalS int; Settings []PolicySetting; EscrowedKeyIDs []string}`, `PolicySetting{PolicyID, Key, Mode string; Params json.RawMessage}`.
  - `ComplianceReport{PolicyHash string; EvaluatedAt time.Time; Results []SettingResult}`, `SettingResult{PolicyID, Key, Status, Message string; Applied bool}`; estados `StatusCompliant = "compliant"`, `StatusNoncompliant`, `StatusError`, `StatusNotApplicable`.
  - `CommandResult{Status string; ExitCode *int; Stdout, Stderr, Error string; StartedAt, FinishedAt time.Time}`; `RecoveryKey{Volume, KeyID, RecoveryPassword string}`.
  - Capacidades `CapPolicy = "policy.v1"`, `CapCommands`, `CapScripts`, `CapBitLocker`, `CapSelfUpdate`; `RequiredCapability(cmdType string) string` (`run_script` → `scripts.v1`; el resto → `commands.v1`).
  - `PolicyHash(settings []PolicySetting, escrowed []string) string`: SHA-256 del JSON canónico (ajustes ordenados por `Key` y luego `PolicyID`; ids custodiados ordenados).
  - `SettingKeys map[string][]string`: claves del catálogo por plataforma (`windows`, `linux`), las 16 de la tabla 5.2 de la especificación. Es la única fuente compartida: el catálogo del servidor (tarea 7) y los ajustes del agente (tareas 15 y 16) comprueban en sus pruebas que cubren exactamente estas claves.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestPolicyHash_OrderIndependent(t *testing.T)  // mismo contenido en otro orden → misma huella
func TestPolicyHash_ParamsMatter(t *testing.T)      // cambiar un parámetro o un modo cambia la huella
func TestCheckinResponse_OmitsNullUpdate(t *testing.T) // sin AgentUpdate no aparece "agent_update"; commands sigue siendo []
func TestRequiredCapability(t *testing.T)
func TestJSONNames_Phase2(t *testing.T)             // policy_hash, capabilities, evaluate_interval_s, escrowed_key_ids, exit_code, recovery_password, expires_at
```

- [ ] **Step 2–4:** Run `go test ./internal/protocol/` — FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat: protocolo de la fase 2`.

---

### Task 5: Reglas de grupos dinámicos (lógica pura)

**Files:**
- Create: `internal/server/rules/rules.go`, `internal/server/rules/version.go`
- Test: `internal/server/rules/rules_test.go`, `internal/server/rules/version_test.go`

**Interfaces:**
- Produces:
  - `rules.Rule{Match string; Conditions []Condition}`, `Condition{Field, Op string; Value json.RawMessage}`.
  - `rules.Validate(r Rule) error` (mensajes en español; campos y operadores de la tabla del apartado 7 de la especificación; 1–20 condiciones).
  - `rules.Facts{Platform, Group, OSName, OSVersion, AgentVersion, Hostname, Manufacturer, Model, AssignedUser, Domain, Compliance, Status string; Tags []string; Software []string; LastSeenDays int}` (`Group` es el id del grupo principal o vacío).
  - `rules.Matches(r Rule, f Facts) bool`.
  - `rules.CompareVersions(a, b string) int`: compara por segmentos numéricos separados por `.`, `-` o `_`; segmentos no numéricos se comparan como texto; segmentos ausentes valen 0.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestCompareVersions(t *testing.T) {
    cases := []struct{ a, b string; want int }{
        {"10.0.26100", "10.0.22631", 1}, {"10.0.9", "10.0.10", -1}, {"24.04", "24.04.0", 0},
        {"1.0.0-rc1", "1.0.0-rc2", -1}, {"", "1", -1}, {"5.2.21-2ubuntu4", "5.2.21-2ubuntu10", -1},
    }
}
func TestMatches_EachFieldAndOp(t *testing.T)  // tabla con un caso positivo y uno negativo por cada (campo, operador) admitido
func TestMatches_CaseInsensitiveText(t *testing.T) // hostname contains "conta" coincide con "PC-CONTA-07"
func TestMatches_AllVsAny(t *testing.T)
func TestMatches_SoftwareInstalled(t *testing.T)  // "firefox" contains en ["Mozilla Firefox (x64 es-ES)"] → true; not_installed → false
func TestValidate(t *testing.T)                   // 0 y 21 condiciones, campo desconocido, operador no admitido para el campo (os_version contains), valor de tipo erróneo (last_seen_days "x") → error con mensaje
```

- [ ] **Step 2–4:** Run `go test ./internal/server/rules/` — FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat: reglas de grupos dinámicos`.

---

### Task 6: Grupos dinámicos en el servidor

**Files:**
- Create: `migrations/0004_dynamic_groups.sql`, `internal/server/store/groups_dynamic.go`, `internal/server/adminapi/groups_dynamic.go`
- Modify: `internal/server/store/groups.go`, `internal/server/adminapi/groups.go`, `internal/server/agentapi/inventory.go`, `internal/server/adminapi/devices.go` (recálculo tras cambios), `internal/server/store/devices_query.go` (filtro por grupo dinámico)
- Test: `internal/server/store/groups_dynamic_test.go`, `internal/server/adminapi/groups_dynamic_test.go`

**Interfaces:**
- Consumes: `rules` (tarea 5), `jobs` (tarea 2).
- Produces:
  - Esquema: `groups.kind` (`static` por defecto, `dynamic`), `groups.rule` (`jsonb`, nulo en estáticos), tabla `group_members` (`group_id`, `device_id`, clave primaria compuesta, borrado en cascada desde ambos lados).
  - `(*Store).DeviceFacts(ctx, id) (rules.Facts, error)`; `(*Store).AllDeviceFacts(ctx) (map[uuid.UUID]rules.Facts, error)`.
  - `(*Store).RecomputeDevice(ctx, deviceID) (changed bool, err error)` y `(*Store).RecomputeGroup(ctx, groupID) error`; `(*Store).RecomputeAll(ctx) error`.
  - `(*Store).DeviceGroupIDs(ctx, deviceID) ([]uuid.UUID, error)`: grupo principal + grupos dinámicos (lo usa la tarea 7).
  - Tarea `groups.recompute` cada hora y con payload `{group_id}` o `{device_id}` bajo demanda.
  - API: `POST /groups` y `PATCH /groups/{id}` aceptan `kind` y `rule` (el tipo no se puede cambiar tras crear; un dinámico no se puede asignar como grupo principal); `POST /groups/preview` con `{rule}` → `{count, sample: [{id, hostname}]}` (10 primeros por nombre); `GET /groups` añade `kind` y `device_count` real de cada tipo. `GET /devices?group_id=` funciona con ambos tipos.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestDynamicGroup_MembershipComputedOnSave(t *testing.T)  // crear grupo con regla platform eq linux → miembros = los Linux sembrados
func TestDynamicGroup_RecomputeOnInventory(t *testing.T)     // un equipo instala "Firefox" (inventario nuevo) → entra en el grupo "software installed firefox" sin esperar a la tarea horaria
func TestDynamicGroup_RecomputeOnDevicePatch(t *testing.T)   // añadir etiqueta "vip" → entra en "tag has vip"
func TestDynamicGroup_CannotBePrimary(t *testing.T)          // PATCH /devices/{id} con group_id de un dinámico → 400
func TestDynamicGroup_KindImmutable(t *testing.T)            // PATCH kind → 400
func TestDynamicGroup_InvalidRule(t *testing.T)              // regla inválida → 400 con el mensaje de rules.Validate
func TestGroupsPreview(t *testing.T)                         // count y 10 muestras ordenadas; no crea nada
func TestDevicesFilterByDynamicGroup(t *testing.T)
func TestRecomputeAll_Job(t *testing.T)                      // la tarea recalcula todos los grupos y elimina miembros que ya no cumplen
func TestDynamicGroup_RoleOperator(t *testing.T)             // readonly 403 al crear; operator 201
```

- [ ] **Step 2–4:** Run `go test ./internal/server/store/ ./internal/server/adminapi/ -run 'Dynamic|Preview|Recompute'` — FAIL, implementar, PASS. El recálculo de un equipo se encola como tarea tras guardar inventario o ficha (no se hace dentro de la petición del agente), salvo en `RecomputeDevice` llamado explícitamente desde las pruebas.
- [ ] **Step 5:** Commit `feat: grupos dinámicos`.

---

### Task 7: Políticas y política efectiva

**Files:**
- Create: `migrations/0005_policies.sql`, `internal/server/policy/catalog.go`, `internal/server/policy/effective.go`, `internal/server/store/policies.go`, `internal/server/adminapi/policies.go`
- Test: `internal/server/policy/catalog_test.go`, `internal/server/policy/effective_test.go`, `internal/server/adminapi/policies_test.go`

**Interfaces:**
- Consumes: `protocol.PolicySetting`, `protocol.PolicyHash` (tarea 4); `DeviceGroupIDs` (tarea 6).
- Produces:
  - Esquema: `policies` (`id`, `name` único sin distinguir mayúsculas, `description`, `platform`, `priority` único, `settings` `jsonb`, `created_by`, `created_at`, `updated_at`), `policy_assignments` (`policy_id`, `group_id` nulo = todos, `exclude`).
  - `policy.Catalog`: `[]Def{Key, Platform, Title, Description string; AuditOnly bool; Schema ParamSchema}` con las 16 claves de la tabla 5.2 de la especificación; `policy.ValidateSetting(platform string, s Setting) error` (clave existente para la plataforma, `mode` permitido, parámetros dentro de rango).
  - `policy.Effective(policies []AssignedPolicy, deviceGroups []uuid.UUID, platform string, escrowed []string) (protocol.EffectivePolicy, []Conflict)` con `AssignedPolicy{ID uuid.UUID; Priority int; Platform string; Settings []Setting; Assignments []Assignment}`; `Conflict{Key string; Winner, Loser uuid.UUID}`.
  - `(*Store).EffectivePolicyFor(ctx, deviceID) (protocol.EffectivePolicy, []policy.Conflict, error)`.
  - API: `GET/POST /policies`, `GET/PATCH/DELETE /policies/{id}`, `PUT /policies/{id}/assignments`, `GET /policies/{id}/devices` (equipos a los que aplica, con su estado de cumplimiento cuando exista), `GET /policy-catalog?platform=`, `POST /policies/reorder` (`{ids: [...]}` fija las prioridades en ese orden).

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestCatalog_AllSpecKeysPresent(t *testing.T)     // las claves del catálogo son exactamente protocol.SettingKeys, con su plataforma y AuditOnly correctos
func TestValidateSetting(t *testing.T)                // screen_lock minutes 0 y 61 → error; linux.disk_encryption enforce → error; clave de otra plataforma → error
func TestEffective_PriorityWins(t *testing.T)         // dos políticas con windows.screen_lock 5 y 15: gana la de menor número; un Conflict
func TestEffective_ExcludeBeatsInclude(t *testing.T)  // Review Focus 1: incluido por un grupo y excluido por otro → la política no aplica
func TestEffective_AllDevices(t *testing.T)           // asignación sin group_id aplica a todos los de la plataforma
func TestEffective_OtherPlatformIgnored(t *testing.T)
func TestEffective_HashStable(t *testing.T)           // mismo resultado → misma huella; reordenar asignaciones no la cambia
func TestPoliciesAPI_CRUDAndRoles(t *testing.T)       // operator 403 al crear; admin 201; nombre repetido 409; ajuste inválido 400 con mensaje
func TestPoliciesAPI_Reorder(t *testing.T)
func TestPoliciesAPI_Audit(t *testing.T)              // policy.created, policy.updated, policy.assigned, policy.deleted
func TestPolicyDevicesEndpoint(t *testing.T)
```

- [ ] **Step 2–4:** Run `go test ./internal/server/policy/ ./internal/server/adminapi/ -run 'Catalog|Validate|Effective|Polic'` — FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat: políticas y política efectiva`.

---

### Task 8: Política en la API de agentes y cumplimiento

**Files:**
- Create: `migrations/0006_compliance.sql`, `internal/server/compliance/compliance.go`, `internal/server/store/compliance.go`, `internal/server/agentapi/policy.go`, `internal/server/agentapi/compliance.go`, `internal/server/adminapi/compliance.go`
- Modify: `internal/server/agentapi/checkin.go`, `internal/server/store/devices_query.go`, `internal/server/store/summary.go`, `internal/server/adminapi/devices.go`
- Test: `internal/server/compliance/compliance_test.go`, `internal/server/agentapi/policy_test.go`, `internal/server/adminapi/compliance_test.go`

**Interfaces:**
- Consumes: tareas 3, 4 y 7.
- Produces:
  - Esquema: `device_compliance` y `device_setting_results` según el apartado 6 de la especificación; `devices.capabilities` (`text[]`).
  - `compliance.Compute(in Input) Result` con `Input{Effective protocol.EffectivePolicy; Report *StoredReport; Device store.Device; Settings settings.Values; Now time.Time}` y `Result{State string ("compliant", "noncompliant", "unknown"); Reasons []string}`.
  - Check-in: guarda `capabilities`; responde `policy_hash` de `EffectivePolicyFor`.
  - `GET /agent/v1/policy`; `POST /agent/v1/compliance` (≤ 256 KB; recorta mensajes a 1 KB; guarda resultados con la huella recibida; recalcula el estado).
  - Tarea `compliance.recompute` cada 15 min.
  - Consola: campo `compliance` en el listado y la ficha; filtro `compliance=compliant|noncompliant|unknown`; `GET /devices/{id}/compliance` → `{state, noncompliant_since, evaluated_at, results: [{policy_id, policy_name, key, title, status, message, applied, evaluated_at}], conflicts, server_reasons}`; el resumen gana `compliance: {compliant, noncompliant, unknown}`.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestCompute_AllCompliant(t *testing.T)
func TestCompute_NotApplicableCounts(t *testing.T)       // not_applicable no rompe el cumplimiento
func TestCompute_ErrorIsNoncompliant(t *testing.T)
func TestCompute_StaleHashIsUnknown(t *testing.T)        // Review Focus 4: informe de huella anterior → unknown, no compliant
func TestCompute_NoReportIn24hIsUnknown(t *testing.T)
func TestCompute_NoPoliciesIsUnknown(t *testing.T)
func TestCompute_ServerRules(t *testing.T)               // offline 15 días con límite 14 → noncompliant con motivo; agente 1.0.0 con mínimo 1.1.0 → noncompliant; OS mínimo
func TestCompute_NoncompliantSinceKept(t *testing.T)     // sigue sin cumplir → misma fecha; pasa a cumplir → nula
func TestCheckin_ReturnsPolicyHashAndStoresCapabilities(t *testing.T)
func TestGetPolicy_ReturnsEffective(t *testing.T)        // incluye escrowed_key_ids vacío y evaluate_interval_s 900
func TestPostCompliance_Validates(t *testing.T)          // estado desconocido → 400; 300 KB → 413; mensaje de 5 KB se recorta a 1 KB
func TestPostCompliance_UpdatesState(t *testing.T)
func TestDevicesFilterByCompliance(t *testing.T)
func TestDeviceComplianceEndpoint(t *testing.T)          // nombres de política y títulos del catálogo; conflictos
func TestSummaryIncludesCompliance(t *testing.T)
```

- [ ] **Step 2–4:** Run — FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat: cumplimiento`.

---

### Task 9: Órdenes y lotes en el servidor

**Files:**
- Create: `migrations/0007_commands.sql`, `internal/server/store/commands.go`, `internal/server/agentapi/commands.go`, `internal/server/adminapi/batches.go`
- Modify: `internal/server/agentapi/checkin.go`
- Test: `internal/server/store/commands_test.go`, `internal/server/agentapi/commands_test.go`, `internal/server/adminapi/batches_test.go`

**Interfaces:**
- Consumes: tareas 2, 4 y 6.
- Produces:
  - Esquema: columnas nuevas de `commands` (apartado 8.2) y tabla `command_batches`.
  - `(*Store).CreateBatch(ctx, b NewBatch, deviceIDs []uuid.UUID) (Batch, error)` con `NewBatch{Type string; Params json.RawMessage; CreatedBy uuid.UUID; TargetDescription string; ExpiresIn time.Duration}`; error si más de 1.000 equipos.
  - `(*Store).NextCommands(ctx, deviceID, caps []string, now, limit int) ([]protocol.Command, error)`: devuelve `pending` y `sent` no caducadas cuyo tipo esté soportado por `caps`, las marca `sent` (Review Focus 2: las no soportadas no cambian de estado).
  - `(*Store).StartCommand`, `(*Store).FinishCommand(ctx, deviceID, cmdID, protocol.CommandResult)`: `stdout` y `stderr` pasan por `store.CleanOutput` (sanea como `CleanString` pero con límite de 64 KB, conservando el final y anteponiendo `[salida recortada]\n`).
  - `(*Store).ExpireCommands(ctx, now) (int, error)` y tarea `commands.expire` cada 5 min.
  - `(*Store).CancelCommand`.
  - El payload de `run_script` lo construye el servidor al crear el lote a partir de `script_version_id` y `params` (tarea 10 aporta las versiones); esta tarea solo exige que `Params` sea JSON válido para tipos sin parámetros y para `reboot`/`shutdown` (`delay_s` 0–3.600, `message` ≤ 200 caracteres).
  - Al terminar con éxito una orden `retire`, el equipo se revoca y se registra `device.retired`.
  - API de agentes: `POST /commands/{id}/start`, `POST /commands/{id}/result` (≤ 300 KB; el id debe ser de ese equipo: si no, 404).
  - API de consola: `POST /batches` (`{type, params, device_ids | group_id}`; rol según el tipo, tabla 8.1 de la especificación), `GET /batches`, `GET /batches/{id}` (con recuento por estado y filas por equipo), `GET /devices/{id}/commands`, `GET /commands/{id}` (con salida), `POST /commands/{id}/cancel`.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestNextCommands_MarksSentAndRedelivers(t *testing.T)   // primera llamada → pending pasa a sent; segunda llamada sin start → la vuelve a entregar
func TestNextCommands_Limit10OldestFirst(t *testing.T)
func TestNextCommands_SkipsUnsupported(t *testing.T)        // Review Focus 2: run_script sin "scripts.v1" queda pending; reboot con "commands.v1" se entrega
func TestNextCommands_SkipsExpired(t *testing.T)
func TestFinishCommand_SanitizesOutput(t *testing.T)        // Review Focus 3: stdout con \x00, bytes inválidos y 200 KB → válido UTF-8, ≤ 64 KB + aviso, conserva el final
func TestFinishCommand_WrongDevice404(t *testing.T)
func TestFinishCommand_Idempotent(t *testing.T)             // segundo resultado igual → 204 sin cambiar finished_at
func TestExpireCommands(t *testing.T)                       // pending de hace 8 días → expired; running con timeout+10 min superado → failed "sin respuesta del equipo"
func TestRetireRevokesOnSuccess(t *testing.T)
func TestBatches_RolesByType(t *testing.T)                  // operator: reboot 201, run_script 403, retire 403; readonly 403 siempre
func TestBatches_GroupTargetAndLimit(t *testing.T)          // group_id resuelve miembros (estático o dinámico); 1.001 equipos → 400
func TestBatches_ProgressCounts(t *testing.T)
func TestCancel_OnlyPendingOrSent(t *testing.T)             // running → 409
func TestBatches_Audit(t *testing.T)                        // batch.created con tipo y número de equipos; command.cancelled
```

- [ ] **Step 2–4:** Run — FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat: órdenes y lotes`.

---

### Task 10: Biblioteca de scripts

**Files:**
- Create: `migrations/0008_scripts.sql`, `internal/server/store/scripts.go`, `internal/server/adminapi/scripts.go`
- Modify: `internal/server/adminapi/batches.go`
- Test: `internal/server/adminapi/scripts_test.go`

**Interfaces:**
- Consumes: tarea 9.
- Produces:
  - Esquema `scripts` y `script_versions` (apartado 9).
  - API (administrador para escribir; lectura para ver): `GET/POST /scripts`, `GET/PATCH /scripts/{id}` (cambiar metadatos), `POST /scripts/{id}/versions` (`{content}` → nueva versión), `GET /scripts/{id}/versions`, `GET /scripts/{id}/versions/{version}`, `POST /scripts/{id}/archive`.
  - `POST /batches` con `type: "run_script"` y `params: {script_id, version (0 = vigente), params: {NOMBRE: valor}}` construye `protocol.ScriptPayload` con `Env` = `MDM_PARAM_<NOMBRE>`; valida obligatorios, nombres desconocidos (400) y plataforma (los equipos de otra plataforma se excluyen del lote y la respuesta lo indica en `skipped`).
  - Auditoría `script.created|updated|archived`.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestScripts_CreateAndVersion(t *testing.T)        // crear → versión 1; nueva versión → 2; la 1 sigue legible
func TestScripts_Validation(t *testing.T)              // intérprete de otra plataforma, 257 KB, timeout 0 o 3.601, parámetro "nombre" (minúsculas) → 400
func TestScripts_ReadonlyCanReadNotWrite(t *testing.T)
func TestRunScript_BuildsPayload(t *testing.T)         // payload con content de la versión pedida, timeout, Env MDM_PARAM_RUTA, nombre y versión
func TestRunScript_ParamsNeverInContent(t *testing.T)  // el valor "$(rm -rf /)" aparece solo en Env, nunca en Content
func TestRunScript_MissingRequiredParam(t *testing.T)  // 400
func TestRunScript_SkipsOtherPlatform(t *testing.T)    // script de Linux sobre un grupo mixto → solo Linux; skipped lista los Windows
func TestRunScript_ArchivedRejected(t *testing.T)
```

- [ ] **Step 2–4:** Run — FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat: biblioteca de scripts`.

---

### Task 11: Custodia de claves BitLocker

**Files:**
- Create: `migrations/0009_recovery_keys.sql`, `internal/server/store/recovery_keys.go`, `internal/server/agentapi/recovery.go`, `internal/server/adminapi/recovery.go`
- Modify: `internal/server/store/policies.go` (`EscrowedKeyIDs` en la política efectiva)
- Test: `internal/server/agentapi/recovery_test.go`, `internal/server/adminapi/recovery_test.go`

**Interfaces:**
- Consumes: `secrets.Box` (tarea 1), política efectiva (tarea 7).
- Produces: `POST /agent/v1/recovery-keys` (sella la contraseña; sustituye la del mismo `volume` y `key_id`); `GET /api/v1/devices/{id}/recovery-keys` → `[{volume, key_id, reported_at}]` (lectura, sin la clave); `POST /api/v1/devices/{id}/recovery-keys/{key_id}/reveal` (administrador) → `{recovery_password}`, auditoría `recovery_key.viewed`; la política efectiva incluye en `escrowed_key_ids` los `key_id` custodiados del equipo.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestRecoveryKey_StoredEncrypted(t *testing.T)    // la columna no contiene la contraseña en claro
func TestRecoveryKey_ReplacesSameKeyID(t *testing.T)
func TestRecoveryKey_ValidatesFormat(t *testing.T)    // la contraseña de BitLocker son 8 grupos de 6 cifras; otro formato → 400
func TestRecoveryKey_ListHidesSecret(t *testing.T)
func TestReveal_AdminOnlyAndAudited(t *testing.T)     // operator 403; admin 200 con la contraseña; auditoría recovery_key.viewed con device y key_id
func TestReveal_WrongServerKey(t *testing.T)          // Review Focus 5: custodiada con una clave, servidor con otra → 409 con «no se puede descifrar con la clave actual del servidor»
func TestEffectivePolicy_EscrowedKeyIDs(t *testing.T) // cambia la huella al custodiar una clave nueva
```

- [ ] **Step 2–4:** Run — FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat: custodia de claves BitLocker`.

---

### Task 12: Actualización del agente en el servidor

**Files:**
- Create: `migrations/0010_agent_pins.sql`, `internal/server/app/releases.go`, `internal/server/store/agent_pins.go`, `internal/server/agentapi/update.go`, `internal/server/adminapi/agent_pins.go`
- Modify: `internal/server/agentapi/checkin.go`, `internal/server/agentapi/agentapi.go` (`Deps.Releases`)
- Test: `internal/server/app/releases_test.go`, `internal/server/agentapi/update_test.go`

**Interfaces:**
- Consumes: ajustes (tarea 3).
- Produces:
  - `app.LoadReleases(dir, serverVersion string) (*Releases, error)`: lee `SHA256SUMS`; `(*Releases).For(platform, arch string) (protocol.AgentUpdate, bool)` con `URL = /downloads/mdm-agent-<platform>-<arch>[.exe]` y `Version = serverVersion`. Sin fichero de sumas, no se ofrecen actualizaciones (se registra un aviso al arrancar).
  - `agent_pins` (`group_id` primaria, `version`); API `GET/PUT/DELETE /agent-pins/{group_id}` (administrador). Un grupo fijado a una versión distinta de la distribuida no recibe actualización (esta fase solo distribuye una versión: fijar sirve para **congelar**).
  - Check-in: `agent_update` si `AgentAutoUpdate`, el agente declara `selfupdate.v1`, su versión es distinta de la distribuida, existe el binario para su plataforma y arquitectura, y su grupo principal no está fijado. Auditoría `agent.updated` (sin actor) cuando un equipo hace check-in con una versión distinta de la anterior.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestReleases_ParsesSums(t *testing.T)          // líneas "<sum>  mdm-agent-linux-amd64" y "<sum> *mdm-agent-windows-amd64.exe"
func TestReleases_MissingSumsFile(t *testing.T)     // sin fichero → For devuelve false, sin error fatal
func TestCheckin_OffersUpdate(t *testing.T)
func TestCheckin_NoUpdateWhen(t *testing.T)         // misma versión; auto_update desactivado; sin capacidad selfupdate.v1; grupo fijado; sin binario de su arquitectura
func TestCheckin_AuditsVersionChange(t *testing.T)
func TestAgentPins_AdminOnly(t *testing.T)
```

- [ ] **Step 2–4:** Run — FAIL, implementar, PASS. `app.NewHandler` carga las versiones de `MDM_DOWNLOADS_DIR` con la versión del binario del servidor.
- [ ] **Step 5:** Commit `feat: distribución de actualizaciones del agente`.

---

### Task 13: Ejecutor de procesos del agente

**Files:**
- Create: `internal/agent/exec/exec.go`, `internal/agent/exec/tree_linux.go`, `internal/agent/exec/tree_windows.go`, `internal/agent/exec/tree_other.go`, `internal/agent/exec/capture.go`
- Test: `internal/agent/exec/exec_test.go`, `internal/agent/exec/capture_test.go`

**Interfaces:**
- Produces:
  - `exec.Spec{Interpreter, Content string; Env map[string]string; Timeout time.Duration; Dir string}`.
  - `exec.Run(ctx context.Context, s Spec) Result` con `Result{ExitCode int; Stdout, Stderr string; TimedOut bool; Err error; Started, Finished time.Time}`.
  - Comportamiento: escribe el contenido en un temporal con permisos 0700 (Windows: mismo directorio protegido de la configuración), lo ejecuta con el intérprete (`powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File <f>`; `sh <f>`; `bash <f>`; `python3 <f>`), entorno = entorno del sistema + `Env`, y lo borra al terminar. Linux: `Setpgid` y, al agotarse el tiempo, `SIGKILL` al grupo. Windows: proceso asignado a un objeto de trabajo con `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`; al agotarse, se cierra el objeto.
  - `capture.go`: búfer que conserva como mucho 64 KB (los últimos) y cuenta lo descartado; la salida final antepone `[salida recortada]\n` si descartó algo.

- [ ] **Step 1: Escribir las pruebas que fallan** (Linux; las de Windows se compilan con `//go:build windows`)

```go
func TestRun_ExitCodeAndOutput(t *testing.T)          // sh: echo hola; echo error >&2; exit 3 → 3, "hola\n", "error\n"
func TestRun_EnvParams(t *testing.T)                  // Env MDM_PARAM_X="a b; rm" → el script lo imprime literal
func TestRun_TimeoutKillsTree(t *testing.T)           // script que lanza "sleep 300 &" y "sleep 300"; timeout 1 s → TimedOut, y ningún proceso sleep hijo sigue vivo (comprobar con pgrep del grupo)
func TestRun_TempFileRemoved(t *testing.T)
func TestRun_UnknownInterpreter(t *testing.T)         // error sin ejecutar nada
func TestCapture_KeepsTail(t *testing.T)              // Review Focus 3: 50 MB escritos → 64 KB finales con prefijo de aviso; memoria acotada
func TestRun_ContextCancel(t *testing.T)              // cancelar el contexto mata el árbol
```

- [ ] **Step 2–4:** Run `go test -race ./internal/agent/exec/` y `GOOS=windows go vet ./internal/agent/exec/` — FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat(agente): ejecutor de procesos con tiempo límite`.

---

### Task 14: Órdenes en el agente

**Files:**
- Create: `internal/agent/commands/commands.go`, `internal/agent/commands/power_linux.go`, `internal/agent/commands/power_windows.go`
- Modify: `internal/agent/client/client.go` (`StartCommand`, `FinishCommand`), `internal/agent/config/config.go` (`State.StartedCommands []string`), `internal/agent/loop/loop.go`
- Test: `internal/agent/commands/commands_test.go`, `internal/agent/loop/loop_test.go`

**Interfaces:**
- Consumes: `exec.Run` (tarea 13), `protocol` (tarea 4).
- Produces:
  - `commands.Runner`: `commands.New(d Deps) *Runner` con `Deps{API API; Exec func(ctx, exec.Spec) exec.Result; Power func(action string, delay time.Duration, message string) error; RefreshInventory func(); EvaluatePolicies func(); Retire func() error; State StateStore; Now func() time.Time; Log *slog.Logger}`; `(*Runner).Submit(cmds []protocol.Command)` (no bloquea); `(*Runner).Run(ctx)` (una orden a la vez en su propia gorutina).
  - Reglas: una orden cuyo id está en `StartedCommands` no se ejecuta de nuevo; si tiene un resultado guardado lo reenvía, si no envía `failed` «interrumpida por un reinicio del agente». La lista conserva los últimos 500 ids. Una orden caducada (`expires_at` pasado) se descarta sin ejecutar. Tipos desconocidos → `failed` «tipo de orden no admitido».
  - `power_*`: Linux `shutdown -r +N "mensaje"` / `shutdown -h`; Windows `shutdown /r /t N /c "mensaje"` / `/s`.
  - `retire`: envía el resultado `succeeded` **antes** de desinstalar (no habría credencial después); luego ejecuta la desinstalación (`service.Uninstall` + borrar configuración) en segundo plano.
  - El bucle (`loop`) declara las capacidades `policy.v1`, `commands.v1`, `scripts.v1`, `bitlocker.v1` (Windows), `selfupdate.v1` y pasa `resp.Commands` a `Submit`.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestRunner_ExecutesScriptAndReports(t *testing.T)    // start y luego result con exit_code, stdout y fechas
func TestRunner_NoRepeatAfterRestart(t *testing.T)        // StartedCommands contiene el id → no se ejecuta; se envía failed "interrumpida…"
func TestRunner_ResendsStoredResult(t *testing.T)         // id con resultado guardado (resultado no confirmado por el servidor) → se reenvía sin ejecutar
func TestRunner_DuplicateInSameDelivery(t *testing.T)     // la misma orden entregada dos veces seguidas → una ejecución
func TestRunner_RemembersLast500(t *testing.T)
func TestRunner_ExpiredSkipped(t *testing.T)
func TestRunner_UnknownType(t *testing.T)
func TestRunner_RetireReportsFirst(t *testing.T)          // orden de llamadas: FinishCommand antes que Retire
func TestRunner_DoesNotBlockSubmit(t *testing.T)          // Submit vuelve al instante aunque haya un script de 10 s en curso
func TestLoop_DeclaresCapabilities(t *testing.T)
func TestLoop_ChecksInWhileScriptRuns(t *testing.T)       // con un script largo en curso, los check-ins siguen a su ritmo
```

- [ ] **Step 2–4:** Run `go test -race ./internal/agent/...` — FAIL, implementar, PASS. `GOOS=windows go vet ./internal/agent/...` limpio.
- [ ] **Step 5:** Commit `feat(agente): ejecución de órdenes`.

---

### Task 15: Motor de políticas y ajustes de Linux

**Files:**
- Create: `internal/agent/policy/engine.go`, `internal/agent/settings/settings.go`, `internal/agent/settings/linux.go`, `internal/agent/settings/custom.go`
- Modify: `internal/agent/client/client.go` (`GetPolicy`, `SendCompliance`), `internal/agent/loop/loop.go`
- Test: `internal/agent/policy/engine_test.go`, `internal/agent/settings/linux_test.go`, `internal/agent/settings/custom_test.go`

**Interfaces:**
- Consumes: `exec.Run` (tarea 13), protocolo (tarea 4).
- Produces:
  - `settings.Setting` interfaz: `Check(ctx) (compliant bool, detail string, err error)`; `Apply(ctx) error`; `settings.ErrNotApplicable`.
  - `settings.Build(platform string, s protocol.PolicySetting, deps Deps) (Setting, error)` con `Deps{Run runner; Exec func(ctx, exec.Spec) exec.Result; Root string}` (`Root` para leer ficheros en pruebas).
  - Linux: `linux.firewall` (ufw `status` / `enable`; firewalld `--state` / `systemctl enable --now firewalld`; ninguno → no aplicable), `linux.auto_security_updates` (Debian: paquete `unattended-upgrades` y `/etc/apt/apt.conf.d/20auto-upgrades` con `APT::Periodic::Unattended-Upgrade "1"`; RHEL: `dnf-automatic` con `upgrade_type = security` y temporizador activo), `linux.ssh` (`sshd -T` para comprobar; aplicar escribe `/etc/ssh/sshd_config.d/90-mdm.conf`, valida con `sshd -t` y si falla borra el fichero y devuelve error; recarga `sshd`), `linux.disk_encryption` (reutiliza `lsblk` del inventario), `linux.forbidden_packages` (dpkg/rpm), `custom.script`.
  - `policy.Engine`: `policy.New(d Deps) *Engine` con `Deps{API; Build; Now; Log; Platform string}`; `(*Engine).Wanted(hash string)` (lo llama el bucle tras cada check-in); `(*Engine).Run(ctx)` evalúa al cambiar la huella y cada `EvaluateIntervalS`; un ajuste con más de 300 s (o su `timeout_s`) se da por `error` «tiempo agotado»; `(*Engine).Hash() string` (la huella evaluada, que el check-in envía).

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestEngine_EvaluatesOnHashChange(t *testing.T)        // Wanted("h2") → GetPolicy y SendCompliance con policy_hash "h2"
func TestEngine_ReevaluatesEvery15Min(t *testing.T)        // reloj simulado
func TestEngine_EnforceAppliesAndRechecks(t *testing.T)    // Check false → Apply → Check true → compliant, applied true
func TestEngine_AuditDoesNotApply(t *testing.T)
func TestEngine_ApplyErrorIsError(t *testing.T)
func TestEngine_NotApplicable(t *testing.T)
func TestEngine_SettingTimeout(t *testing.T)
func TestEngine_PolicyDownloadFailsKeepsLast(t *testing.T) // GetPolicy falla → sigue evaluando la anterior; reintenta en el siguiente Wanted
func TestEngine_MessageTruncated(t *testing.T)             // detalle de 5 KB → 1 KB
func TestLinuxFirewall(t *testing.T)                       // salidas grabadas de ufw activo/inactivo, firewalld, ninguno
func TestLinuxSSH_ApplyValidatesAndRollsBack(t *testing.T) // sshd -t falla → fichero eliminado y error
func TestLinuxSSH_Check(t *testing.T)                      // sshd -T con "permitrootlogin yes" → no cumple con root_login false
func TestLinuxAutoUpdates(t *testing.T)                    // árbol de ficheros de prueba Debian y RHEL
func TestForbiddenPackages(t *testing.T)                   // detail lista los encontrados
func TestCustomScript(t *testing.T)                        // detección 0 → cumple; 1 y enforce → corrección y nueva detección
func TestAllLinuxKeysBuild(t *testing.T)                   // settings.Build acepta cada clave de protocol.SettingKeys["linux"] y custom.script
```

- [ ] **Step 2–4:** Run `go test -race ./internal/agent/...` — FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat(agente): motor de políticas y ajustes de Linux`.

---

### Task 16: Ajustes de Windows y custodia de BitLocker

**Files:**
- Create: `internal/agent/settings/windows.go`, `internal/agent/settings/windows_parse.go`, `internal/agent/settings/scripts/*.ps1` (uno por clave, con funciones `Test` y `Set`), `internal/agent/settings/embed_windows.go`
- Modify: `internal/agent/client/client.go` (`SendRecoveryKey`), `docs/validacion-windows.md`
- Test: `internal/agent/settings/windows_parse_test.go`

**Interfaces:**
- Consumes: `settings.Setting`, `exec.Run`.
- Produces:
  - Cada `.ps1` acepta `-Mode Test|Set` y parámetros con nombre, y escribe en la salida estándar un único JSON `{"compliant": bool, "detail": "…", "not_applicable": bool, "recovery": {"volume": "C:", "key_id": "…", "password": "…"} | null}`. `windows.go` los ejecuta con `exec.Run` (parámetros como variables de entorno, igual que los scripts) y `windows_parse.go` interpreta el JSON.
  - `windows.bitlocker`: `Test` con `Get-BitLockerVolume -MountPoint $env:SystemDrive`; `Set` con `Enable-BitLocker -RecoveryPasswordProtector -UsedSpaceOnly` (y protector TPM si existe; sin TPM → `not_applicable` «el equipo no tiene TPM»). Si `escrow_key` y el `key_id` del protector de contraseña no está en `EscrowedKeyIDs`, el ajuste devuelve la clave en `recovery` y el motor la envía con `SendRecoveryKey` antes del informe.
  - El resto según la tabla 5.2 de la especificación.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestParseWindowsResult(t *testing.T)           // JSON válido; salida con texto antes del JSON (avisos de PowerShell) → se toma la última línea JSON; salida vacía → error claro
func TestParseRecovery(t *testing.T)                // recovery presente/ausente
func TestBitLockerNeedsEscrow(t *testing.T)         // key_id ya custodiado → no se reenvía; nuevo → se envía
func TestAllWindowsKeysHaveScript(t *testing.T)     // cada clave de protocol.SettingKeys["windows"] tiene su .ps1 incrustado, y no sobra ninguno
```

- [ ] **Step 2–4:** Run `go test ./internal/agent/settings/` y `GOOS=windows GOARCH=amd64 go vet ./internal/agent/...` — FAIL, implementar, PASS. Comprobar sintaxis de cada `.ps1` con `pwsh -NoProfile -Command "[System.Management.Automation.Language.Parser]::ParseFile(…)"` si `pwsh` está disponible; si no, anotarlo en el libro.
- [ ] **Step 5:** Ampliar `docs/validacion-windows.md` con una casilla por ajuste (auditar y aplicar), la custodia y la vista de la clave en la consola.
- [ ] **Step 6:** Commit `feat(agente): ajustes de Windows y custodia de BitLocker`.

---

### Task 17: Autoactualización del agente

**Files:**
- Create: `internal/agent/update/update.go`, `internal/agent/update/swap_windows.go`, `internal/agent/update/swap_other.go`
- Modify: `cmd/mdm-agent/main.go` (orden `selftest`, código 75), `internal/agent/loop/loop.go`, `internal/agent/client/client.go` (`Download(ctx, url, dst) error`)
- Test: `internal/agent/update/update_test.go`, `cmd/mdm-agent/main_test.go`

**Interfaces:**
- Produces:
  - `update.Updater`: `update.New(d Deps) *Updater` con `Deps{Download func(ctx, url, dst string) error; SelfTest func(ctx, binPath string) error; Swap func(newPath, currentPath string) error; Exit func(code int); BinPath string; Dir string; Now func() time.Time; Log *slog.Logger}`; `(*Updater).Offer(u protocol.AgentUpdate)`; `(*Updater).CleanupOld()` (se llama tras el primer check-in correcto).
  - Rechazos recordados: una versión que falla la suma o la prueba previa no se reintenta hasta 24 h después.
  - `mdm-agent selftest`: carga la configuración, hace un check-in real con `policy_hash` vacío y termina 0 si responde 200; 1 en otro caso.
  - Swap: Linux renombra el binario en uso a `.old` y mueve el nuevo; Windows igual (renombrar un ejecutable en uso está permitido).

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestUpdater_HappyPath(t *testing.T)            // Download → verifica suma → SelfTest → Swap → Exit(75)
func TestUpdater_BadChecksum(t *testing.T)          // no SelfTest, no Swap; fichero temporal borrado; la misma versión se ignora 24 h
func TestUpdater_SelfTestFails(t *testing.T)        // no Swap; ignorada 24 h; tras 24 h se reintenta
func TestUpdater_SameVersionIgnored(t *testing.T)
func TestUpdater_OneAtATime(t *testing.T)           // dos Offer seguidas → una descarga
func TestCleanupOld(t *testing.T)
func TestCLI_SelftestExitCodes(t *testing.T)        // servidor de prueba 200 → 0; 401 → 1; sin configuración → 1
```

- [ ] **Step 2–4:** Run — FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat(agente): actualización automática con prueba previa`.

---

### Task 18: Consola — políticas y cumplimiento

**Files:**
- Create: `web/src/pages/Policies.tsx`, `web/src/pages/PolicyEdit.tsx`, `web/src/components/SettingForm.tsx`, `web/src/components/ComplianceBadge.tsx`, `web/src/lib/policyCatalog.ts`
- Modify: `web/src/api/types.ts`, `web/src/App.tsx`, `web/src/components/Layout.tsx`, `web/src/pages/Devices.tsx`, `web/src/pages/DeviceDetail.tsx`, `web/src/pages/Summary.tsx`
- Test: `web/src/pages/Policies.test.tsx`, `web/src/pages/PolicyEdit.test.tsx`, `web/src/pages/DeviceCompliance.test.tsx`

**Interfaces:**
- Consumes: rutas de las tareas 7, 8 y 11.
- Produces:
  - Menú «Políticas» (todos los roles). Rutas `/politicas`, `/politicas/nueva`, `/politicas/:id`.
  - Lista con nombre, plataforma, prioridad (reordenable con botones subir/bajar, administrador), número de ajustes, equipos que cumplen y no.
  - Editor: datos generales; ajustes elegidos del catálogo de la plataforma (`GET /policy-catalog`) con un formulario generado del esquema (`SettingForm`) y el modo (Auditar/Aplicar; Aplicar deshabilitado en los «solo auditar» con explicación); asignaciones (grupos incluidos, excluidos, «Todos los equipos»); pestaña de equipos afectados.
  - `ComplianceBadge` («Cumple», «No cumple», «Desconocido», con texto además de color).
  - Listado de dispositivos: columna y filtro de cumplimiento. Ficha: pestaña «Cumplimiento» con resultados por ajuste, conflictos, motivos del servidor y, en Windows, el estado de la clave BitLocker con «Mostrar clave» (administrador; diálogo que la muestra 60 s y luego la oculta). Resumen: sección de cumplimiento con enlaces filtrados.

- [ ] **Step 1: Escribir las pruebas que fallan**

```ts
it("la lista muestra prioridad y recuentos y un operador no ve «Nueva política»")
it("el editor ofrece solo los ajustes de la plataforma elegida")
it("un ajuste solo auditable no permite elegir «Aplicar» y lo explica")
it("valida los rangos del esquema antes de guardar: bloqueo de pantalla 0 muestra el error")
it("guardar envía ajustes y asignaciones con inclusiones y exclusiones")
it("reordenar envía la nueva lista de ids")
it("la pestaña Cumplimiento muestra cada ajuste con su política y mensaje, y los conflictos")
it("Mostrar clave pide confirmación, muestra la clave y la oculta a los 60 s")
it("el filtro de cumplimiento añade compliance= a la URL y a la petición")
```

- [ ] **Step 2–4:** Run `cd web && npx vitest run && npx tsc --noEmit` — FAIL, implementar, PASS.
- [ ] **Step 5:** Revisión visual con capturas (como en la fase 1) sobre datos sembrados.
- [ ] **Step 6:** Commit `feat(consola): políticas y cumplimiento`.

---

### Task 19: Consola — grupos dinámicos, acciones, lotes y scripts

**Files:**
- Create: `web/src/components/RuleEditor.tsx`, `web/src/components/RunDialog.tsx`, `web/src/pages/Activity.tsx`, `web/src/pages/BatchDetail.tsx`, `web/src/pages/Scripts.tsx`, `web/src/pages/ScriptEdit.tsx`
- Modify: `web/src/pages/Groups.tsx`, `web/src/pages/Devices.tsx` (selección múltiple y barra de acciones), `web/src/pages/DeviceDetail.tsx` (menú de acciones, pestaña Actividad), `web/src/App.tsx`, `web/src/components/Layout.tsx`, `web/src/lib/auditLabels.ts`
- Test: `web/src/components/RuleEditor.test.tsx`, `web/src/components/RunDialog.test.tsx`, `web/src/pages/Activity.test.tsx`, `web/src/pages/Scripts.test.tsx`

**Interfaces:**
- Consumes: rutas de las tareas 6, 9 y 10.
- Produces:
  - Grupos: al crear, elegir «Manual» o «Dinámico»; `RuleEditor` con filas campo/operador/valor (operadores según el campo), «todas/cualquiera», y vista previa en vivo (`POST /groups/preview`, con retraso de 400 ms) con el número de equipos y los 10 primeros.
  - Listado: casillas de selección, «Seleccionar todos los de esta página», barra con acciones según el rol (Reiniciar, Apagar, Actualizar inventario, Reevaluar políticas, Ejecutar script, Retirar).
  - `RunDialog`: resumen («Se reiniciará en 5 minutos con aviso en 14 equipos»), parámetros del tipo (retraso y mensaje; script, versión y sus parámetros), confirmación con el número de equipos; para «Retirar», escribir el número de equipos.
  - Menú «Actividad»: lotes con barra de progreso por estado; detalle del lote con filas por equipo, salida desplegable y «Cancelar» en las pendientes.
  - Menú «Scripts» (todos ven; administrador edita): lista, editor con área de texto monoespaciada, parámetros, historial de versiones y comparación lado a lado de dos versiones (texto plano, sin biblioteca de diff), «Ejecutar».
  - Ficha: la pestaña «Actividad» de la fase 1 (gráfica de conexiones) pasa a llamarse «Conexiones»; la nueva «Actividad» lista las órdenes del equipo con su estado y salida.
  - `auditLabel` cubre todas las acciones nuevas de la fase 2.

- [ ] **Step 1: Escribir las pruebas que fallan**

```ts
it("RuleEditor ofrece operadores de versión para os_version y de texto para hostname")
it("RuleEditor pide la vista previa tras dejar de editar y muestra el recuento")
it("un grupo dinámico no aparece en el selector de grupo principal de la ficha")
it("seleccionar 3 equipos y Reiniciar crea un lote con esos 3 ids, retraso y mensaje")
it("un operador no ve Ejecutar script ni Retirar en la barra de acciones")
it("Retirar exige escribir el número de equipos")
it("el detalle del lote muestra el progreso y la salida de cada equipo")
it("cancelar una orden pendiente la marca cancelada")
it("el editor de scripts crea una versión nueva al guardar y conserva el historial")
it("ejecutar un script con un parámetro obligatorio vacío no se envía")
it("auditLabel traduce todas las acciones de la fase 2")
```

- [ ] **Step 2–4:** Run — FAIL, implementar, PASS.
- [ ] **Step 5:** Revisión visual con capturas.
- [ ] **Step 6:** Commit `feat(consola): grupos dinámicos, acciones, lotes y scripts`.

---

### Task 20: Consola — ajustes y tareas

**Files:**
- Create: `web/src/pages/Settings.tsx`
- Modify: `web/src/App.tsx`, `web/src/components/Layout.tsx`
- Test: `web/src/pages/Settings.test.tsx`

**Interfaces:**
- Consumes: rutas de las tareas 2, 3 y 12.
- Produces: menú «Ajustes» (solo administrador) con tres secciones: **Agentes** (actualización automática, versión distribuida, grupos con versión congelada con «Congelar»/«Descongelar»), **Cumplimiento** (días máximos sin conexión, versiones mínimas de agente y de sistema por plataforma) y **Tareas** (tareas fallidas con tipo, intentos, último error y fecha).

- [ ] **Step 1: Escribir las pruebas que fallan**

```ts
it("solo un administrador ve Ajustes en el menú")
it("guardar envía solo los campos cambiados y muestra «Ajustes guardados.»")
it("una versión mínima con formato inválido muestra el error del servidor")
it("congelar un grupo llama a PUT /agent-pins/{id}")
it("la sección Tareas lista las tareas fallidas con su error")
```

- [ ] **Step 2–4:** Run — FAIL, implementar, PASS.
- [ ] **Step 5:** Commit `feat(consola): ajustes y tareas`.

---

### Task 21: Extremo a extremo y documentación de la fase

**Files:**
- Modify: `scripts/e2e.sh`, `docs/despliegue.md`, `docs/agente.md`, `README.md`
- Create: `docs/politicas.md`

**Interfaces:**
- Consumes: todo lo anterior.
- Produces:
  - `e2e.sh` ampliado (sigue requiriendo Docker): tras la inscripción, crea por la API una política Linux (`linux.ssh` con `root_login: false` en modo aplicar) asignada a todos, espera a que el equipo informe «cumple» y comprueba el fichero `90-mdm.conf` dentro del contenedor; ejecuta un script con un parámetro y comprueba su salida; crea un grupo dinámico `platform eq linux` y comprueba que el equipo es miembro.
  - Prueba manual de autoactualización documentada (compilar el agente con otra versión en `MDM_DOWNLOADS_DIR`, ver el cambio en la auditoría).
  - `docs/politicas.md`: catálogo de ajustes con qué hace cada uno en el equipo (comprobación y aplicación), cómo funcionan prioridades y exclusiones, y cómo escribir un script de detección y corrección.
  - `docs/despliegue.md`: `MDM_SECRET_KEY` (generación, custodia, consecuencias de perderla), actualización desde la fase 1.

- [ ] **Step 1:** Ampliar `scripts/e2e.sh`; `shellcheck scripts/*.sh` — Expected: sin avisos.
- [ ] **Step 2:** Ejecutar a mano en este entorno la misma secuencia que `e2e.sh` (servidor real, agente real como en la tarea 16 del núcleo), con `systemctl` sustituido — Expected: política cumplida, script con salida correcta, pertenencia al grupo dinámico.
- [ ] **Step 3:** Escribir la documentación.
- [ ] **Step 4:** Run la batería completa: `go vet ./...`, `GOOS=windows go vet ./internal/agent/... ./cmd/mdm-agent/`, `go test -race ./...`, `cd web && npx tsc --noEmit && npx vitest run && npm run build`, `sh scripts/install_test.sh`, `make build` — Expected: todo correcto.
- [ ] **Step 5:** Commit `docs: fase 2 (políticas, scripts y actualización del agente)`.
