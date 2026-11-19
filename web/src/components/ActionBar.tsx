import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { COMMAND_LABEL, type CommandType } from "../lib/commands";
import RunDialog, { type RunTarget } from "./RunDialog";

const OPERATOR: CommandType[] = ["reboot", "shutdown", "refresh_inventory", "evaluate_policies"];
const ADMIN: CommandType[] = ["run_script", "retire"];
const ANDROID_OPERATOR: CommandType[] = ["lock", "reboot", "lost_mode_start", "lost_mode_stop"];
const ANDROID_ADMIN: CommandType[] = ["reset_password", "wipe"];
const DANGER: CommandType[] = ["retire", "wipe"];

/** Botones de acción sobre un destino (selección, equipo o grupo). */
export default function ActionBar({ target, label, children }: { target: RunTarget; label: string; children?: React.ReactNode }) {
  const { can } = useAuth();
  const navigate = useNavigate();
  const [type, setType] = useState<CommandType | null>(null);
  if (!can("operator")) return null;
  const android = target.platform === "android";
  const types = android ? [...ANDROID_OPERATOR, ...(can("admin") ? ANDROID_ADMIN : [])] : [...OPERATOR, ...(can("admin") ? ADMIN : [])];
  return (
    <div className="action-bar" role="toolbar" aria-label="Acciones">
      {children ?? <span className="small">{label}</span>}
      {types.map((t) => (
        <button key={t} className={DANGER.includes(t) ? "btn btn-small btn-danger-outline" : "btn btn-small"} onClick={() => setType(t)}>
          {COMMAND_LABEL[t]}
        </button>
      ))}
      {type && <RunDialog type={type} target={target} onClose={() => setType(null)} onDone={(id) => navigate(`/actividad/${id}`)} />}
    </div>
  );
}
