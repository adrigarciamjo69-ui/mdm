// Textos de los despliegues y de sus estados.
export const APP_STATE: Record<string, { label: string; cls: string }> = {
  pending: { label: "Pendiente", cls: "status status-inactive" },
  waiting_window: { label: "Esperando la ventana", cls: "status status-inactive" },
  downloading: { label: "Descargando", cls: "status status-offline" },
  installing: { label: "Instalando", cls: "status status-offline" },
  installed: { label: "Instalado", cls: "status status-online" },
  absent: { label: "Desinstalado", cls: "status status-online" },
  failed: { label: "Fallido", cls: "status status-revoked" },
  not_applicable: { label: "No aplicable", cls: "status status-inactive" },
};

export const INTENT: Record<string, string> = { required: "Instalar", uninstall: "Desinstalar", available: "Disponible" };

export interface DeploymentRow {
  id: string;
  app_id: string;
  app_name: string;
  app_source: string;
  platform: string;
  intent: "required" | "uninstall" | "available";
  keep_updated: boolean;
  start_at: string | null;
  respect_window: boolean;
  reboot: "never" | "notify" | "in_window";
  assignments: { group_id: string | null; group_name: string; exclude: boolean }[];
  counts: Record<string, number>;
}

export interface DeviceDeploymentRow {
  device_id: string;
  hostname: string;
  deployment_id: string;
  app_name: string;
  intent: string;
  state: string;
  message: string;
  attempts: number;
  exit_code: number | null;
}

export function AppStateBadge({ state }: { state: string }) {
  const s = APP_STATE[state] ?? { label: state, cls: "status" };
  return <span className={s.cls}>{s.label}</span>;
}
