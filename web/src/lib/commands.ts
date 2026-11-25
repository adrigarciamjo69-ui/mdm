// Textos de las órdenes y sus estados.
export type CommandType = "reboot" | "shutdown" | "refresh_inventory" | "evaluate_policies" | "run_script" | "retire"
  | "lock" | "reset_password" | "lost_mode_start" | "lost_mode_stop" | "wipe" | "temp_admin" | "temp_admin_revoke";

export const COMMAND_LABEL: Record<CommandType, string> = {
  reboot: "Reiniciar", shutdown: "Apagar", refresh_inventory: "Actualizar inventario",
  evaluate_policies: "Reevaluar políticas", run_script: "Ejecutar script", retire: "Retirar",
  lock: "Bloquear", reset_password: "Cambiar contraseña", lost_mode_start: "Modo perdido", lost_mode_stop: "Salir del modo perdido",
  wipe: "Borrar el móvil", temp_admin: "Administrador temporal", temp_admin_revoke: "Quitar administrador temporal",
};

export const COMMAND_STATUS: Record<string, { label: string; cls: string }> = {
  pending: { label: "Pendiente", cls: "status status-inactive" },
  sent: { label: "Enviada", cls: "status status-inactive" },
  running: { label: "En curso", cls: "status status-offline" },
  succeeded: { label: "Correcta", cls: "status status-online" },
  failed: { label: "Fallida", cls: "status status-revoked" },
  expired: { label: "Caducada", cls: "status status-inactive" },
  cancelled: { label: "Cancelada", cls: "status status-inactive" },
};

export const FINISHED = ["succeeded", "failed", "expired", "cancelled"];

export function commandTitle(type: string, params?: Record<string, unknown>): string {
  const base = COMMAND_LABEL[type as CommandType] ?? type;
  if (type === "run_script" && params?.script_name) return `${base}: ${params.script_name} (v${params.version})`;
  if (type === "temp_admin") return `${base}: ${params?.user || "usuario con sesión"} (${formatMinutes(Number(params?.minutes ?? 0))})`;
  if (type === "temp_admin_revoke" && params?.user) return `${base}: ${params.user}`;
  return base;
}

export interface CommandRow {
  id: string;
  device_id: string;
  hostname: string;
  type: string;
  params?: Record<string, unknown>;
  status: string;
  exit_code: number | null;
  error: string;
  created_at: string;
  finished_at?: string | null;
  stdout?: string;
  stderr?: string;
}

export interface BatchRow {
  id: string;
  type: string;
  params: Record<string, unknown>;
  target_description: string;
  device_count: number;
  created_by_email: string;
  created_at: string;
  counts: Record<string, number>;
  commands?: CommandRow[];
}

/** 90 → «1 h 30 min». */
export function formatMinutes(m: number): string {
  const h = Math.floor(m / 60), r = m % 60;
  if (h === 0) return `${r} min`;
  return r ? `${h} h ${r} min` : `${h} h`;
}
