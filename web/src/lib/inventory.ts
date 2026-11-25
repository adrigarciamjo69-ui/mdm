import type { AssetStatus, LicenseStatus, LicenseType } from "../api/types";

export const ASSET_STATUS_LABEL: Record<AssetStatus, string> = {
  en_uso: "En uso",
  en_almacen: "En almacén",
  en_reparacion: "En reparación",
  baja: "Baja",
};

export const ASSET_STATUS_CLASS: Record<AssetStatus, string> = {
  en_uso: "status status-online",
  en_almacen: "status status-inactive",
  en_reparacion: "status status-offline",
  baja: "status status-revoked",
};

export const LICENSE_TYPE_LABEL: Record<LicenseType, string> = {
  perpetua: "Perpetua",
  suscripcion: "Suscripción",
  volumen: "Volumen",
  oem: "OEM",
  freeware: "Gratuita",
  open_source: "Código abierto",
  prueba: "Prueba",
};

export const LICENSE_STATUS_LABEL: Record<LicenseStatus, string> = { activa: "Activa", expirada: "Expirada", baja: "Baja" };

export const PERSON_STATUS_LABEL: Record<string, string> = {
  active: "Activa",
  disabled: "Deshabilitada en Entra",
  deleted: "Eliminada de Entra",
};

export function formatPrice(n: number | null): string {
  if (n === null || n === undefined) return "";
  return n.toLocaleString("es-ES", { style: "currency", currency: "EUR" });
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

export const ISSUE_LABELS: Record<string, string> = {
  low_disk: "Poco espacio en disco",
  unencrypted: "Sin cifrar",
  stale: "Sin conexión reciente",
};

// Igual que store.SerialKey en el servidor: números de serie de relleno que
// no identifican a un equipo.
const JUNK_SERIALS = new Set(["TO BE FILLED BY O.E.M.", "DEFAULT STRING", "SYSTEM SERIAL NUMBER", "CHASSIS SERIAL NUMBER", "NOT SPECIFIED",
  "NONE", "N/A", "0123456789", "NOT APPLICABLE", "INVALID", "UNKNOWN"]);

export function isJunkSerial(serial: string): boolean {
  const k = serial.trim().toUpperCase();
  return k.length < 4 || JUNK_SERIALS.has(k) || k.startsWith("VMWARE-") || k.includes("PARALLELS") || [...k].every((c) => c === k[0]);
}
