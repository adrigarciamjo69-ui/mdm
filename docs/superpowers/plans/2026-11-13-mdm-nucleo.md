# MDM núcleo (fase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Equipos Windows y Linux inscritos con un comando, inventariados y visibles en una consola web autoalojada, con administración segura y auditada.

**Architecture:** Un binario de servidor en Go con dos API (`/agent/v1` para agentes, `/api/v1` para la consola) sobre PostgreSQL, que además sirve la consola React incrustada. Un binario de agente en Go por plataforma que solo abre conexiones salientes HTTPS: se inscribe con un token, hace check-in cada 60 s y envía inventario cuando cambia su huella.

**Tech Stack:** Go 1.23, `chi`, `pgx` v5, `goose`, `x/crypto/argon2`, `pquerna/otp`, `x/sys/windows/svc`, `yusufpapurcu/wmi`, PostgreSQL 16, React 18 + TypeScript + Vite, Vitest, Docker Compose, Caddy.

**Spec:** `docs/superpowers/specs/2026-11-12-mdm-nucleo-design.md`

## Global Constraints

- Módulo de Go: `mdm`. Versión mínima de Go: 1.23. PostgreSQL 16.
- Todas las fechas en `timestamptz` UTC. Identificadores UUID v4 salvo `audit_log.id` (bigserial).
- JSON en `snake_case` en ambas API. Errores siempre como `{ "error": { "code": "…", "message": "…" } }`, con `message` en español.
- Códigos de error: `invalid_request` (400), `unauthorized` (401), `forbidden` (403), `not_found` (404), `conflict` (409), `too_large` (413), `rate_limited` (429), `unavailable` (503).
- Todo texto visible en la consola, en la salida del agente y en la documentación va en español.
- Solo `internal/server/store` contiene SQL. Solo `internal/protocol` se comparte entre servidor y agente.
- El agente no ejecuta nada que venga del servidor: ignora `commands`.
- Las pruebas que usan PostgreSQL leen `MDM_TEST_DATABASE_URL`; se omiten con `go test -short` y fallan con un mensaje claro si la variable falta sin `-short`.
- Límites: check-in 16 KB; inventario 1 MB comprimido y 8 MB descomprimido; resto de la API de consola 64 KB; inscripción 10 intentos fallidos por minuto por IP (las inscripciones correctas no cuentan); inicio de sesión 5 fallos cada 15 minutos por IP y correo.
- Valores por defecto: check-in 60 s, inventario 21 600 s, en línea si conectó hace menos de 3 intervalos, inactivo a partir de 30 días, sesión de 12 horas, actividad conservada 90 días.
- Cada tarea termina con `go vet ./...` limpio y un commit con mensaje en español con prefijo convencional (`feat:`, `test:`, `docs:`, `chore:`).

## Review Focus

1. **Inscripción masiva simultánea** (despliegue por GPO a decenas de equipos a la vez con un token de usos limitados): los usos nunca superan `max_uses`. Prueba en la tarea 7.
2. **Toda la empresa sale por una misma IP pública** (NAT de oficina) o el servidor está detrás de Caddy: cientos de inscripciones válidas desde una sola IP no deben bloquearse; solo cuentan los intentos fallidos, y la IP se lee de `X-Forwarded-For` cuando hay proxy. Pruebas en las tareas 4 y 7.
3. **Cadenas corruptas en el inventario** (caracteres nulos o valores enormes en el registro de Windows): el envío se guarda saneado en vez de rechazarse. Prueba en la tarea 8.
4. **Equipos clonados con el mismo identificador de hardware**: la reinscripción con nombre distinto queda señalada en auditoría para que el administrador lo vea. Prueba en la tarea 7.
5. **CSV abierto en una hoja de cálculo** con un nombre de equipo o usuario que empieza por `=`, `+`, `-` o `@`: la celda no se interpreta como fórmula. Prueba en la tarea 9.

---

## File Structure

```
go.mod  Makefile  .gitignore
cmd/mdm-server/main.go            subcomandos serve y admin
cmd/mdm-agent/main.go             subcomandos enroll, run, status, uninstall
internal/protocol/                tipos compartidos y huella de inventario
internal/server/store/            SQL: un fichero por entidad
internal/server/store/storetest/  base de datos aislada por prueba
internal/server/auth/             primitivas sin estado
internal/server/httpx/            JSON, errores, IP, límite de cuerpo
internal/server/ratelimit/        ventana deslizante en memoria
internal/server/audit/            registro de auditoría
internal/server/agentapi/         /agent/v1
internal/server/adminapi/         /api/v1 (un fichero por recurso)
internal/server/app/              ensamblado de rutas, estáticos, descargas, limpieza
internal/agent/config/            fichero de configuración y de estado
internal/agent/client/            cliente HTTP y espera exponencial
internal/agent/inventory/         interfaz común + linux + windows
internal/agent/enroll/            inscripción
internal/agent/loop/              bucle de check-in
internal/agent/service/           systemd y gestor de servicios de Windows
migrations/                       SQL de goose + embed.go
web/                              consola; web/embed.go incrusta web/dist
deploy/                           Dockerfile, docker-compose.yml, Caddyfile, .env.example
scripts/                          install.sh, install.ps1, loadtest/
docs/                             despliegue.md, agente.md, validacion-windows.md
```

---

### Task 1: Protocolo compartido y huella de inventario

**Files:**
- Create: `go.mod`, `.gitignore`, `Makefile`, `internal/protocol/protocol.go`, `internal/protocol/hash.go`
- Test: `internal/protocol/hash_test.go`, `internal/protocol/protocol_test.go`

**Interfaces:**
- Produces (paquete `protocol`, etiquetas JSON según los apartados 7.1 a 7.3 de la especificación):
  - `EnrollRequest{Token, HardwareID, Hostname, Platform, OSName, OSVersion, Arch, Serial, Manufacturer, Model, AgentVersion string}`
  - `EnrollResponse{DeviceID, Credential string; CheckinIntervalS, InventoryIntervalS int}`
  - `CheckinRequest{AgentVersion string; UptimeS int64; InventoryHash string}`
  - `AgentConfig{CheckinIntervalS, InventoryIntervalS int}`
  - `Command{ID, Type string; Payload json.RawMessage}`
  - `CheckinResponse{Config AgentConfig; SendInventory bool; Commands []Command}` (`Commands` se serializa como `[]`, nunca `null`)
  - `Inventory{CollectedAt time.Time; Hash string; Unavailable []string; System System; Hardware Hardware; Disks []Disk; Network []NetIf; Security Security; Software []Software}` con los subtipos y campos del apartado 7.3; `Software.InstalledAt *time.Time`
  - Constantes: `DefaultCheckinIntervalS = 60`, `DefaultInventoryIntervalS = 21600`, `PlatformWindows = "windows"`, `PlatformLinux = "linux"`, secciones `SectionSystem … SectionSoftware` (`"system"`, `"hardware"`, `"disks"`, `"network"`, `"security"`, `"software"`), estados `StateOn = "on"`, `StateOff = "off"`, `StatePartial = "partial"`, `StateUnknown = "unknown"`
  - `func InventoryHash(inv Inventory) string` devuelve `"sha256:" + hex`
- `Makefile`: objetivos `test` (`go test ./...`), `test-short`, `build` (servidor y agentes `linux/amd64`, `linux/arm64`, `windows/amd64`, `windows/arm64` en `dist/`, con `CGO_ENABLED=0` y `-ldflags "-X main.version=$(VERSION)"`), `web`.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestInventoryHash_IgnoresVolatileFields(t *testing.T) {
    a := sampleInventory()
    b := sampleInventory()
    b.CollectedAt = a.CollectedAt.Add(time.Hour)
    b.Hash = "sha256:otro"
    b.System.BootTime = "2026-01-01T00:00:00Z"
    b.Disks[0].FreeBytes = 1
    if InventoryHash(a) != InventoryHash(b) { t.Fatal("la huella cambió con campos volátiles") }
}
func TestInventoryHash_ChangesWithSoftware(t *testing.T)   // añadir un programa cambia la huella
func TestInventoryHash_OrderIndependent(t *testing.T)      // mismo software, discos y red en otro orden: misma huella
func TestInventoryHash_Format(t *testing.T)                // prefijo "sha256:" y 64 caracteres hexadecimales
func TestCheckinResponse_EmptyCommandsIsArray(t *testing.T) // json.Marshal(CheckinResponse{}) contiene `"commands":[]`
func TestEnrollRequest_JSONNames(t *testing.T)             // claves token, hardware_id, os_name, agent_version
```

- [ ] **Step 2:** Run `go test ./internal/protocol/` — Expected: FAIL, no compila.
- [ ] **Step 3: Implementar los tipos y `InventoryHash`.** La huella copia el inventario, pone a cero `CollectedAt`, `Hash`, `System.BootTime` y cada `FreeBytes`, ordena `Software` por (nombre, versión), `Disks` por `Device`, `Network` por `Name` y `Unavailable` alfabéticamente, serializa con `encoding/json` y aplica SHA-256. `CheckinResponse` implementa `MarshalJSON` para emitir `[]`.
- [ ] **Step 4:** Run `go test ./internal/protocol/` — Expected: PASS.
- [ ] **Step 5:** Commit `feat: protocolo compartido y huella de inventario`.

---

### Task 2: Migraciones y base del almacén

**Files:**
- Create: `migrations/0001_init.sql`, `migrations/embed.go`, `internal/server/store/store.go`, `internal/server/store/models.go`, `internal/server/store/storetest/storetest.go`
- Test: `internal/server/store/store_test.go`

**Interfaces:**
- Produces:
  - `migrations.FS embed.FS`
  - `store.Open(ctx context.Context, url string) (*Store, error)`; `(*Store).Close()`; `(*Store).Ping(ctx) error`; `store.Migrate(ctx, url string) error`
  - `var ErrNotFound, ErrConflict error`
  - Modelos en `models.go`: `Admin`, `Session`, `Group`, `EnrollmentToken`, `Device`, `AuditEntry`, con un campo por columna del apartado 5 (`uuid.UUID` de `github.com/google/uuid`, `*time.Time` para columnas anulables, `[]string` para `tags`)
  - `storetest.New(t *testing.T) *store.Store`: crea un esquema con nombre aleatorio, migra, y lo elimina en `t.Cleanup`

- [ ] **Step 1: Escribir la prueba que falla**

```go
func TestMigrate_CreatesAllTables(t *testing.T) {
    s := storetest.New(t)
    for _, tbl := range []string{"admins","sessions","groups","enrollment_tokens","devices",
        "device_inventory","device_software","device_activity","commands","audit_log"} {
        if !s.TableExists(context.Background(), tbl) { t.Errorf("falta la tabla %s", tbl) }
    }
}
func TestMigrate_Idempotent(t *testing.T) // llamar a Migrate dos veces no da error
```

- [ ] **Step 2:** Run `go test ./internal/server/store/ -run TestMigrate` — Expected: FAIL.
- [ ] **Step 3: Escribir `0001_init.sql`** con las tablas del apartado 5. Restricciones que la especificación fija: `admins.email` único sobre `lower(email)`; `admins.role` con `CHECK` en (`admin`, `operator`, `readonly`); `devices.hardware_id` único; `devices.platform` con `CHECK`; claves ajenas de `device_inventory`, `device_software`, `device_activity` y `commands` hacia `devices` con `ON DELETE CASCADE`; `devices.group_id` y `enrollment_tokens.group_id` con `ON DELETE SET NULL`; `sessions.admin_id` con `ON DELETE CASCADE`; `sessions.verified boolean NOT NULL DEFAULT false`. Índices: `devices(last_seen_at)`, `devices(group_id)`, `devices(lower(hostname))`, `device_software(device_id)`, `device_software(lower(name))`, `audit_log(at DESC)`, `sessions(expires_at)`.
- [ ] **Step 4: Implementar `Open`, `Migrate` y `storetest.New`.** `Migrate` usa `goose` con `migrations.FS`. `storetest.New` fija `search_path` al esquema nuevo en la cadena de conexión. `TableExists` consulta `information_schema.tables` en el esquema actual.
- [ ] **Step 5:** Run `go test ./internal/server/store/` — Expected: PASS.
- [ ] **Step 6:** Commit `feat: esquema inicial y almacén`.

---

### Task 3: Primitivas de autenticación

**Files:**
- Create: `internal/server/auth/passwords.go`, `secrets.go`, `roles.go`, `totp.go`, `device.go`
- Test: un `_test.go` por fichero

**Interfaces:**
- Produces (paquete `auth`):
  - `HashPassword(pw string) (string, error)` — Argon2id, 64 MB, 3 iteraciones, paralelismo 2, sal de 16 bytes, formato PHC `$argon2id$v=19$m=65536,t=3,p=2$<sal>$<hash>`
  - `VerifyPassword(encoded, pw string) bool`; `ValidatePassword(pw string) error` (mínimo 12 caracteres)
  - `NewSecret() (plain, hash string, err error)` — 32 bytes aleatorios en base64url sin relleno; `HashSecret(plain string) string` — SHA-256 en hexadecimal; `SecretMatches(hash, plain string) bool` — comparación en tiempo constante
  - `type Role string`; `RoleReadonly = "readonly"`, `RoleOperator = "operator"`, `RoleAdmin = "admin"`; `(Role).AtLeast(min Role) bool`; `ParseRole(s string) (Role, error)`
  - `NewTOTP(email string) (secret, uri string, err error)` con emisor `MDM`; `VerifyTOTP(secret, code string) bool`
  - `FormatDeviceCredential(deviceID, secret string) string` → `"<deviceID>.<secret>"`; `ParseDeviceCredential(header string) (deviceID uuid.UUID, secret string, err error)` a partir del valor completo de `Authorization`

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestPassword_RoundTrip(t *testing.T)        // Verify(Hash("correcta-y-larga-1"), misma) == true; con otra == false
func TestPassword_HashFormat(t *testing.T)       // empieza por "$argon2id$v=19$m=65536,t=3,p=2$"
func TestPassword_SaltDiffers(t *testing.T)      // dos hashes de la misma contraseña son distintos
func TestValidatePassword(t *testing.T)          // 11 caracteres → error; 12 → nil
func TestNewSecret(t *testing.T)                 // len(base64url-decodificado) == 32; HashSecret(plain) == hash; dos llamadas difieren
func TestSecretMatches(t *testing.T)             // true con el propio; false con otro y con ""
func TestRoleAtLeast(t *testing.T) {
    cases := []struct{ r, min Role; want bool }{
        {RoleAdmin, RoleOperator, true}, {RoleOperator, RoleAdmin, false},
        {RoleReadonly, RoleReadonly, true}, {RoleReadonly, RoleOperator, false},
    }
}
func TestParseRole(t *testing.T)                 // "admin" ok; "root" → error
func TestTOTP(t *testing.T)                      // código generado con totp.GenerateCode(secret, now) valida; "000000" no; uri contiene "issuer=MDM"
func TestDeviceCredential(t *testing.T)          // Parse("Bearer "+Format(id, s)) devuelve id y s; sin "Bearer ", sin punto o con UUID inválido → error
```

- [ ] **Step 2:** Run `go test ./internal/server/auth/` — Expected: FAIL.
- [ ] **Step 3:** Implementar.
- [ ] **Step 4:** Run `go test ./internal/server/auth/` — Expected: PASS.
- [ ] **Step 5:** Commit `feat: primitivas de autenticación`.

---

### Task 4: Utilidades HTTP y límite de intentos

**Files:**
- Create: `internal/server/httpx/httpx.go`, `internal/server/ratelimit/ratelimit.go`
- Test: `internal/server/httpx/httpx_test.go`, `internal/server/ratelimit/ratelimit_test.go`

**Interfaces:**
- Produces:
  - `httpx.WriteJSON(w, status int, v any)`; `httpx.WriteError(w, status int, code, message string)`
  - `httpx.DecodeJSON(w, r, maxBytes int64, dst any) bool` — responde 413 `too_large` o 400 `invalid_request` y devuelve `false`; rechaza campos desconocidos
  - `httpx.ClientIP(r *http.Request, trustProxy bool) string`
  - `ratelimit.New(limit int, window time.Duration, now func() time.Time) *Limiter`
  - `(*Limiter).Record(key string)`; `(*Limiter).Blocked(key string) (bool, time.Duration)` (segundo valor: espera hasta que caduque el registro más antiguo); `(*Limiter).Reset(key string)`

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestClientIP_TrustedProxyUsesLastForwarded(t *testing.T) {
    r := httptest.NewRequest("GET", "/", nil)
    r.RemoteAddr = "172.18.0.2:5555"
    r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
    if got := ClientIP(r, true); got != "203.0.113.9" { t.Fatalf("got %s", got) }
}
func TestClientIP_UntrustedIgnoresHeader(t *testing.T)  // con trustProxy=false devuelve "172.18.0.2"
func TestClientIP_TrustedWithoutHeader(t *testing.T)    // sin cabecera devuelve "172.18.0.2"
func TestDecodeJSON_TooLarge(t *testing.T)              // cuerpo de 17 KB con máximo 16 KB → 413 y code "too_large"
func TestDecodeJSON_Malformed(t *testing.T)             // "{" → 400 y code "invalid_request"
func TestWriteError_Shape(t *testing.T)                 // cuerpo == {"error":{"code":"not_found","message":"No existe."}}

func TestLimiter_BlocksAfterLimit(t *testing.T)         // limit 5: tras 5 Record, Blocked == true; tras 4, false
func TestLimiter_WindowExpires(t *testing.T)            // avanzar el reloj 15 min + 1 s desbloquea
func TestLimiter_RetryAfter(t *testing.T)               // 5 Record en t0, consulta en t0+5min → espera de 10 min
func TestLimiter_KeysIndependent(t *testing.T)
func TestLimiter_Reset(t *testing.T)
```

- [ ] **Step 2:** Run `go test ./internal/server/httpx/ ./internal/server/ratelimit/` — Expected: FAIL.
- [ ] **Step 3:** Implementar. El limitador guarda marcas de tiempo por clave bajo un `sync.Mutex` y descarta las caducadas en cada consulta.
- [ ] **Step 4:** Run las mismas pruebas — Expected: PASS.
- [ ] **Step 5:** Commit `feat: utilidades HTTP y límite de intentos`.

---

### Task 5: Administradores, sesiones y auditoría

**Files:**
- Create: `internal/server/store/admins.go`, `store/sessions.go`, `store/audit.go`, `internal/server/audit/audit.go`, `internal/server/adminapi/adminapi.go`, `adminapi/auth.go`, `adminapi/middleware.go`
- Test: `internal/server/store/admins_test.go`, `internal/server/adminapi/auth_test.go`, `adminapi/testhelpers_test.go`

**Interfaces:**
- Consumes: tareas 2, 3 y 4.
- Produces:
  - Almacén: `CreateAdmin(ctx, email, passwordHash string, role auth.Role) (Admin, error)` (`ErrConflict` si el correo existe); `AdminByEmail`, `AdminByID`; `ListAdmins(ctx) ([]Admin, error)`; `UpdateAdmin(ctx, id uuid.UUID, p AdminPatch) error` con `AdminPatch{Role *auth.Role; Disabled *bool}` (devuelve `ErrConflict` si dejaría cero administradores activos con rol `admin`); `SetAdminTOTP(ctx, id, secret *string) error`; `TouchAdminLogin(ctx, id, at)`
  - `CreateSession(ctx, Session) error`; `SessionByTokenHash(ctx, hash string, now time.Time) (Session, Admin, error)` (`ErrNotFound` si caducó o el administrador está desactivado); `VerifySession(ctx, tokenHash string) error`; `DeleteSession(ctx, tokenHash string) error`; `DeleteSessionsForAdmin(ctx, adminID) error`; `PurgeExpiredSessions(ctx, now) (int64, error)`
  - `InsertAudit(ctx, AuditEntry) error`; `ListAudit(ctx, AuditFilter{ActorEmail, Action string; From, To *time.Time; Page, PerPage int}) ([]AuditEntry, int, error)`
  - `audit.New(s *store.Store) *Logger`; `(*Logger).Log(ctx, audit.Entry{Actor *store.Admin; Action, TargetType, TargetID string; Details map[string]any; IP string})`. Si la escritura falla, registra el error con `slog` y no interrumpe la petición.
  - Constantes de acción en `audit`: `login.ok`, `login.failed`, `logout`, `totp.enrolled`, `admin.created`, `admin.updated`, `token.created`, `token.revoked`, `device.enrolled`, `device.reenrolled`, `device.updated`, `device.revoked`, `device.deleted`, `group.created`, `group.updated`, `group.deleted`, `devices.exported`
  - `adminapi.Deps{Store *store.Store; Audit *audit.Logger; LoginLimiter *ratelimit.Limiter; Now func() time.Time; TrustProxy bool; CheckinIntervalS int; PublicURL string}`
  - `adminapi.New(d Deps) http.Handler` (monta bajo `/api/v1`)
  - Middleware: `requireSession(min auth.Role)`, que además exige `X-CSRF-Token` igual al de la sesión en métodos distintos de GET y HEAD; `adminFrom(ctx) store.Admin`
  - Cookie `mdm_session`: `HttpOnly`, `Secure`, `SameSite=Strict`, `Path=/`, 12 horas
  - Rutas: `POST /auth/login` → 200 `{"next":"totp"}` o `{"next":"totp_setup"}` y cookie de sesión no verificada; `POST /auth/totp/setup` → `{"secret","uri"}` (solo si el administrador no tiene TOTP); `POST /auth/totp/verify` con `{"code"}` → 200 con el cuerpo de `/auth/me`; `POST /auth/logout` → 204; `GET /auth/me` → `{"id","email","role","csrf_token"}`
  - Ayuda de pruebas (en `testhelpers_test.go`, reutilizada por las tareas 6, 9 y 10): `newTestAPI(t) *testAPI` con `.loginAs(role auth.Role) *testClient`, y `(*testClient).do(method, path string, body any) *http.Response` que envía cookie y CSRF

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
// store
func TestCreateAdmin_DuplicateEmailCaseInsensitive(t *testing.T) // "A@x.es" y "a@x.es" → ErrConflict
func TestUpdateAdmin_CannotRemoveLastAdmin(t *testing.T)         // desactivar o degradar al único admin → ErrConflict
func TestSession_ExpiredNotFound(t *testing.T)                   // now = expires_at + 1 s → ErrNotFound
func TestSession_DisabledAdminNotFound(t *testing.T)

// adminapi
func TestLogin_WrongPasswordAndUnknownEmailLookTheSame(t *testing.T) // ambos 401, mismo cuerpo
func TestLogin_FirstTimeRequiresTOTPSetup(t *testing.T)   // next == "totp_setup"
func TestLogin_PartialSessionCannotAccessAPI(t *testing.T) // tras login sin TOTP, GET /auth/me → 401
func TestLogin_FullFlow(t *testing.T)                     // login → setup → verify → GET /auth/me 200 con role y csrf_token
func TestLogin_BadTOTPCode(t *testing.T)                  // verify con "000000" → 401; cuenta como fallo
func TestLogin_RateLimited(t *testing.T)                  // 5 fallos → el sexto intento, aun correcto, 429 con Retry-After
func TestCSRF_MissingHeaderRejected(t *testing.T)         // POST /auth/logout sin X-CSRF-Token → 403
func TestLogout_InvalidatesSession(t *testing.T)          // tras logout, GET /auth/me → 401
func TestCookieAttributes(t *testing.T)                   // HttpOnly, Secure, SameSite=Strict
func TestAudit_LoginRecorded(t *testing.T)                // existe login.ok con el correo y login.failed tras un fallo
```

- [ ] **Step 2:** Run `go test ./internal/server/store/ ./internal/server/adminapi/` — Expected: FAIL.
- [ ] **Step 3:** Implementar almacén y auditoría.
- [ ] **Step 4:** Implementar el flujo de sesión. La clave del limitador es `ip + "|" + lower(email)`; se consulta antes de verificar y se reinicia tras un acceso completo. Para un correo inexistente se ejecuta igualmente una verificación Argon2 contra un hash fijo, de modo que el tiempo de respuesta no revele si existe.
- [ ] **Step 5:** Run las mismas pruebas — Expected: PASS.
- [ ] **Step 6:** Commit `feat: administradores, sesiones con TOTP y auditoría`.

---

### Task 6: Grupos y tokens de inscripción

**Files:**
- Create: `internal/server/store/groups.go`, `store/tokens.go`, `internal/server/adminapi/groups.go`, `adminapi/tokens.go`
- Test: `internal/server/adminapi/groups_test.go`, `adminapi/tokens_test.go`

**Interfaces:**
- Consumes: `requireSession`, `newTestAPI` (tarea 5); `auth.NewSecret` (tarea 3).
- Produces:
  - `CreateGroup(ctx, name, description string) (Group, error)`; `ListGroups(ctx) ([]GroupWithCount, error)` (`GroupWithCount{Group; DeviceCount int}`); `UpdateGroup(ctx, id, name, description string) error`; `DeleteGroup(ctx, id) error`
  - `CreateToken(ctx, TokenParams{Name, TokenHash string; GroupID *uuid.UUID; ExpiresAt *time.Time; MaxUses *int; CreatedBy uuid.UUID}) (EnrollmentToken, error)`; `ListTokens(ctx) ([]EnrollmentToken, error)`; `RevokeToken(ctx, id, at) error`
  - `store.TokenState(t EnrollmentToken, now time.Time) string` → `"active"`, `"expired"`, `"exhausted"` o `"revoked"`
  - Rutas: `GET /groups` (lectura); `POST /groups`, `PATCH /groups/{id}`, `DELETE /groups/{id}` (operador); `GET /enrollment-tokens`, `POST /enrollment-tokens`, `POST /enrollment-tokens/{id}/revoke` (operador)
  - `POST /enrollment-tokens` acepta `{"name","group_id","expires_at","max_uses"}` y responde 201 con el token en claro en `"token"` y los comandos listos en `"install":{"linux","windows"}`. Los listados nunca incluyen el token ni su hash.
  - Comandos (con `PublicURL` y el token sustituidos):
    - Linux: `curl -fsSL <url>/downloads/install.sh | sudo sh -s -- --server <url> --token <token>`
    - Windows: `powershell -ExecutionPolicy Bypass -Command "& ([scriptblock]::Create((irm <url>/downloads/install.ps1))) -Server <url> -Token <token>"`

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestGroups_CRUD(t *testing.T)                      // crear 201, listar con device_count 0, editar 200, borrar 204
func TestGroups_DuplicateName(t *testing.T)             // 409 conflict
func TestGroups_ReadonlyCannotCreate(t *testing.T)      // 403
func TestGroups_EmptyNameRejected(t *testing.T)         // 400
func TestTokens_CreateReturnsPlainOnce(t *testing.T)    // 201 con "token" de 43 caracteres; GET no contiene esa cadena
func TestTokens_InstallCommands(t *testing.T)           // install.linux contiene PublicURL y el token; install.windows también
func TestTokens_ReadonlyCannotList(t *testing.T)        // 403
func TestTokens_Revoke(t *testing.T)                    // tras revocar, state == "revoked"
func TestTokenState(t *testing.T)                       // caducado → "expired"; uses == max_uses → "exhausted"; revocado prevalece
func TestTokens_UnknownGroupRejected(t *testing.T)      // group_id inexistente → 400
func TestAudit_TokenAndGroupActions(t *testing.T)       // token.created, token.revoked, group.created, group.updated, group.deleted
```

- [ ] **Step 2:** Run `go test ./internal/server/adminapi/ -run 'Groups|Tokens|TokenState|Audit_Token'` — Expected: FAIL.
- [ ] **Step 3:** Implementar.
- [ ] **Step 4:** Run las mismas pruebas — Expected: PASS.
- [ ] **Step 5:** Commit `feat: grupos y tokens de inscripción`.

---

### Task 7: API de agentes — inscripción

**Files:**
- Create: `internal/server/store/devices.go`, `internal/server/agentapi/agentapi.go`, `agentapi/enroll.go`
- Test: `internal/server/agentapi/enroll_test.go`, `agentapi/testhelpers_test.go`

**Interfaces:**
- Consumes: `protocol` (tarea 1); `auth.NewSecret`, `auth.HashSecret` (tarea 3); `httpx`, `ratelimit` (tarea 4); `audit` (tarea 5); `CreateToken` (tarea 6).
- Produces:
  - `store.EnrollDevice(ctx, EnrollParams{TokenHash, CredentialHash string; Req protocol.EnrollRequest; IP string; At time.Time}) (EnrollResult, error)` con `EnrollResult{DeviceID uuid.UUID; Reenrolled bool; PreviousHostname string}`; `var ErrTokenInvalid error`
  - `store.DeviceByID(ctx, id uuid.UUID) (Device, error)`
  - `agentapi.Deps{Store *store.Store; Audit *audit.Logger; EnrollLimiter *ratelimit.Limiter; Now func() time.Time; TrustProxy bool}`
  - `agentapi.New(d Deps) http.Handler` (monta bajo `/agent/v1`)
  - `POST /enroll` según el apartado 7.1
  - Ayuda de pruebas: `newAgentAPI(t) *agentTest` con `.token(maxUses *int, expires *time.Time) string` y `.enroll(req protocol.EnrollRequest) (*http.Response, protocol.EnrollResponse)`

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestEnroll_Success(t *testing.T) {
    // 201; device_id es un UUID; credential de 43 caracteres;
    // checkin_interval_s == 60; inventory_interval_s == 21600;
    // el dispositivo existe con hostname, platform y el group_id del token;
    // credential_hash == auth.HashSecret(credential); uses del token == 1
}
func TestEnroll_InvalidTokensAllLookTheSame(t *testing.T) // inexistente, caducado, revocado y agotado → 403 con el mismo cuerpo
func TestEnroll_MissingFields(t *testing.T)               // sin hardware_id, sin hostname o con platform "macos" → 400
func TestEnroll_RateLimitCountsOnlyFailures(t *testing.T) {
    // 10 intentos con token inválido desde una IP → el undécimo, aun con token válido, 429 con Retry-After;
    // desde otra IP, 30 inscripciones válidas seguidas → todas 201
}
func TestEnroll_ReenrollKeepsRecordAndRotatesCredential(t *testing.T) {
    // mismo hardware_id: mismo device_id, credential distinta, credential_hash actualizado,
    // revoked_at vuelve a NULL, y group_id, assigned_user y tags se conservan
}
func TestEnroll_ReenrollWithDifferentHostnameFlaggedInAudit(t *testing.T) {
    // auditoría device.reenrolled con details.hostname_changed == true,
    // details.previous_hostname == "PC-A", details.hostname == "PC-B"
}
func TestEnroll_AuditOnFirstEnroll(t *testing.T)          // device.enrolled con target_id == device_id y el nombre del token
func TestEnroll_ConcurrentRespectsMaxUses(t *testing.T) {
    // token con max_uses = 5; 20 goroutines inscriben hardware_id distintos a la vez:
    // exactamente 5 respuestas 201 y 15 respuestas 403; uses == 5; 5 dispositivos
}
func TestEnroll_FailedAttemptDoesNotConsumeUse(t *testing.T) // petición 400 con token válido deja uses en 0
```

- [ ] **Step 2:** Run `go test ./internal/server/agentapi/ -run TestEnroll` — Expected: FAIL.
- [ ] **Step 3: Implementar `EnrollDevice`** en una sola transacción. El consumo del token es un único `UPDATE … SET uses = uses + 1 WHERE token_hash = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > $2) AND (max_uses IS NULL OR uses < max_uses) RETURNING id, group_id`; cero filas significa `ErrTokenInvalid`. El dispositivo se inserta con `ON CONFLICT (hardware_id) DO UPDATE` que actualiza identidad, credencial y `revoked_at = NULL` pero no `group_id`, `assigned_user` ni `tags`.
- [ ] **Step 4:** Implementar el manejador. Orden: consultar el límite por IP, decodificar, validar, generar secreto, `EnrollDevice`, auditoría. Solo un `ErrTokenInvalid` registra un intento en el limitador.
- [ ] **Step 5:** Run `go test ./internal/server/agentapi/ -run TestEnroll -race` — Expected: PASS.
- [ ] **Step 6:** Commit `feat: inscripción de dispositivos`.

---

### Task 8: API de agentes — check-in e inventario

**Files:**
- Create: `internal/server/store/inventory.go`, `internal/server/store/sanitize.go`, `internal/server/agentapi/middleware.go`, `agentapi/checkin.go`, `agentapi/inventory.go`
- Modify: `internal/server/store/devices.go`
- Test: `internal/server/agentapi/checkin_test.go`, `agentapi/inventory_test.go`, `internal/server/store/sanitize_test.go`

**Interfaces:**
- Consumes: tarea 7.
- Produces:
  - `store.RecordCheckin(ctx, id uuid.UUID, p CheckinParams{AgentVersion, IP string; At time.Time}) error` — actualiza `last_seen_at`, `last_ip`, `agent_version` y suma 1 en `device_activity` para la hora de `At`
  - `store.SaveInventory(ctx, id uuid.UUID, inv protocol.Inventory, at time.Time) error`
  - `store.SanitizeInventory(inv *protocol.Inventory)` — elimina `\x00` y recorta cada cadena a 512 caracteres (runas, no bytes)
  - `store.DeviceInventory(ctx, id) (data json.RawMessage, collectedAt time.Time, err error)`
  - Middleware `requireDevice`: 401 `unauthorized` si la credencial falta, es inválida, no coincide o el dispositivo tiene `revoked_at`; `deviceFrom(ctx) store.Device`
  - `POST /checkin` (apartado 7.2) y `POST /inventory` (apartado 7.3)
  - Ayuda de pruebas: `(*agentTest).enrolled() *agentClient` con `.checkin(protocol.CheckinRequest)` y `.inventory(protocol.Inventory)`; `sampleInventory()`

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestCheckin_UpdatesLastSeenAndActivity(t *testing.T) // last_seen_at == now; last_ip; agent_version; activity de la hora == 1, tras otro == 2
func TestCheckin_RequestsInventoryWhenHashDiffers(t *testing.T) // dispositivo nuevo → send_inventory true
func TestCheckin_NoInventoryWhenHashMatchesAndFresh(t *testing.T)
func TestCheckin_RequestsInventoryWhenStale(t *testing.T) // misma huella, inventory_at hace 21601 s → true
func TestCheckin_ResponseShape(t *testing.T)              // config 60 / 21600; "commands":[]
func TestCheckin_Unauthorized(t *testing.T)               // sin cabecera, secreto erróneo, UUID inexistente → 401
func TestCheckin_RevokedDevice(t *testing.T)              // revoked_at fijado → 401
func TestCheckin_OldCredentialAfterReenroll(t *testing.T) // 401
func TestCheckin_BodyTooLarge(t *testing.T)               // 17 KB → 413

func TestInventory_StoresSnapshotAndSoftware(t *testing.T) {
    // 204; device_inventory.data contiene system, hardware, disks, network, security y no software;
    // device_software tiene las filas enviadas; devices.inventory_hash == inv.Hash;
    // devices.hostname, os_name, os_version, serial, manufacturer y model reflejan el inventario
}
func TestInventory_ReplacesPrevious(t *testing.T)          // segundo envío con 1 programa deja 1 fila
func TestInventory_UnavailableSectionKeepsPrevious(t *testing.T) {
    // primer envío con 3 programas; segundo con unavailable ["software"] y software vacío:
    // siguen las 3 filas. Igual con "security": el valor anterior de data.security se conserva
}
func TestInventory_RequiresGzip(t *testing.T)              // sin Content-Encoding: gzip → 400
func TestInventory_CompressedTooLarge(t *testing.T)        // más de 1 MB comprimido → 413
func TestInventory_DecompressedTooLarge(t *testing.T)      // bomba: 9 MB de ceros comprimidos → 413
func TestInventory_NulBytesAndHugeStringsSanitized(t *testing.T) {
    // software name "Evil\x00App", publisher de 5000 caracteres, hostname con \x00:
    // 204; nombre guardado "EvilApp"; publisher de 512 runas
}
func TestInventory_ConcurrentSendsBothSucceed(t *testing.T) // dos envíos simultáneos del mismo dispositivo → 204 y 204, sin filas duplicadas
func TestSanitize_MultibyteTruncation(t *testing.T)        // 600 «ñ» → 512 «ñ», UTF-8 válido
```

- [ ] **Step 2:** Run `go test ./internal/server/agentapi/ ./internal/server/store/ -run 'Checkin|Inventory|Sanitize'` — Expected: FAIL.
- [ ] **Step 3: Implementar `SaveInventory`.** Transacción que empieza con `SELECT … FROM devices WHERE id = $1 FOR UPDATE` (serializa envíos simultáneos), fusiona con el `data` anterior las secciones listadas en `unavailable`, hace `UPSERT` en `device_inventory`, y si `software` no está en `unavailable` borra e inserta con `pgx.CopyFrom`.
- [ ] **Step 4:** Implementar middleware y manejadores. El inventario se lee con `http.MaxBytesReader` (1 MB) y se descomprime a través de `io.LimitReader` de 8 MB + 1 byte para detectar el exceso.
- [ ] **Step 5:** Run las mismas pruebas con `-race` — Expected: PASS.
- [ ] **Step 6:** Commit `feat: check-in e inventario`.

---

### Task 9: API de consola — dispositivos y resumen

**Files:**
- Create: `internal/server/store/status.go`, `store/devices_query.go`, `store/summary.go`, `internal/server/adminapi/devices.go`, `adminapi/summary.go`, `adminapi/csv.go`
- Test: `internal/server/store/status_test.go`, `internal/server/adminapi/devices_test.go`, `adminapi/summary_test.go`

**Interfaces:**
- Consumes: tareas 5 a 8.
- Produces:
  - `type Status string`; `StatusOnline = "online"`, `StatusOffline = "offline"`, `StatusInactive = "inactive"`, `StatusRevoked = "revoked"`
  - `store.StatusOf(d Device, now time.Time, checkinIntervalS int) Status`
  - `store.ListDevices(ctx, f DeviceFilter) ([]DeviceRow, int, error)` con `DeviceFilter{Q, Platform, OSVersion, Tag string; Status Status; GroupID *uuid.UUID; Sort, Order string; Page, PerPage int; Now time.Time; CheckinIntervalS int}` y `DeviceRow{Device; GroupName string}`
  - `store.ListSoftware(ctx, id uuid.UUID, q string, page, perPage int) ([]protocol.Software, int, error)`; `store.DeviceActivity(ctx, id, since time.Time) ([]ActivityPoint, error)` con `ActivityPoint{Hour time.Time; Checkins int}`
  - `store.UpdateDevice(ctx, id, DevicePatch{GroupID **uuid.UUID; AssignedUser *string; Tags *[]string}) error`; `RevokeDevice(ctx, id, at) error`; `DeleteDevice(ctx, id) error`
  - `store.Summary(ctx, now time.Time, checkinIntervalS int) (Summary, error)` con `Summary{Total int; ByStatus map[Status]int; ByPlatform map[string]int; OSVersions []OSCount; LowDisk, Unencrypted, Stale int; RecentEnrollments []DeviceRow}`
  - Rutas del apartado 8: `GET /summary`, `GET /devices`, `GET /devices.csv`, `GET /devices/{id}`, `GET /devices/{id}/software`, `GET /devices/{id}/activity`, `PATCH /devices/{id}`, `POST /devices/{id}/revoke`, `DELETE /devices/{id}`
  - Forma del listado: `{"items":[…],"total":n,"page":1,"per_page":25}`. Cada dispositivo: `id, hostname, platform, os_name, os_version, arch, serial, manufacturer, model, agent_version, status, group_id, group_name, assigned_user, tags, enrolled_at, last_seen_at, last_ip, inventory_at`. La ficha añade `inventory` (el JSON guardado) y `inventory_collected_at`.
  - CSV: UTF-8 con BOM; columnas `Nombre, Plataforma, Sistema, Versión, Serie, Fabricante, Modelo, Estado, Grupo, Usuario, Etiquetas, Última conexión, Inscrito`; nombre de fichero `dispositivos-AAAA-MM-DD.csv`; sin paginar.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestStatusOf(t *testing.T) {
    now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
    cases := []struct{ name string; lastSeen time.Duration; revoked bool; want Status }{
        {"recién conectado", 10 * time.Second, false, StatusOnline},
        {"179 s", 179 * time.Second, false, StatusOnline},
        {"180 s", 180 * time.Second, false, StatusOffline},
        {"29 días", 29 * 24 * time.Hour, false, StatusOffline},
        {"30 días", 30 * 24 * time.Hour, false, StatusInactive},
        {"revocado y en línea", 10 * time.Second, true, StatusRevoked},
    }
    // además: last_seen_at nulo → StatusOffline
}
func TestListDevices_Pagination(t *testing.T)      // 30 dispositivos: página 1 → 25 y total 30; página 2 → 5
func TestListDevices_PerPageCapped(t *testing.T)   // per_page=500 → 200; per_page=0 → 25
func TestListDevices_Search(t *testing.T)          // q coincide sin distinguir mayúsculas en hostname, serial y assigned_user
func TestListDevices_SearchEscapesWildcards(t *testing.T) // q="%" no devuelve todo
func TestListDevices_Filters(t *testing.T)         // platform, group_id, os_version, tag, y cada status coincide con StatusOf
func TestListDevices_Sort(t *testing.T)            // hostname asc/desc; sort desconocido → 400
func TestDevice_Detail(t *testing.T)               // incluye inventory y status; id inexistente → 404; id mal formado → 400
func TestDevice_Software(t *testing.T)             // paginado; q filtra por nombre
func TestDevice_Activity(t *testing.T)             // puntos por hora en orden ascendente
func TestDevice_Patch(t *testing.T)                // operador cambia grupo, usuario y etiquetas; group_id null lo quita; lectura → 403
func TestDevice_PatchTagsNormalized(t *testing.T)  // [" Ventas ","ventas",""] → ["ventas"]
func TestDevice_RevokeAndDelete_AdminOnly(t *testing.T) // operador → 403; admin → 204
func TestDevice_RevokeMakesAgentUnauthorized(t *testing.T) // tras revocar, el check-in del agente da 401
func TestDevice_DeleteCascades(t *testing.T)       // sin filas en device_inventory, device_software, device_activity; la auditoría permanece
func TestCSV_ContentAndHeaders(t *testing.T)       // Content-Type text/csv; charset=utf-8; BOM; cabecera exacta; respeta filtros; auditoría devices.exported
func TestCSV_FormulaInjectionNeutralized(t *testing.T) {
    // hostname "=HYPERLINK(\"http://x\")", assigned_user "+34 600", etiqueta "@todos", serie "-1":
    // cada celda sale con apóstrofo delante; un hostname normal no cambia
}
func TestSummary(t *testing.T) {
    // 2 en línea, 1 desconectado hace 8 días, 1 revocado; 1 con un volumen al 5 % libre;
    // 1 con security.disk_encryption "off":
    // total 4; by_status; by_platform; low_disk 1; unencrypted 1; stale 1; recent_enrollments ≤ 10 en orden descendente
}
func TestAudit_DeviceActions(t *testing.T)         // device.updated (con los campos cambiados), device.revoked, device.deleted
```

- [ ] **Step 2:** Run `go test ./internal/server/store/ ./internal/server/adminapi/ -run 'Status|Devices|Device_|CSV|Summary|Audit_Device'` — Expected: FAIL.
- [ ] **Step 3:** Implementar `StatusOf` y las consultas. El filtro por estado traduce los mismos umbrales a condiciones SQL sobre `last_seen_at` y `revoked_at`. La búsqueda usa `ILIKE` con `%`, `_` y `\` escapados. `LowDisk` cuenta dispositivos con algún elemento de `data->'disks'` donde `size_bytes > 0` y `free_bytes::numeric / size_bytes < 0.10`. `Stale` cuenta no revocados sin conexión desde hace 7 días o más.
- [ ] **Step 4:** Implementar los manejadores y el CSV. Las etiquetas se normalizan a minúsculas, sin espacios alrededor, sin vacías ni duplicadas, 20 como máximo y 32 caracteres cada una.
- [ ] **Step 5:** Run las mismas pruebas — Expected: PASS.
- [ ] **Step 6:** Commit `feat: API de dispositivos, resumen y exportación`.

---

### Task 10: API de consola — administradores y auditoría

**Files:**
- Create: `internal/server/adminapi/admins.go`, `adminapi/audit.go`
- Test: `internal/server/adminapi/admins_test.go`, `adminapi/audit_test.go`

**Interfaces:**
- Consumes: almacén de la tarea 5.
- Produces: `GET /admins`, `POST /admins` (`{"email","password","role"}`), `PATCH /admins/{id}` (`{"role","disabled"}`), `GET /audit` (`actor`, `action`, `from`, `to`, `page`, `per_page`). Todas exigen rol `admin`. Los listados nunca incluyen `password_hash` ni `totp_secret`; exponen `has_totp`.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestAdmins_OperatorForbidden(t *testing.T)          // GET, POST y PATCH → 403
func TestAdmins_Create(t *testing.T)                     // 201; contraseña de 11 caracteres → 400; correo repetido → 409; rol "root" → 400
func TestAdmins_ListHidesSecrets(t *testing.T)           // el cuerpo no contiene "password_hash" ni "totp_secret"; has_totp presente
func TestAdmins_CannotDisableOrDemoteSelf(t *testing.T)  // 409
func TestAdmins_CannotRemoveLastAdmin(t *testing.T)      // 409
func TestAdmins_DisableKillsSessions(t *testing.T)       // la sesión abierta del desactivado recibe 401 en la siguiente petición
func TestAdmins_AuditRecorded(t *testing.T)              // admin.created y admin.updated con el cambio en details
func TestAudit_ListFiltersAndPaginates(t *testing.T)     // filtra por action, actor y rango de fechas; orden descendente por fecha
func TestAudit_NoWriteRoutes(t *testing.T)               // POST, PATCH y DELETE sobre /audit → 405
```

- [ ] **Step 2:** Run `go test ./internal/server/adminapi/ -run 'Admins|Audit_List|Audit_No'` — Expected: FAIL.
- [ ] **Step 3:** Implementar.
- [ ] **Step 4:** Run las mismas pruebas — Expected: PASS.
- [ ] **Step 5:** Commit `feat: gestión de administradores y consulta de auditoría`.

---

### Task 11: Ensamblado del servidor y línea de órdenes

**Files:**
- Create: `internal/server/app/app.go`, `app/static.go`, `app/downloads.go`, `app/janitor.go`, `app/config.go`, `cmd/mdm-server/main.go`, `web/embed.go`, `web/dist/.gitkeep`
- Test: `internal/server/app/app_test.go`, `app/config_test.go`

**Interfaces:**
- Consumes: `adminapi.New`, `agentapi.New`, almacén.
- Produces:
  - `app.Config{DatabaseURL, ListenAddr, PublicURL, DownloadsDir string; TrustProxy bool}`; `app.LoadConfig(getenv func(string) string) (Config, error)` — `MDM_DATABASE_URL` y `MDM_PUBLIC_URL` obligatorias; `MDM_PUBLIC_URL` debe empezar por `https://`; valores por defecto `:8080` y `/downloads`
  - `app.NewHandler(cfg Config, s *store.Store, static fs.FS, now func() time.Time) http.Handler`
  - `app.RunJanitor(ctx, s *store.Store, every time.Duration, now func() time.Time)` — purga sesiones caducadas y actividad de más de 90 días
  - `web.Dist embed.FS` (`//go:embed all:dist`)
  - Rutas: `/agent/v1/*`, `/api/v1/*`, `GET /healthz` (200 `{"status":"ok"}`, o 503 si `Ping` falla), `GET /downloads/*` (solo ficheros de `DownloadsDir`, sin listado de directorio), y el resto sirve la consola con `index.html` como respuesta para rutas desconocidas
  - `NewHandler` crea los limitadores: inicio de sesión 5 cada 15 minutos; inscripción 10 por minuto
  - Cuando `Ping` falla, toda ruta de API responde 503 `unavailable`
  - Cabeceras en todas las respuestas: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`; en la consola, `Content-Security-Policy: default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'`
  - CLI: `mdm-server serve`; `mdm-server admin create --email <correo> [--role admin]` (contraseña por terminal sin eco, dos veces); `mdm-server admin reset-totp --email <correo>`; `mdm-server version`. `serve` aplica las migraciones al arrancar y se cierra con gracia ante SIGTERM en 15 s como máximo.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestLoadConfig(t *testing.T)                 // sin MDM_DATABASE_URL → error; MDM_PUBLIC_URL "http://x" → error; valores por defecto; MDM_TRUSTED_PROXY=true
func TestHandler_Healthz(t *testing.T)            // 200 {"status":"ok"}
func TestHandler_SPAFallback(t *testing.T)        // GET /dispositivos/abc devuelve index.html; GET /api/v1/nope → 404 JSON, no HTML
func TestHandler_DownloadsNoTraversal(t *testing.T) // /downloads/../secreto → 404; /downloads/ → 404; fichero existente → 200
func TestHandler_SecurityHeaders(t *testing.T)
func TestHandler_DatabaseDownReturns503(t *testing.T) // con el almacén cerrado: /api/v1/auth/me y /agent/v1/checkin → 503 "unavailable"
func TestJanitor_PurgesOldRows(t *testing.T)      // actividad de hace 91 días se borra, la de 89 queda; sesión caducada se borra
```

- [ ] **Step 2:** Run `go test ./internal/server/app/` — Expected: FAIL.
- [ ] **Step 3:** Implementar `app` y `main.go`.
- [ ] **Step 4:** Run `go test ./internal/server/app/` y `go build ./cmd/mdm-server` — Expected: PASS y binario generado.
- [ ] **Step 5:** Commit `feat: servidor ensamblado y línea de órdenes`.

---

### Task 12: Agente — configuración y cliente

**Files:**
- Create: `internal/agent/config/config.go`, `config/paths_linux.go`, `config/paths_windows.go`, `config/perm_unix.go`, `config/perm_windows.go`, `internal/agent/client/client.go`, `client/backoff.go`
- Test: `internal/agent/config/config_test.go`, `internal/agent/client/client_test.go`, `client/backoff_test.go`

**Interfaces:**
- Consumes: `protocol`.
- Produces:
  - `config.Config{ServerURL, DeviceID, Credential string; CheckinIntervalS, InventoryIntervalS int; Revoked bool}`
  - `config.State{LastSuccess *time.Time; LastError string; LastErrorAt *time.Time}`
  - `config.Load(path string) (Config, error)`; `config.Save(path string, c Config) error` (escritura atómica: fichero temporal y renombrado); `config.LoadState`, `config.SaveState`; `config.Remove(dir string) error`
  - `config.Dir() string`, `config.Path() string`, `config.StatePath() string` con las ubicaciones del apartado 4.2
  - En Unix el fichero queda en 0600 y el directorio en 0700. En Windows `Save` ejecuta `icacls <dir> /inheritance:r /grant:r "*S-1-5-18:(OI)(CI)F" "*S-1-5-32-544:(OI)(CI)F"` (identificadores de SYSTEM y Administradores, independientes del idioma del sistema).
  - `client.New(serverURL, credential string, hc *http.Client) (*Client, error)` — error si la URL no es `https://`; `hc` nulo usa un cliente con 30 s de tiempo límite
  - `(*Client).Enroll(ctx, protocol.EnrollRequest) (protocol.EnrollResponse, error)`; `(*Client).Checkin(ctx, protocol.CheckinRequest) (protocol.CheckinResponse, error)`; `(*Client).SendInventory(ctx, protocol.Inventory) error` (comprime con gzip)
  - Errores: `client.ErrRevoked` (401), `client.ErrTokenRejected` (403), `client.ErrTooLarge` (413), `*client.RetryAfterError{After time.Duration}` (429), `*client.TemporaryError{Err error}` (red, 5xx)
  - Cabecera `User-Agent: mdm-agent/<versión>`
  - `client.NewBackoff(rnd func() float64) *Backoff`; `(*Backoff).Next() time.Duration`; `(*Backoff).Reset()` — base 5 s, factor 2, máximo 15 min, variación ±20 %

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestConfig_SaveLoadRoundTrip(t *testing.T)
func TestConfig_FilePermissions(t *testing.T)      // (unix) modo 0600; directorio 0700
func TestConfig_LoadMissing(t *testing.T)          // errors.Is(err, fs.ErrNotExist)
func TestConfig_SaveIsAtomic(t *testing.T)         // no queda ningún *.tmp tras guardar

func TestClient_RejectsNonHTTPS(t *testing.T)      // New("http://mdm.example", …) → error
func TestClient_SendsBearer(t *testing.T)          // Authorization == "Bearer <credencial>"; Enroll no la envía
func TestClient_InventoryIsGzipped(t *testing.T)   // Content-Encoding gzip y el cuerpo descomprime al JSON original
func TestClient_ErrorMapping(t *testing.T) {
    // 401 → ErrRevoked; 403 → ErrTokenRejected; 413 → ErrTooLarge;
    // 429 con Retry-After: 120 → *RetryAfterError{After: 2 min};
    // 500, 502, 503 y conexión rechazada → *TemporaryError
}
func TestBackoff_Sequence(t *testing.T)            // con rnd fijo en 0,5 (sin variación): 5s, 10s, 20s, 40s, …, tope de 15 min
func TestBackoff_Jitter(t *testing.T)              // rnd = 0 → 4 s; rnd = 1 → 6 s en el primer paso
func TestBackoff_Reset(t *testing.T)               // tras Reset vuelve a 5 s
```

- [ ] **Step 2:** Run `go test ./internal/agent/config/ ./internal/agent/client/` — Expected: FAIL.
- [ ] **Step 3:** Implementar. Las pruebas del cliente usan `httptest.NewTLSServer` y su `Client()`.
- [ ] **Step 4:** Run las mismas pruebas y `GOOS=windows go build ./internal/agent/...` — Expected: PASS y compila.
- [ ] **Step 5:** Commit `feat: configuración y cliente del agente`.

---

### Task 13: Agente — inventario común y Linux

**Files:**
- Create: `internal/agent/inventory/inventory.go`, `inventory/hardwareid.go`, `inventory/network.go`, `inventory/linux.go`, `inventory/linux_parse.go`, `inventory/testdata/linux/…`
- Test: `internal/agent/inventory/hardwareid_test.go`, `inventory/linux_parse_test.go`, `inventory/linux_test.go`

**Interfaces:**
- Consumes: `protocol`.
- Produces:
  - `type Collector interface { Identify(ctx context.Context) protocol.EnrollRequest; Collect(ctx context.Context) protocol.Inventory }` — `Identify` deja vacíos `Token` y `AgentVersion`; `Collect` devuelve `Hash` ya calculado con `protocol.InventoryHash` y `CollectedAt` en UTC
  - `inventory.New() Collector` (un fichero por plataforma con etiqueta de compilación)
  - `chooseHardwareID(smbios, fallback string) string`
  - `collectNetwork() []protocol.NetIf` — omite interfaces de bucle local y las que no están activas
  - En Linux: `newLinux(root string, run func(ctx context.Context, name string, args ...string) ([]byte, error)) *linuxCollector`, para que las pruebas apunten a un árbol de `testdata`
  - Analizadores puros en `linux_parse.go`: `parseOSRelease([]byte) (name, version string)`, `parseCPUInfo([]byte) (model string, cores, threads int)`, `parseMemTotal([]byte) uint64`, `parseDpkg([]byte) []protocol.Software`, `parseRPM([]byte) []protocol.Software`, `parseLsblkCrypt([]byte) map[string]bool`, `parseMounts([]byte) []mount`, `firewallState(ufw, firewalld, nft []byte) string`

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestChooseHardwareID(t *testing.T) {
    cases := []struct{ smbios, fallback, want string }{
        {"4C4C4544-0042-3510-8054-B4C04F564433", "abc", "4c4c4544-0042-3510-8054-b4c04f564433"},
        {"", "abc", "abc"},
        {"00000000-0000-0000-0000-000000000000", "abc", "abc"},
        {"FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF", "abc", "abc"},
        {"03000200-0400-0500-0006-000700080009", "abc", "abc"},
        {"Not Settable", "abc", "abc"},
        {"To Be Filled By O.E.M.", "abc", "abc"},
    }
}
func TestParseOSRelease(t *testing.T)    // Ubuntu 24.04 → ("Ubuntu", "24.04"); Rocky 9.4 → ("Rocky Linux", "9.4"); valores con y sin comillas
func TestParseCPUInfo(t *testing.T)      // x86 con 2 sockets × 4 núcleos × 2 hilos → cores 8, threads 16; ARM sin "model name" usa "Hardware" o "CPU part"
func TestParseMemTotal(t *testing.T)     // "MemTotal: 16308180 kB" → 16699576320
func TestParseDpkg(t *testing.T)         // solo estado "install ok installed"; nombre, versión y mantenedor como editor
func TestParseRPM(t *testing.T)          // nombre, versión-lanzamiento, vendedor y fecha de instalación
func TestParseLsblkCrypt(t *testing.T)   // JSON de lsblk con un volumen crypt bajo nvme0n1p3 → el punto de montaje "/" figura como cifrado
func TestParseMounts(t *testing.T)       // excluye tmpfs, proc, sysfs, devtmpfs, overlay, squashfs y cgroup
func TestFirewallState(t *testing.T)     // ufw "Status: active" → on; firewalld "running" → on; nft con reglas → on; nada → off; sin herramientas → unknown
func TestLinuxCollect_FullTree(t *testing.T)   // con testdata/linux/ubuntu: sistema, hardware, discos y software poblados; Unavailable vacío; Hash coincide con InventoryHash
func TestLinuxCollect_PartialFailure(t *testing.T) // run falla para dpkg-query y rpm: Unavailable == ["software"] y el resto poblado
func TestLinuxCollect_DiskEncryptionSummary(t *testing.T) // todos cifrados → on; ninguno → off; mezcla → partial
func TestLinuxIdentify(t *testing.T)     // platform "linux"; hardware_id sale de product_uuid, o de machine-id si es genérico
```

- [ ] **Step 2:** Run `go test ./internal/agent/inventory/` — Expected: FAIL.
- [ ] **Step 3:** Crear `testdata/linux/ubuntu` y `testdata/linux/rocky` con `etc/os-release`, `proc/cpuinfo`, `proc/meminfo`, `proc/mounts`, `sys/class/dmi/id/*`, `etc/machine-id` y salidas grabadas de `dpkg-query -W -f='${Package}\t${Version}\t${Maintainer}\t${Status}\n'`, `rpm -qa --qf '%{NAME}\t%{VERSION}-%{RELEASE}\t%{VENDOR}\t%{INSTALLTIME}\n'` y `lsblk -J -o NAME,TYPE,MOUNTPOINT`.
- [ ] **Step 4:** Implementar. Cada sección se recoge en su propia función; si devuelve error, su nombre va a `Unavailable` y la recogida continúa. El arranque seguro se lee del último byte de `sys/firmware/efi/efivars/SecureBoot-*`; sin EFI, `unknown`. El antivirus es siempre `unknown`. El usuario con sesión es el primero de `who`.
- [ ] **Step 5:** Run `go test ./internal/agent/inventory/` — Expected: PASS.
- [ ] **Step 6:** Commit `feat: inventario de Linux`.

---

### Task 14: Agente — inventario de Windows

**Files:**
- Create: `internal/agent/inventory/windows.go`, `inventory/windows_parse.go`
- Test: `internal/agent/inventory/windows_parse_test.go`

**Interfaces:**
- Consumes: `Collector`, `chooseHardwareID`, `collectNetwork` (tarea 13).
- Produces:
  - `inventory.New()` para Windows (etiqueta `//go:build windows`)
  - Funciones puras en `windows_parse.go` (sin etiqueta de compilación, para probarse en cualquier sistema):
    - `type uninstallEntry struct{ DisplayName, DisplayVersion, Publisher, InstallDate string; SystemComponent uint64; ParentKeyName string }`
    - `softwareFromUninstall(entries []uninstallEntry) []protocol.Software`
    - `parseInstallDate(s string) *time.Time` (formato `AAAAMMDD`)
    - `bitlockerState(protectionStatus []uint32) string`
    - `firewallFromProfiles(domain, private, public *uint32) string`
    - `antivirusFromProducts(products []avProduct) (state, name string)` con `avProduct{DisplayName string; ProductState uint32}`
    - `windowsOSName(caption string) string`

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestSoftwareFromUninstall(t *testing.T) {
    // omite entradas sin DisplayName, con SystemComponent == 1 o con ParentKeyName;
    // elimina duplicados (mismo nombre y versión en las ramas de 64 y 32 bits);
    // ordena por nombre
}
func TestParseInstallDate(t *testing.T)     // "20260115" → 15 de enero de 2026 UTC; "", "0" y "2026-01-15" → nil
func TestBitlockerState(t *testing.T)       // [1,1] → on; [0,0] → off; [1,0] → partial; [] → unknown
func TestFirewallFromProfiles(t *testing.T) // los tres a 1 → on; los tres a 0 → off; mezcla → partial; todos nil → unknown
func TestAntivirusFromProducts(t *testing.T) {
    // ProductState 0x061100 (activado) → ("on", "Microsoft Defender");
    // 0x060100 (desactivado) → ("off", nombre);
    // lista vacía → ("unknown", "");
    // con varios, basta uno activado para "on" y se devuelve su nombre
}
func TestWindowsOSName(t *testing.T)        // "Microsoft Windows 11 Pro" → "Windows 11 Pro"
```

- [ ] **Step 2:** Run `go test ./internal/agent/inventory/ -run 'Uninstall|InstallDate|Bitlocker|Firewall|Antivirus|WindowsOS'` — Expected: FAIL.
- [ ] **Step 3:** Implementar las funciones puras. El antivirus está activado cuando `(ProductState >> 12) & 0xF == 1`.
- [ ] **Step 4: Implementar `windows.go`** con las fuentes del apartado 7.4. WMI con `wmi.Query` (y `wmi.QueryNamespace` para `root\CIMV2\Security\MicrosoftVolumeEncryption` y `root\SecurityCenter2`). Software: `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall` y su equivalente bajo `WOW6432Node`. Cortafuegos: `EnableFirewall` en `HKLM\SYSTEM\CurrentControlSet\Services\SharedAccess\Parameters\FirewallPolicy\{DomainProfile,StandardProfile,PublicProfile}`. Arranque seguro: `UEFISecureBootEnabled` en `HKLM\SYSTEM\CurrentControlSet\Control\SecureBoot\State`. Identificador alternativo: `MachineGuid` en `HKLM\SOFTWARE\Microsoft\Cryptography`. Si `SecurityCenter2` no existe (Windows Server), el antivirus es `unknown` y la sección no se marca como no disponible.
- [ ] **Step 5:** Run las pruebas del paso 2, `GOOS=windows GOARCH=amd64 go vet ./internal/agent/inventory/` y `GOOS=windows GOARCH=arm64 go build ./internal/agent/inventory/` — Expected: PASS, sin avisos, compila.
- [ ] **Step 6:** Commit `feat: inventario de Windows`.

---

### Task 15: Agente — inscripción, bucle y estado

**Files:**
- Create: `internal/agent/enroll/enroll.go`, `internal/agent/loop/loop.go`
- Test: `internal/agent/enroll/enroll_test.go`, `internal/agent/loop/loop_test.go`

**Interfaces:**
- Consumes: `config`, `client` (tarea 12); `inventory.Collector` (tarea 13).
- Produces:
  - `enroll.Options{ServerURL, Token, Version, ConfigPath string; Collector inventory.Collector; HTTPClient *http.Client; InstallService func() error}`
  - `enroll.Run(ctx, o Options) error`; `enroll.ExitCode(err error) int` → 0 sin error, 2 para `client.ErrTokenRejected`, 3 para `*client.TemporaryError`, 1 en otro caso
  - `loop.API` (interfaz con `Checkin` y `SendInventory`, satisfecha por `*client.Client`)
  - `loop.Deps{API loop.API; Collector inventory.Collector; Config config.Config; SaveConfig func(config.Config) error; SaveState func(config.State) error; Sleep func(ctx context.Context, d time.Duration) error; Rand func() float64; Now func() time.Time; Version string; Log *slog.Logger}`
  - `loop.Run(ctx, d Deps) error` — solo vuelve cuando el contexto se cancela (devuelve `nil`)

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
// enroll
func TestEnroll_SavesConfigAndInstallsService(t *testing.T) // configuración con ServerURL, DeviceID, Credential e intervalos; InstallService llamado una vez
func TestEnroll_TokenRejected(t *testing.T)   // servidor 403: ExitCode == 2; no existe el fichero; InstallService no se llama
func TestEnroll_NetworkFailure(t *testing.T)  // servidor cerrado: ExitCode == 3; sin fichero
func TestEnroll_RejectsHTTP(t *testing.T)     // ServerURL "http://…": ExitCode == 1 y el mensaje menciona https
func TestEnroll_ServiceInstallFailureRemovesConfig(t *testing.T) // InstallService falla: se borra la configuración y se devuelve el error
func TestEnroll_SendsIdentity(t *testing.T)   // la petición lleva token, agent_version y lo que devuelve Collector.Identify

// loop (API falsa que registra llamadas; Sleep falso que anota duraciones y cancela tras N iteraciones)
func TestLoop_ChecksInAtInterval(t *testing.T)        // Rand = 0,5: duerme exactamente 60 s entre check-ins
func TestLoop_JitterTenPercent(t *testing.T)          // Rand = 0 → 54 s; Rand = 1 → 66 s
func TestLoop_SendsInventoryWhenAsked(t *testing.T)   // send_inventory true → una llamada a SendInventory con el inventario del Collector
func TestLoop_NoInventoryWhenNotAsked(t *testing.T)
func TestLoop_CheckinCarriesCurrentHash(t *testing.T) // InventoryHash del check-in == Hash del Collector
func TestLoop_AppliesServerConfig(t *testing.T)       // la respuesta trae 120 s: SaveConfig recibe 120 y la siguiente espera es de 120 s
func TestLoop_IgnoresZeroOrNegativeIntervals(t *testing.T) // la respuesta trae 0: se mantiene 60 s
func TestLoop_BackoffOnTemporaryError(t *testing.T)   // tres TemporaryError seguidos: 5 s, 10 s, 20 s; tras un éxito, 60 s
func TestLoop_RetryAfterHonored(t *testing.T)         // RetryAfterError{2 min} → duerme 2 min
func TestLoop_RevokedStopsSending(t *testing.T) {
    // Checkin devuelve ErrRevoked: SaveConfig con Revoked == true;
    // no hay más llamadas a la API; Run sigue vivo hasta cancelar el contexto
}
func TestLoop_StartsRevokedStaysIdle(t *testing.T)    // Config.Revoked == true al empezar: cero llamadas a la API
func TestLoop_InventoryTooLargeSkipsUntilNextCycle(t *testing.T) // ErrTooLarge: no reintenta en el mismo ciclo; el check-in siguiente sigue normal
func TestLoop_IgnoresCommands(t *testing.T)           // respuesta con commands no vacío: sin efecto ni error
func TestLoop_WritesState(t *testing.T)               // LastSuccess tras un éxito; LastError y LastErrorAt tras un fallo
func TestLoop_FirstCheckinImmediate(t *testing.T)     // el primer check-in ocurre antes de la primera espera
```

- [ ] **Step 2:** Run `go test ./internal/agent/enroll/ ./internal/agent/loop/` — Expected: FAIL.
- [ ] **Step 3:** Implementar. El bucle calcula la huella llamando a `Collector.Collect` como máximo una vez cada 5 minutos y reutiliza el resultado entre check-ins.
- [ ] **Step 4:** Run las mismas pruebas con `-race` — Expected: PASS.
- [ ] **Step 5:** Commit `feat: inscripción y bucle del agente`.

---

### Task 16: Agente — servicio, línea de órdenes y scripts de instalación

**Files:**
- Create: `internal/agent/service/service.go`, `service/service_linux.go`, `service/service_windows.go`, `service/unit.go`, `cmd/mdm-agent/main.go`, `cmd/mdm-agent/logging_windows.go`, `cmd/mdm-agent/logging_other.go`, `scripts/install.sh`, `scripts/install.ps1`
- Test: `internal/agent/service/unit_test.go`, `cmd/mdm-agent/main_test.go`, `scripts/install_test.sh`

**Interfaces:**
- Consumes: tareas 12 a 15.
- Produces:
  - `service.Install(binPath string) error`; `service.Uninstall() error`; `service.Run(run func(ctx context.Context) error) error` (en Windows, bajo el gestor de servicios; en Linux, atiende SIGTERM)
  - `service.Name = "MDMAgent"` (Windows) y `"mdm-agent"` (systemd)
  - `unitFile(binPath string) string`
  - CLI: `enroll --server --token`, `run`, `status`, `uninstall`, `version`. Todos salvo `version` exigen privilegios de administrador y, si faltan, terminan con código 1 y «Hace falta ejecutar como administrador (root).»
  - `enroll` copia el ejecutable a su ubicación definitiva si se lanza desde otra ruta
  - Salida de `status`:
    ```
    Servidor:         https://mdm.ejemplo.es
    Dispositivo:      <uuid>
    Estado:           activo | revocado | sin inscribir
    Última conexión:  2026-11-13 10:02:11 (hace 40 s)
    Último error:     ninguno
    ```
  - `install.sh --server <url> --token <token>`: detecta `amd64` o `arm64`, descarga `<url>/downloads/mdm-agent-linux-<arch>` y `<url>/downloads/SHA256SUMS`, comprueba la suma, y ejecuta `enroll`. `install.ps1 -Server -Token` hace lo mismo con `mdm-agent-windows-<arch>.exe`. Ambos terminan con el código de salida de `enroll` y borran la descarga si la suma no coincide.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestUnitFile(t *testing.T) {
    u := unitFile("/usr/local/bin/mdm-agent")
    // contiene: "ExecStart=/usr/local/bin/mdm-agent run", "Restart=always", "RestartSec=10",
    // "After=network-online.target", "Wants=network-online.target", "WantedBy=multi-user.target",
    // "User=root", "ProtectSystem=strict", "ReadWritePaths=/etc/mdm-agent", "NoNewPrivileges=true"
}
func TestCLI_UnknownCommand(t *testing.T)      // código 1 y muestra la ayuda
func TestCLI_EnrollRequiresFlags(t *testing.T) // sin --server o sin --token → código 1 y dice cuál falta
func TestCLI_Version(t *testing.T)             // imprime la versión inyectada
func TestCLI_StatusNotEnrolled(t *testing.T)   // sin configuración: "Estado:           sin inscribir" y código 0
func TestFormatStatus(t *testing.T)            // salida exacta para activo con última conexión y para revocado
func TestUninstall_RemovesServiceAndConfig(t *testing.T) // con service.Uninstall sustituido: se llama una vez y el directorio de configuración desaparece; repetirlo no da error
```

`scripts/install_test.sh` levanta `python3 -m http.server` con TLS sobre un directorio temporal y comprueba: suma correcta → se invoca el binario (un sustituto que anota sus argumentos) con `enroll --server … --token …`; suma incorrecta → código distinto de cero, mensaje «La suma de comprobación no coincide» y la descarga se borra; arquitectura desconocida → mensaje claro.

- [ ] **Step 2:** Run `go test ./internal/agent/service/ ./cmd/mdm-agent/` y `sh scripts/install_test.sh` — Expected: FAIL.
- [ ] **Step 3:** Implementar `service`. En Linux escribe `/etc/systemd/system/mdm-agent.service`, ejecuta `systemctl daemon-reload` y `systemctl enable --now mdm-agent`. En Windows usa `golang.org/x/sys/windows/svc/mgr`: inicio automático, cuenta LocalSystem, nombre visible «Agente MDM», y acciones de recuperación que reinician a los 10 s.
- [ ] **Step 4:** Implementar `main.go`, el registro (journald por salida estándar en Linux; fichero con rotación a 5 MB y 3 copias en Windows) y los dos scripts.
- [ ] **Step 5:** Run las pruebas del paso 2, `make build` y `shellcheck scripts/install.sh` — Expected: PASS, cuatro binarios de agente en `dist/`, sin avisos.
- [ ] **Step 6:** Commit `feat: servicio, línea de órdenes e instaladores del agente`.

---

### Task 17: Consola — base, cliente de API e inicio de sesión

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/index.html`, `web/src/main.tsx`, `web/src/App.tsx`, `web/src/styles.css`, `web/src/api/client.ts`, `web/src/api/types.ts`, `web/src/auth/AuthContext.tsx`, `web/src/pages/Login.tsx`, `web/src/components/Layout.tsx`, `web/src/lib/format.ts`
- Test: `web/src/api/client.test.ts`, `web/src/pages/Login.test.tsx`, `web/src/lib/format.test.ts`, `web/src/App.test.tsx`

**Interfaces:**
- Consumes: rutas `/api/v1/auth/*` (tarea 5).
- Produces:
  - Dependencias: `react`, `react-dom`, `react-router-dom` 6, `@tanstack/react-query` 5, `qrcode`; desarrollo: `vite`, `typescript`, `vitest`, `@testing-library/react`, `@testing-library/user-event`, `jsdom`. Sin biblioteca de componentes: CSS propio con variables y tema claro y oscuro según `prefers-color-scheme`.
  - `api<T>(method: string, path: string, body?: unknown): Promise<T>` — antepone `/api/v1`, envía `X-CSRF-Token` en métodos distintos de GET, lanza `ApiError`
  - `class ApiError extends Error { status: number; code: string }`
  - `setCsrfToken(t: string | null)`; `onUnauthorized(cb: () => void)`
  - `types.ts`: `Role`, `Me`, `Device`, `DeviceDetail`, `Inventory`, `Group`, `EnrollmentToken`, `Admin`, `AuditEntry`, `Summary`, `Page<T>`, con los nombres de campo de las tareas 5 a 10
  - `useAuth(): { me: Me | null; loading: boolean; refresh(): Promise<void>; logout(): Promise<void>; can(min: Role): boolean }`
  - `formatBytes(n: number): string` (binario, coma decimal: `1,5 GB`); `relativeTime(iso: string | null, now?: Date): string` («hace 40 s», «hace 3 min», «hace 2 h», «hace 5 días», «nunca»); `formatDateTime(iso: string): string`; `statusLabel(s: Status): string` («En línea», «Desconectado», «Inactivo», «Revocado»)
  - Rutas: `/login`, `/` (resumen), `/dispositivos`, `/dispositivos/:id`, `/grupos`, `/inscripcion`, `/administradores`, `/auditoria`. Las dos últimas solo aparecen en el menú y solo son accesibles con rol `admin`; `/inscripcion` exige `operator`.
  - `Layout`: barra lateral con el menú, correo y rol del administrador, y botón «Cerrar sesión»
  - Vite: en desarrollo, intermediario de `/api` hacia `https://localhost:8443`; `build.outDir = "dist"`

- [ ] **Step 1: Escribir las pruebas que fallan**

```ts
// client.test.ts
it("antepone /api/v1 y envía JSON")
it("envía X-CSRF-Token en POST y no en GET")
it("lanza ApiError con status, code y message del cuerpo de error")
it("llama a onUnauthorized ante un 401 fuera de /auth/login")
it("devuelve undefined con un 204")

// format.test.ts
it("formatBytes", () => { expect(formatBytes(0)).toBe("0 B"); expect(formatBytes(1610612736)).toBe("1,5 GB") })
it("relativeTime", () => { /* 40 s → "hace 40 s"; 3 min; 2 h; 5 días; null → "nunca" */ })

// Login.test.tsx
it("pide correo y contraseña y, con next=totp, pasa al paso del código")
it("con next=totp_setup muestra el QR y la clave en texto para copiarla")
it("muestra «Correo o contraseña incorrectos.» ante un 401")
it("muestra el tiempo de espera ante un 429")
it("tras verificar el código, navega a la ruta de origen")

// App.test.tsx
it("sin sesión, redirige a /login conservando la ruta pedida")
it("un operador no ve «Administradores» ni «Auditoría» en el menú")
it("un rol de solo lectura que abre /auditoria ve «No tienes permiso para ver esta página.»")
```

- [ ] **Step 2:** Run `cd web && npm install && npx vitest run` — Expected: FAIL.
- [ ] **Step 3:** Implementar.
- [ ] **Step 4:** Run `npx vitest run && npx tsc --noEmit && npm run build` — Expected: PASS y `web/dist/index.html` generado.
- [ ] **Step 5:** Commit `feat: base de la consola e inicio de sesión`.

---

### Task 18: Consola — resumen, dispositivos y ficha

**Files:**
- Create: `web/src/pages/Summary.tsx`, `pages/Devices.tsx`, `pages/DeviceDetail.tsx`, `web/src/components/DataTable.tsx`, `components/Pagination.tsx`, `components/StatusBadge.tsx`, `components/Tabs.tsx`, `components/ConfirmDialog.tsx`, `components/ActivityChart.tsx`, `components/EmptyState.tsx`
- Test: `web/src/pages/Devices.test.tsx`, `pages/DeviceDetail.test.tsx`, `pages/Summary.test.tsx`, `components/ConfirmDialog.test.tsx`

**Interfaces:**
- Consumes: `api`, `useAuth`, `format` (tarea 17); rutas de la tarea 9; `GET /groups` (tarea 6).
- Produces:
  - `DataTable<T>({ columns, rows, sort, order, onSort, loading, empty })`; `Pagination({ page, perPage, total, onChange })`; `StatusBadge({ status })` (texto además de color); `Tabs({ tabs, active, onChange })`; `ConfirmDialog({ title, body, confirmLabel, danger, requireText?, onConfirm, onCancel })`
  - En `Devices`, filtros, página y orden viven en la cadena de consulta de la URL, de modo que un enlace reproduce la vista. La búsqueda espera 300 ms tras la última tecla. El botón «Exportar CSV» enlaza a `/api/v1/devices.csv` con los filtros actuales.
  - En `DeviceDetail`, pestañas: General, Hardware, Almacenamiento, Red, Software, Seguridad, Actividad. Las secciones presentes en `inventory.unavailable` muestran «No se pudo recoger este dato en el último inventario.»
  - En `Summary`, cada cifra enlaza al listado ya filtrado. Los datos se refrescan cada 30 s.

- [ ] **Step 1: Escribir las pruebas que fallan**

```ts
// Devices.test.tsx
it("muestra nombre, plataforma, sistema, estado, grupo, usuario y última conexión")
it("escribir en la búsqueda pide ?q=… tras 300 ms y vuelve a la página 1")
it("elegir «Windows» añade platform=windows a la URL y a la petición")
it("pulsar la cabecera «Nombre» alterna asc y desc")
it("carga los filtros desde la URL al abrir")
it("sin resultados muestra «Ningún dispositivo coincide con los filtros.» y un botón para quitarlos")
it("sin dispositivos ni filtros muestra «Aún no hay dispositivos inscritos.» con enlace a Inscripción")
it("el enlace de exportación lleva los filtros actuales")

// DeviceDetail.test.tsx
it("la pestaña Almacenamiento muestra cada volumen con usado, libre y cifrado")
it("marca en rojo un volumen con menos del 10 % libre")
it("la pestaña Software filtra por nombre y pagina")
it("una sección no disponible muestra el aviso en lugar de datos vacíos")
it("un operador edita grupo, usuario y etiquetas y ve «Cambios guardados.»")
it("un rol de solo lectura no ve controles de edición")
it("solo un administrador ve «Revocar» y «Eliminar»")
it("eliminar exige escribir el nombre del equipo antes de habilitar el botón")
it("un 404 muestra «Este dispositivo ya no existe.» con enlace al listado")

// Summary.test.tsx
it("muestra totales por estado y por plataforma")
it("«Poco espacio en disco», «Sin cifrar» y «Sin conexión reciente» muestran su cifra")
it("la cifra «En línea» enlaza a /dispositivos?status=online")

// ConfirmDialog.test.tsx
it("con requireText, el botón queda deshabilitado hasta escribir el texto exacto")
it("Escape cancela y el foco vuelve al elemento que abrió el diálogo")
```

- [ ] **Step 2:** Run `cd web && npx vitest run` — Expected: FAIL.
- [ ] **Step 3:** Implementar. Textos de confirmación: revocar — título «Revocar este dispositivo», cuerpo «El agente dejará de comunicarse con el servidor hasta que se vuelva a inscribir. El equipo y su inventario se conservan.», botón «Revocar»; eliminar — título «Eliminar este dispositivo», cuerpo «Se borrarán el equipo, su inventario y su actividad. Esta acción no se puede deshacer.», botón «Eliminar».
- [ ] **Step 4:** Run `npx vitest run && npx tsc --noEmit` — Expected: PASS.
- [ ] **Step 5:** Commit `feat: resumen, listado y ficha de dispositivo`.

---

### Task 19: Consola — grupos, inscripción, administradores y auditoría

**Files:**
- Create: `web/src/pages/Groups.tsx`, `pages/Enrollment.tsx`, `pages/Admins.tsx`, `pages/Audit.tsx`, `web/src/components/CopyField.tsx`, `web/src/lib/auditLabels.ts`
- Test: `web/src/pages/Groups.test.tsx`, `pages/Enrollment.test.tsx`, `pages/Admins.test.tsx`, `pages/Audit.test.tsx`

**Interfaces:**
- Consumes: componentes de la tarea 18; rutas de las tareas 6 y 10.
- Produces:
  - `CopyField({ label, value })` con botón «Copiar» que pasa a «Copiado» durante 2 s
  - `auditLabel(action: string): string` — una frase en español por cada constante de acción de la tarea 5 (por ejemplo `device.revoked` → «Revocó un dispositivo»); una acción desconocida se muestra tal cual

- [ ] **Step 1: Escribir las pruebas que fallan**

```ts
// Groups.test.tsx
it("lista grupos con su número de dispositivos")
it("crear un grupo con nombre repetido muestra «Ya existe un grupo con ese nombre.»")
it("eliminar avisa de que los dispositivos quedarán sin grupo y no se borran")

// Enrollment.test.tsx
it("crear un token muestra el token y los dos comandos una sola vez, con aviso de que no volverán a verse")
it("al cerrar el aviso el token ya no aparece en la página")
it("la tabla muestra estado (Activo, Caducado, Agotado, Revocado), usos «3 de 50» o «3» y caducidad")
it("revocar pide confirmación y actualiza el estado")
it("el formulario ofrece caducidad en 1, 7 o 30 días o sin caducidad, y 7 días por defecto")

// Admins.test.tsx
it("crear con contraseña de 11 caracteres muestra «La contraseña debe tener al menos 12 caracteres.» sin enviar")
it("no ofrece desactivar ni cambiar el rol de uno mismo")
it("un 409 al desactivar muestra «Debe quedar al menos un administrador activo.»")
it("indica quién no ha configurado aún el segundo factor")

// Audit.test.tsx
it("muestra fecha, autor, acción traducida, objetivo e IP")
it("filtra por acción y por rango de fechas")
it("una fila se despliega para ver los detalles")
```

- [ ] **Step 2:** Run `cd web && npx vitest run` — Expected: FAIL.
- [ ] **Step 3:** Implementar.
- [ ] **Step 4:** Run `npx vitest run && npx tsc --noEmit && npm run build` — Expected: PASS.
- [ ] **Step 5:** Commit `feat: grupos, inscripción, administradores y auditoría en la consola`.

---

### Task 20: Despliegue, documentación y prueba de extremo a extremo

**Files:**
- Create: `deploy/Dockerfile`, `deploy/docker-compose.yml`, `deploy/Caddyfile`, `deploy/.env.example`, `docs/despliegue.md`, `docs/agente.md`, `docs/validacion-windows.md`, `README.md`, `scripts/e2e.sh`, `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: todo lo anterior.
- Produces:
  - `Dockerfile` en tres etapas: Node 22 compila `web/dist`; Go 1.23 compila el servidor y los cuatro agentes y genera `SHA256SUMS`; imagen final `gcr.io/distroless/static-debian12:nonroot` con `/mdm-server` y `/downloads` (agentes, `SHA256SUMS`, `install.sh`, `install.ps1`)
  - `docker-compose.yml`: `db` (`postgres:16`, volumen `pgdata`, `healthcheck` con `pg_isready`), `server` (depende de `db` sano; `MDM_TRUSTED_PROXY=true`; no publica puertos), `caddy` (publica 80 y 443; volúmenes `caddy_data` y `caddy_config`). `restart: unless-stopped` en los tres.
  - `Caddyfile`: `{$MDM_DOMAIN} { encode gzip; reverse_proxy server:8080 }`
  - `.env.example`: `MDM_DOMAIN`, `POSTGRES_PASSWORD`
  - `scripts/e2e.sh`: levanta el despliegue con un dominio local y la autoridad interna de Caddy, crea un administrador, crea un token por la API, lanza un contenedor `ubuntu:24.04` con systemd, ejecuta `install.sh`, y comprueba por la API que en menos de 120 s existe un dispositivo `online` con software inventariado. Después lo revoca y comprueba que `mdm-agent status` informa «revocado».
  - CI: `go vet`, `go test -race ./...` con un servicio PostgreSQL 16, `GOOS=windows go build ./...`, `npx vitest run`, `npx tsc --noEmit`, `shellcheck`
  - `docs/despliegue.md`: requisitos (2 vCPU, 4 GB, dominio con DNS apuntando al servidor, puertos 80 y 443), puesta en marcha, alta del primer administrador con `docker compose exec server /mdm-server admin create --email …`, actualización, copia de seguridad (`pg_dump`) y restauración, y recuperación del segundo factor
  - `docs/agente.md`: instalación con el comando de la consola, despliegue masivo (GPO o script de inicio en Windows; Ansible o similar en Linux), `status`, desinstalación, ubicaciones y solución de problemas (certificado no válido, token rechazado, equipo revocado)
  - `docs/validacion-windows.md`: lista de comprobación con casillas — instalación en Windows 11 y en Windows Server 2022; el servicio «Agente MDM» arranca solo tras reiniciar; un usuario sin privilegios no puede leer `agent.json`; el inventario muestra BitLocker, cortafuegos, antivirus y arranque seguro correctos; el software coincide con «Aplicaciones instaladas»; revocar y reinscribir conserva el registro; `uninstall` no deja servicio ni ficheros

- [ ] **Step 1:** Escribir `scripts/e2e.sh` con las comprobaciones descritas.
- [ ] **Step 2:** Run `sh scripts/e2e.sh` — Expected: FAIL (no existe el despliegue).
- [ ] **Step 3:** Escribir `Dockerfile`, `docker-compose.yml`, `Caddyfile` y `.env.example`.
- [ ] **Step 4:** Run `sh scripts/e2e.sh` — Expected: termina con «Prueba de extremo a extremo correcta».
- [ ] **Step 5:** Escribir la documentación y la CI.
- [ ] **Step 6:** Commit `feat: despliegue, documentación y prueba de extremo a extremo`.

---

### Task 21: Prueba de carga

**Files:**
- Create: `scripts/loadtest/main.go`, `scripts/loadtest/README.md`
- Test: `scripts/loadtest/main_test.go`

**Interfaces:**
- Consumes: `client`, `protocol`.
- Produces: `go run ./scripts/loadtest --server <url> --token <token> --agents 1000 --duration 10m [--insecure]`. Inscribe N agentes simulados con `hardware_id` únicos, cada uno hace check-in cada 60 s (±10 %) y envía un inventario sintético de 150 programas cuando el servidor lo pide. Al terminar imprime peticiones totales, errores y percentiles 50, 95 y 99 del check-in, y sale con código 1 si hay algún error o si el percentil 99 supera los 200 ms.

- [ ] **Step 1: Escribir las pruebas que fallan**

```go
func TestPercentile(t *testing.T)        // sobre 1..100 ms: p50 == 50 ms, p95 == 95 ms, p99 == 99 ms; lista vacía → 0
func TestVerdict(t *testing.T)           // p99 de 199 ms y 0 errores → apto; 201 ms → no apto; 1 error → no apto
func TestSyntheticInventory(t *testing.T) // 150 programas; huella estable entre llamadas con el mismo índice de agente
```

- [ ] **Step 2:** Run `go test ./scripts/loadtest/` — Expected: FAIL.
- [ ] **Step 3:** Implementar. Las inscripciones se escalonan a 20 por segundo. El README explica cómo crear el token, cómo interpretar el resultado y cómo borrar después los dispositivos simulados (su nombre empieza por `carga-`).
- [ ] **Step 4:** Run `go test ./scripts/loadtest/` — Expected: PASS. Después, contra el despliegue de la tarea 20: `go run ./scripts/loadtest --agents 1000 --duration 10m` — Expected: 0 errores y percentil 99 por debajo de 200 ms.
- [ ] **Step 5:** Commit `test: simulador de carga de 1.000 agentes`.
