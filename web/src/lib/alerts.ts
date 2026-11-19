// Textos de las alertas.
export const SEVERITY_LABEL: Record<string, string> = { critical: "Crítica", warning: "Aviso", info: "Información" };
export const SEVERITY_CLASS: Record<string, string> = { critical: "status status-revoked", warning: "status status-offline", info: "status status-inactive" };
export const STATE_LABEL: Record<string, string> = { open: "Abierta", acknowledged: "Reconocida", resolved: "Resuelta" };

export interface RuleType { type: string; label: string; kind: "state" | "event"; param?: { name: string; label: string; min: number; max: number; def: number } }

export const RULE_TYPES: RuleType[] = [
  { type: "device.offline", label: "Equipo sin conexión", kind: "state", param: { name: "hours", label: "Horas sin conectar", min: 1, max: 720, def: 1 } },
  { type: "disk.low", label: "Poco espacio en disco", kind: "state", param: { name: "percent", label: "Porcentaje libre por debajo de", min: 1, max: 50, def: 10 } },
  { type: "compliance.noncompliant", label: "Equipo que no cumple", kind: "state" },
  { type: "patches.security_pending", label: "Parches de seguridad pendientes", kind: "state", param: { name: "days", label: "Días pendientes", min: 1, max: 90, def: 14 } },
  { type: "bitlocker.key_missing", label: "Clave de BitLocker sin custodiar", kind: "state" },
  { type: "android.integrity", label: "Móvil con integridad dudosa", kind: "state" },
  { type: "deployment.failed", label: "Despliegue fallido", kind: "event" },
  { type: "command.failed", label: "Orden fallida", kind: "event" },
  { type: "device.enrolled", label: "Equipo inscrito", kind: "event" },
  { type: "console.login_failures", label: "Inicios de sesión fallidos en la consola", kind: "event", param: { name: "count", label: "Intentos en 15 minutos", min: 3, max: 100, def: 5 } },
];

export interface AlertRow {
  id: number; rule_id: string | null; rule_name: string; device_id: string | null; hostname: string; state: string; severity: string;
  title: string; details: Record<string, unknown>; opened_at: string; acknowledged_at: string | null; resolved_at: string | null; resolved_by_hand: boolean;
}
