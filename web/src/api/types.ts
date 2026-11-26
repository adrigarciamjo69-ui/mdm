// Tipos de la API de consola (/api/v1). Los nombres de campo coinciden con el JSON del servidor.

export type Role = "readonly" | "operator" | "admin";
export type Status = "online" | "offline" | "inactive" | "revoked";
export type Platform = "windows" | "linux" | "android";
export type SecurityState = "on" | "off" | "partial" | "unknown";

export interface Me {
  id: string;
  email: string;
  role: Role;
  csrf_token: string;
}

export interface Page<T> {
  items: T[];
  total: number;
  page: number;
  per_page: number;
}

export interface Device {
  id: string;
  hostname: string;
  platform: Platform;
  os_name: string;
  os_version: string;
  arch: string;
  serial: string;
  manufacturer: string;
  model: string;
  agent_version: string;
  status: Status;
  compliance?: "compliant" | "noncompliant" | "unknown";
  group_id: string | null;
  group_name: string;
  assigned_user: string;
  tags: string[];
  enrolled_at: string;
  last_seen_at: string | null;
  last_ip: string;
  inventory_at: string | null;
}

export interface Disk {
  device: string;
  mount: string;
  filesystem: string;
  size_bytes: number;
  free_bytes: number;
  encrypted: boolean;
}

export interface NetIf {
  name: string;
  mac: string;
  ipv4: string[] | null;
  ipv6: string[] | null;
}

export type Section = "system" | "hardware" | "disks" | "network" | "security" | "software";

export interface Inventory {
  unavailable: Section[];
  system: {
    hostname: string;
    os_name: string;
    os_version: string;
    kernel: string;
    arch: string;
    boot_time: string;
    timezone: string;
    domain: string;
    logged_in_user: string;
  };
  hardware: {
    manufacturer: string;
    model: string;
    serial: string;
    bios_vendor: string;
    bios_version: string;
    bios_date: string;
    cpu_model: string;
    cpu_cores: number;
    cpu_threads: number;
    memory_bytes: number;
  };
  disks: Disk[];
  network: NetIf[];
  security: {
    disk_encryption: SecurityState;
    firewall: SecurityState;
    antivirus: SecurityState;
    antivirus_product: string;
    secure_boot: SecurityState;
  };
}

export interface DeviceDetail extends Device {
  inventory: (Inventory & { mobile?: Mobile }) | null;
  inventory_collected_at: string | null;
}

export interface Mobile {
  state: string;
  imei: string;
  meid: string;
  phone_number: string;
  carrier: string;
  security_patch_level: string;
  build_number: string;
  integrity: string;
  device_secure: boolean;
  developer_options: boolean;
  unknown_sources: boolean;
  storage_bytes: number;
  policy_applied: boolean;
  last_status_report: string;
}

export interface Software {
  name: string;
  version: string;
  publisher: string;
  installed_at: string | null;
}

export interface ActivityPoint {
  hour: string;
  checkins: number;
}

export interface Group {
  id: string;
  name: string;
  description: string;
  kind?: "static" | "dynamic";
  rule?: unknown;
  device_count: number;
}

export type TokenState = "active" | "expired" | "exhausted" | "revoked";

export interface EnrollmentToken {
  id: string;
  name: string;
  state: TokenState;
  group_id: string | null;
  group_name: string;
  expires_at: string | null;
  max_uses: number | null;
  uses: number;
  created_at: string;
}

export interface CreatedToken extends EnrollmentToken {
  token: string;
  install: { linux: string; windows: string };
}

export interface Admin {
  id: string;
  email: string;
  role: Role;
  disabled: boolean;
  has_totp: boolean;
  created_at: string;
  last_login_at: string | null;
}

export interface AuditEntry {
  id: number;
  at: string;
  actor_email: string;
  action: string;
  target_type: string;
  target_id: string;
  details: Record<string, unknown>;
  ip: string;
}

export interface OSCount {
  platform: Platform;
  os_name: string;
  os_version: string;
  count: number;
}

export interface Summary {
  total: number;
  by_status: Record<Status, number>;
  by_platform: Partial<Record<Platform, number>>;
  os_versions: OSCount[];
  low_disk: number;
  unencrypted: number;
  stale: number;
  recent_enrollments: Device[];
  compliance?: Record<"compliant" | "noncompliant" | "unknown", number>;
  security_updates_pending?: number;
  deployment_failures?: number;
}

// ---------- Fase 2 ----------

export type ComplianceState = "compliant" | "noncompliant" | "unknown";
export type SettingStatus = "compliant" | "noncompliant" | "error" | "not_applicable";

export interface CatalogField {
  name: string;
  label: string;
  type: "int" | "bool" | "string" | "text" | "enum" | "string_list" | "time" | "package" | "secret";
  required: boolean;
  default?: unknown;
  min?: number;
  max?: number;
  options?: string[];
  /** Texto de cada opción de un enum. */
  option_labels?: Record<string, string>;
  max_len?: number;
  description?: string;
}

export interface CatalogDef {
  key: string;
  platform: Platform;
  title: string;
  description: string;
  audit_only: boolean;
  enforce_only?: boolean;
  fields: CatalogField[];
}

export interface PolicySetting {
  key: string;
  mode: "audit" | "enforce";
  params: Record<string, unknown>;
}

export interface PolicyAssignment {
  group_id: string | null;
  group_name: string;
  exclude: boolean;
}

export interface Policy {
  id: string;
  name: string;
  description: string;
  platform: Platform;
  priority: number;
  settings: PolicySetting[];
  assignments: PolicyAssignment[];
  created_at: string;
  updated_at: string;
}

export interface SettingResultRow {
  policy_id: string;
  policy_name: string;
  key: string;
  title: string;
  status: SettingStatus;
  message: string;
  applied: boolean;
  evaluated_at: string;
}

export interface ComplianceDetail {
  state: ComplianceState;
  noncompliant_since: string | null;
  evaluated_at: string | null;
  reported_at: string | null;
  up_to_date: boolean;
  server_reasons: string[];
  results: SettingResultRow[];
  conflicts: { key: string; title: string; winner_id: string; winner_name: string; loser_id: string; loser_name: string }[];
}

export interface RecoveryKeyMeta {
  key_id: string;
  volume: string;
  reported_at: string;
}

// ---------- Fase 3 ----------

export interface Detection {
  type: "inventory" | "file" | "registry" | "script";
  name_contains?: string;
  min_version?: string;
  path?: string;
  registry_key?: string;
  registry_value?: string;
  interpreter?: string;
  script?: string;
}

export interface AppVersion {
  id: string;
  version: string;
  file_sha256: string;
  file_size: number;
  file_name: string;
  installer_type: "msi" | "exe" | "deb" | "rpm";
  install_command: string;
  uninstall_command: string;
  success_codes: number[];
  detection: Detection;
  is_current: boolean;
  created_at: string;
}

export interface App {
  id: string;
  name: string;
  publisher: string;
  description: string;
  platform: Platform;
  source: "winget" | "apt" | "dnf" | "upload" | "google_play";
  package_id: string;
  archived: boolean;
  current_version: AppVersion | null;
  versions: AppVersion[];
  updated_at: string;
}

// --- Inventario ---

export type AssetStatus = "en_uso" | "en_almacen" | "en_reparacion" | "baja";

export interface PersonRef {
  id: string;
  name: string;
  email: string;
  /** Departamento en Entra ID: si lo tiene, es el del activo. */
  department?: string;
}

export interface Asset {
  id: string;
  device_id: string | null;
  origin: "agent" | "manual";
  category_id: string;
  category: string;
  category_auto: boolean;
  name: string;
  brand: string;
  model: string;
  serial: string;
  asset_tag: string;
  status: AssetStatus;
  owner: PersonRef | null;
  department: string;
  location: string;
  price: number | null;
  purchase_date: string;
  warranty_until: string;
  purchase_order: string;
  supplier: string;
  notes: string;
  deleted_at: string | null;
  deleted_reason: string;
  created_at: string;
  updated_at: string;
  device: Device | null;
  /** La persona no sigue al usuario con sesión en el equipo. */
  owner_locked: boolean;
  /** El departamento no sigue al de la persona ni a la sesión. */
  department_locked: boolean;
}

export interface Category {
  id: string;
  name: string;
  assets: number;
}

export interface AssetDocument {
  id: string;
  filename: string;
  content_type: string;
  size: number;
  uploaded_by: string;
  uploaded_at: string;
}

export interface DirectoryUser {
  id: string;
  entra_id: string;
  display_name: string;
  email: string;
  upn: string;
  department: string;
  job_title: string;
  employee_id: string;
  phone: string;
  office: string;
  enabled: boolean;
  deleted_at: string | null;
  synced_at: string;
  notes: string;
  status: "active" | "disabled" | "deleted";
  asset_count: number;
}

export interface DirectoryStatus {
  configured: boolean;
  last_sync_at: string | null;
  last_success_at: string | null;
  last_error: string;
  last_count: number;
  secret_expires: string;
}

export type LicenseType = "perpetua" | "suscripcion" | "volumen" | "oem" | "freeware" | "open_source" | "prueba";
export type LicenseStatus = "activa" | "expirada" | "baja";

export interface LicenseAssignment {
  id: string;
  asset: { id: string; name: string; serial: string } | null;
  user: PersonRef | null;
  notes: string;
  assigned_at: string;
}

export interface License {
  id: string;
  name: string;
  vendor: string;
  version: string;
  license_type: LicenseType;
  seats: number;
  has_key: boolean;
  purchase_date: string;
  expiry_date: string;
  purchase_order: string;
  price: number | null;
  department: string;
  status: LicenseStatus;
  effective_status: LicenseStatus;
  match_pattern: string;
  notes: string;
  assigned: number;
  installed: number;
  over: boolean;
  created_at: string;
  updated_at: string;
  assignments?: LicenseAssignment[];
}

export interface DeliveryItem {
  id?: string;
  asset_id: string | null;
  category: string;
  brand: string;
  model: string;
  serial: string;
  observations: string;
}

export interface Delivery {
  id: string;
  number: string;
  type: "entrega" | "devolucion";
  status: "borrador" | "confirmada" | "anulada";
  user: { id: string; name: string; email: string; department: string; job_title: string; employee_id: string };
  recipient_dni: string;
  date: string;
  responsible: string;
  notes: string;
  cancel_reason: string;
  created_by: string;
  created_at: string;
  confirmed_at: string | null;
  items: DeliveryItem[];
}

export interface DeliverySettings {
  company: string;
  nif: string;
  address1: string;
  address2: string;
  phone: string;
  web: string;
  email: string;
  city: string;
  logo: string;
  colors: { entrega: { primary: string; footer: string }; devolucion: { primary: string; footer: string } };
  clauses: { entrega: string[]; devolucion: string[] };
}
