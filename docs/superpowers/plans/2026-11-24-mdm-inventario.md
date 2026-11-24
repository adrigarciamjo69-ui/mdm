# Inventario, licencias, usuarios de Entra y actas — plan de implementación

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unir en el MDM el inventario de activos (con agente y manuales), las licencias, los usuarios de Entra ID y las actas de entrega, según el diseño.

**Architecture:** Tabla `assets` como base de todo; cada equipo con agente tiene un activo enlazado por `device_id`, que se crea o se une por número de serie en la misma transacción que la inscripción. Usuarios sincronizados de Microsoft Graph por una tarea periódica. Licencias y actas apuntan a activos y a usuarios del directorio. La consola añade Inventario (sustituye a Dispositivos), Usuarios, Licencias, Doc. Entrega y Papelera; el PDF se genera en el navegador con jsPDF.

**Tech Stack:** Go 1.23, PostgreSQL 16 (pgx, goose), chi; React + TypeScript + Vite, TanStack Query, Vitest; jsPDF.

**Spec:** `docs/superpowers/specs/2026-11-24-mdm-inventario-design.md`

## Global Constraints

- Migración nueva `migrations/0023_inventory.sql`; solo SQL en `internal/server/store`.
- Datos cifrados con `secrets.Box` (`MDM_SECRET_KEY`); en la API, los secretos guardados se devuelven como `secretMask`.
- Toda alta, cambio y borrado escribe en la auditoría con una acción nueva de `internal/server/audit`, añadida también a `Actions` y a `web/src/lib/auditLabels.ts`.
- Roles según la tabla del §9 del diseño.
- Textos de la interfaz y mensajes de error en español, frases completas, como el resto del proyecto.
- Dependencias nuevas solo en la consola: `jspdf`.
- El menú dice **Licencias** (no «Software»): «Software» ya es el catálogo de aplicaciones de la fase 3. Ruta `/licencias`.

## Review Focus

1. Un número de serie basura (`To be filled by O.E.M.`, `0`, vacío) en dos equipos distintos no debe unirlos ni chocar por unicidad → test en Task 2.
2. Un activo manual con el serie de un equipo ya enlazado a otro activo → 409, no un 500 por el índice único → test en Task 3.
3. Restaurar de la papelera un activo cuyo serie ya ha tomado otro → 409 con mensaje → test en Task 3.
4. Graph devuelve error a mitad de la paginación → nadie se marca como borrado → test en Task 5.
5. Confirmar dos veces la misma acta (doble clic) → la segunda da 409 y los activos no se tocan dos veces → test en Task 7.

---

### Task 1: Esquema y almacén de activos y categorías

**Files:**
- Create: `migrations/0023_inventory.sql` (todas las tablas del diseño §3: `asset_categories`, `assets` con `category_auto boolean NOT NULL DEFAULT false`, `deleted_at`, `deleted_by`, `deleted_reason`; `asset_documents`; `directory_users`; `entra_config` (fila única); `licenses`; `license_assignments`; `delivery_records`; `delivery_items`; `delivery_counters (type, year, last)`; `delivery_settings` (fila única, JSON)). Índices únicos parciales: `assets(upper(trim(serial))) WHERE serial_key <> '' AND deleted_at IS NULL` usando columna generada `serial_key` que la aplicación rellena (vacía si el serie no sirve), `assets(lower(asset_tag)) WHERE asset_tag <> '' AND deleted_at IS NULL`.
- Create: `internal/server/store/assets.go`, `internal/server/store/serials.go`, `internal/server/store/assets_test.go`, `internal/server/store/serials_test.go`

**Interfaces — Produces:**
- `func SerialKey(serial string) string` — normalizado `upper(trim)`; `""` si no sirve (§4.3 del diseño).
- `type Asset struct{ ID, CategoryID uuid.UUID; DeviceID, OwnerID *uuid.UUID; CategoryName, Name, Brand, Model, Serial, AssetTag, Status, Department, Location, PurchaseOrder, Supplier, Notes string; Price *float64; PurchaseDate, WarrantyUntil *time.Time; CategoryAuto bool; DeletedAt *time.Time; DeletedBy *uuid.UUID; CreatedAt, UpdatedAt time.Time }`
- `type AssetInput struct{ CategoryID uuid.UUID; Name, Brand, Model, Serial, AssetTag, Status, Department, Location, PurchaseOrder, Supplier, Notes string; OwnerID *uuid.UUID; Price *float64; PurchaseDate, WarrantyUntil *time.Time }`
- `func (s *Store) CreateAsset(ctx, in AssetInput) (Asset, error)`; `UpdateAsset(ctx, id, in AssetInput) (Asset, error)`; `AssetByID(ctx, id) (Asset, error)`; `AssetByDevice(ctx, deviceID) (Asset, error)`.
- `ErrSerialTaken`, `ErrTagTaken` (errores de unicidad traducidos desde el índice).
- `type AssetFilter struct{ Q, Status, Origin /*agent|manual*/, Department string; CategoryID, OwnerID *uuid.UUID; Device DeviceFilter; Trash bool; Sort, Order string; Page, PerPage int; All bool }`; `type AssetRow struct{ Asset; OwnerName, OwnerEmail string; Device *DeviceRow }`; `func (s *Store) ListAssets(ctx, f AssetFilter) ([]AssetRow, int, error)` — `FROM assets a LEFT JOIN devices d ON d.id = a.device_id`, reutiliza `DeviceFilter.where()` con `Q` vacío y añade sus condiciones; si el filtro de dispositivo tiene algo, solo salen activos con agente.
- Categorías: `ListCategories`, `CreateCategory(name)`, `RenameCategory(id, name)`, `DeleteCategory(id)` (`ErrConflict` si está en uso), `CategoryByName(name)`.

- [ ] **Step 1: tests** `TestSerialKey` (tabla: `" abc123 "→"ABC123"`, `""`, `"0"`, `"0000000"`, `"To be filled by O.E.M."`, `"Default string"`, `"VMware-56 4d…"`, `"Parallels-…"` → `""`), `TestAssets_CreateUpdateUnique` (dos activos con serie `ab-1234`/`AB-1234 ` → `ErrSerialTaken`; dos con `"0"` → ambos se crean), `TestAssets_ListFilters` (origen, categoría, estado, persona, Q por nombre/serie/marca/modelo/etiqueta, papelera excluida por defecto), `TestCategories_DeleteInUse`.
- [ ] **Step 2:** ejecutar `go test ./internal/server/store -run 'Serial|Assets|Categories'` → falla.
- [ ] **Step 3:** implementar migración y almacén.
- [ ] **Step 4:** ejecutar de nuevo → pasa.
- [ ] **Step 5:** commit `inventario: esquema y almacén de activos`.

### Task 2: Unión con los equipos del agente

**Files:**
- Modify: `internal/server/store/devices.go` (`EnrollDevice` llama a `ensureDeviceAsset(ctx, tx, id)` antes del commit), `internal/server/store/android_devices.go` (`UpsertAndroidDevice` en transacción con la misma llamada), `internal/server/store/inventory.go` (`SaveInventory`: si el activo tiene `category_auto`, recalcula la categoría con el chasis), `internal/protocol/protocol.go` (`Hardware.Chassis string \`json:"chassis"\``), `internal/agent/inventory/windows.go`, `linux.go` (+ parse y tests), `internal/server/app/janitor.go` (tarea `assets.backfill`, cada hora y al arrancar).
- Create: `internal/server/store/asset_link.go`, `asset_link_test.go`

**Interfaces — Produces:**
- `func ChassisFromSMBIOS(codes []int) string` en `internal/protocol` (`laptop` para 8,9,10,14,30,31,32; `desktop` 3,4,5,6,7,13,15,16,24,35,36; `server` 17,23,25,28,29; si no, `unknown`). Linux lee un código; Windows `ChassisTypes`.
- `func (s *Store) EnsureAllDeviceAssets(ctx) (int, error)`; `func (s *Store) LinkAsset(ctx, assetID, deviceID) error`; `UnlinkAsset(ctx, assetID) error`.
- Categoría inicial (§4.5): Android → «Móvil»; `os_name` contiene «Server» o chasis `server` → «Servidor»; `laptop` → «Portátil»; `desktop` → «Sobremesa»; si no, «Ordenador». `category_auto = true` hasta que un usuario cambie la categoría.
- Dueño → `devices.assigned_user` (correo, si no UPN, si no vacío) dentro de `CreateAsset`/`UpdateAsset` cuando hay `device_id`.

- [ ] **Step 1: tests** `TestEnroll_CreatesAsset`, `TestEnroll_LinksManualAssetBySerial` (conserva precio, persona y estado; rellena marca vacía), `TestEnroll_JunkSerialDoesNotLink` (activo manual con `0` + equipo con `0` → dos activos), `TestEnroll_Reenroll_NoDuplicate`, `TestEnroll_TrashedAssetNotLinked`, `TestAndroidUpsert_CreatesAsset`, `TestInventory_ChassisSetsCategoryOnlyWhenAuto`, `TestDeleteDevice_KeepsAsset`, `TestEnsureAll_Backfill`; agente: `TestChassisFromSMBIOS`, parseo Linux con `chassis_type` = `10`.
- [ ] **Step 2–4:** fallan, implementar, pasan (`go test ./internal/server/store ./internal/protocol ./internal/agent/...`).
- [ ] **Step 5:** commit `inventario: unión de activos con equipos por número de serie`.

### Task 3: API de activos, papelera, documentos e importación CSV

**Files:**
- Create: `internal/server/adminapi/assets.go`, `assets_import.go`, `asset_documents.go`, `categories.go` y sus `_test.go`; `internal/server/store/asset_documents.go`, `asset_trash.go`
- Modify: `routes.go`, `internal/server/audit/audit.go`, `internal/server/store/files.go` y `apps.go` (la consulta de ficheros sin uso excluye `asset_documents`)

**Rutas:**
- `GET /assets` (filtros: `q, category_id, status, origin, owner_id, department, group_id, platform, device_status, compliance, issue, trash, sort, order, page, per_page`), `GET /assets.csv`, `GET /assets/{id}`, `POST /assets` (operador), `PATCH /assets/{id}` (operador), `POST /assets/bulk` (operador; `{ids, status?, category_id?, owner_id?, location?}`), `DELETE /assets/{id}` (admin, papelera, `{reason}`; 409 si tiene equipo enlazado), `POST /assets/{id}/restore` (operador; 409 si el serie está ocupado), `DELETE /assets/{id}/purge` (admin; solo si está en papelera), `POST /assets/{id}/link` `{device_id}` y `POST /assets/{id}/unlink` (admin), `GET /assets/{id}/history` (auditoría con `target_id`).
- `POST /assets/import?dry_run=1` (operador; CSV UTF-8 con BOM opcional, `;` o `,`; columnas de la plantilla del diseño §8) → `{rows:[{line, errors[]}], created}`; sin `dry_run` guarda solo si no hay errores, en una transacción. `GET /assets/import/template.csv`.
- `GET /assets/{id}/documents`, `POST /assets/{id}/documents` (multipart, 20 MB, extensiones y firmas de §3.6), `GET /assets/{id}/documents/{doc}` (attachment), `DELETE /assets/{id}/documents/{doc}`.
- `GET /asset-categories`, `POST|PATCH|DELETE /asset-categories[/{id}]` (admin).

- [ ] **Step 1: tests** de rol (solo lectura no crea; operador no borra), `TestAssets_SerialOfLinkedDevice409`, `TestAssets_TrashRestorePurge` (incluido restore con serie ocupado → 409), `TestAssets_DeleteLinked409`, `TestAssets_Bulk`, `TestAssetsImport_DryRunErrorsByLine` (categoría inexistente, correo desconocido, fecha mala, serie repetido dentro del CSV) y `TestAssetsImport_SavesAll`, `TestDocuments_TypeChecksAndDownloadHeaders` (`.html` rechazado; un `.pdf` sin `%PDF` rechazado; descarga con `Content-Disposition: attachment`), `TestFiles_DocumentsNotPurged`.
- [ ] **Step 2–4:** fallan, implementar, pasan (`go test ./internal/server/adminapi -run 'Assets|Documents|Categories'`).
- [ ] **Step 5:** commit `inventario: API de activos, papelera, documentos e importación`.

### Task 4: Directorio de Entra ID

**Files:**
- Create: `internal/server/directory/graph.go` (cliente: token por credenciales de cliente contra `{AuthBase}/{tenant}/oauth2/v2.0/token`, `GET {GraphBase}/v1.0/users?$select=id,displayName,mail,userPrincipalName,department,jobTitle,employeeId,mobilePhone,officeLocation,accountEnabled&$filter=userType eq 'Member'&$top=999` siguiendo `@odata.nextLink`), `internal/server/directory/sync.go`, `internal/server/directory/fake/fake.go` (servidor de pruebas), tests; `internal/server/store/directory.go`; `internal/server/adminapi/directory.go`, test.
- Modify: `janitor.go` (`directory.sync` cada hora), `app.go` (deps `GraphBase`, `AuthBase` desde config; vacío = Microsoft), `internal/server/app/config.go`.

**Interfaces — Produces:**
- `func (s *Store) ApplyDirectorySync(ctx, users []DirectoryUser, at time.Time) error` — upsert por `entra_id`, marca `deleted_at` a los ausentes, limpia a los que vuelven; una transacción.
- `func (s *Store) ListDirectoryUsers(ctx, f DirectoryFilter) ([]DirectoryUserRow, int, error)` con `AssetCount`; filtro `Status` (`active|disabled|deleted|leavers` = deshabilitados o borrados con activos).
- `type EntraConfig struct{ TenantID, ClientID, SealedSecret string; SecretExpires *time.Time; LastSyncAt *time.Time; LastError string; LastCount int }`; `EntraConfig(ctx)`, `SaveEntraConfig`, `RecordDirectorySync(ok bool, err string, n int, at)`.
- `func (s *Syncer) Run(ctx) error` (no hace nada sin configuración).
- Rutas: `GET /directory/users`, `GET /directory/users/{id}` (con activos, licencias y actas), `PATCH /directory/users/{id}` `{notes}` (operador), `POST /directory/sync` (operador), `GET|PUT /integrations/entra` (admin), `POST /integrations/entra/test` (admin).

- [ ] **Step 1: tests** con el Graph simulado: `TestSync_Paginates`, `TestSync_MarksDeletedAndRestores`, `TestSync_ErrorMidwayChangesNothing`, `TestSync_DisabledKept`, `TestEntraConfig_SecretMasked`, `TestDirectoryUsers_LeaversFilter`.
- [ ] **Step 2–4.** **Step 5:** commit `inventario: usuarios desde Entra ID`.

### Task 5: Licencias

**Files:**
- Create: `internal/server/store/licenses.go`, `internal/server/adminapi/licenses.go`, tests.
- Modify: routes, audit.

**Interfaces — Produces:**
- `type License struct{…§3.4; HasKey bool; Assigned, Installed int; Expired bool}`; `ListLicenses(ctx, f LicenseFilter)`, `LicenseByID`, `CreateLicense`, `UpdateLicense`, `DeleteLicense`, `LicenseKeySealed(id)`, `SetLicenseAssignments`/`AddLicenseAssignment(licenseID, assetID|userID)`/`RemoveLicenseAssignment(id)`, `LicenseInstallations(ctx, id) ([]DeviceRef, error)` (`device_software.name ILIKE '%'||pattern||'%'`, equipos no revocados, distintos).
- Rutas: `GET|POST /licenses`, `GET|PATCH|DELETE /licenses/{id}`, `POST /licenses/{id}/key/reveal` (admin, auditado), `GET /licenses/{id}/installations`, `POST /licenses/{id}/assignments` `{asset_id}|{user_id}`, `DELETE /licenses/{id}/assignments/{aid}`.

- [ ] **Step 1: tests** `TestLicenses_KeySealedAndMasked` (la clave nunca sale en GET; reveal solo admin y en auditoría), `TestLicenses_Counts` (asignadas vs instaladas por patrón), `TestLicenses_ExpiredComputed`, `TestLicenses_AssignmentUniqueness`.
- [ ] **Step 2–4.** **Step 5:** commit `inventario: licencias`.

### Task 6: Alertas e informes nuevos

**Files:** `internal/server/alerts/rules.go`, `engine.go` (rama de reglas globales), `internal/server/store/alerts.go` (consultas), `internal/server/reports/reports.go`, `web/src/lib/alerts.ts`, tests.

- Tipos de estado globales `license.expiring` (`days` 1–180), `directory.sync_failed`, `directory.secret_expiring` (14 días): una alerta sin equipo, abierta mientras se cumple, resuelta cuando deja de cumplirse.
- Informes `assets` («Inventario de activos») y `licenses` («Licencias»).

- [ ] **Step 1: tests** `TestGlobalRule_OpensAndResolves` para cada tipo; `TestReports_Assets`, `TestReports_Licenses`.
- [ ] **Step 2–4.** **Step 5:** commit `inventario: alertas e informes`.

### Task 7: Actas de entrega

**Files:** `internal/server/store/deliveries.go`, `internal/server/adminapi/deliveries.go`, tests.

**Interfaces — Produces:**
- `CreateDelivery(ctx, in DeliveryInput) (Delivery, error)` (borrador, número provisional vacío), `UpdateDelivery` (solo borrador), `DeleteDelivery` (solo borrador), `ConfirmDelivery(ctx, id, actor) (Delivery, error)` — `SELECT … FOR UPDATE`, si no es borrador `ErrConflict`; asigna número `ENT|DEV-AAAA-NNNN` con `delivery_counters` y aplica los efectos del §7.1 a los activos en la misma transacción; `CancelDelivery(ctx, id, reason)`.
- `DeliverySettings` (JSON): `company, nif, address1, address2, phone, web, email, city, logo (data URL ≤ 500 KB), colors{entrega,devolucion}{primary,footer}, clauses{entrega[],devolucion[]}` con los textos por defecto de la app de inventario.
- Rutas: `GET|POST /deliveries`, `GET|PATCH|DELETE /deliveries/{id}`, `POST /deliveries/{id}/confirm` (operador), `POST /deliveries/{id}/cancel` (admin), `GET /delivery-settings` (solo lectura), `PUT /delivery-settings` (admin).

- [ ] **Step 1: tests** `TestDelivery_ConfirmEntregaAssigns`, `TestDelivery_ConfirmDevolucionReturnsToStock`, `TestDelivery_DoubleConfirm409`, `TestDelivery_NumberingPerTypeAndYear`, `TestDelivery_ItemsSnapshot`, `TestDelivery_TrashedAssetRejected`, `TestDeliverySettings_LogoTooLarge`.
- [ ] **Step 2–4.** **Step 5:** commit `inventario: actas de entrega`.

### Task 8: Consola — Inventario, ficha de activo y Papelera

**Files:** `web/src/api/types.ts`; create `web/src/pages/Inventory.tsx`, `AssetDetail.tsx`, `AssetForm.tsx` (componente), `AssetImport.tsx`, `Trash.tsx`, `components/AssetPanel.tsx` (datos + documentos + licencias + actas, usado por la ficha manual y por la pestaña **Activo** de `DeviceDetail.tsx`), tests; modify `App.tsx` (rutas; `/dispositivos` → `/inventario`), `Layout.tsx` (menú: Inventario, Usuarios, Licencias, Doc. Entrega), `DeviceDetail.tsx` (pestaña Activo; el usuario mostrado es el dueño).

- Tests: lista mezclada con icono de origen, filtros en la URL, selección que solo habilita órdenes para filas con agente, formulario con validación, importación con errores por línea, papelera con restaurar.
- Commit `consola: inventario y papelera`.

### Task 9: Consola — Usuarios, Licencias y Ajustes

**Files:** create `Users.tsx`, `UserDetail.tsx`, `Licenses.tsx`, `LicenseDetail.tsx`; modify `Settings.tsx` (secciones Entra ID, Categorías y Actas), tests.
- Commit `consola: usuarios, licencias y ajustes`.

### Task 10: Consola — Doc. Entrega y PDF

**Files:** `web/package.json` (+`jspdf`), create `web/src/lib/deliveryPdf.ts` (port de `generatePDF` de la app de inventario, con `doc.save`), `pages/Deliveries.tsx`, `DeliveryEdit.tsx`, tests (el PDF se prueba comprobando que `generateDeliveryPdf` llama a `save` con `<número>_<tipo>.pdf` y que las cláusulas sustituyen `{empresa}`, `{trabajador}`, `{ciudad}`).
- Commit `consola: actas de entrega`.

### Task 11: Documentación y verificación final

- `docs/inventario.md` (guía de uso, cómo registrar la app en Entra con `User.Read.All` y consentimiento), README, guía de despliegue (salida a Microsoft).
- `go vet ./...`, `go test ./... -p 1`, `npx vitest run`, `npm run build`.
- Commit `docs: inventario`.
