import { useEffect, useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import { COMMAND_LABEL, type CommandType } from "../lib/commands";

interface Script {
  id: string;
  name: string;
  platform: string;
  latest_version: number;
  archived: boolean;
  parameters: { name: string; description: string; required: boolean; default: string }[];
}

export interface RunTarget {
  deviceIds?: string[];
  groupId?: string;
  label: string;
  /** Plataforma común de los destinos, si se conoce: filtra los scripts. */
  platform?: string;
}

interface Props {
  type: CommandType;
  target: RunTarget;
  onClose(): void;
  onDone(batchId: string): void;
}

const DELAYS: [number, string][] = [[0, "Ahora"], [60, "1 minuto"], [300, "5 minutos"], [900, "15 minutos"], [1800, "30 minutos"], [3600, "1 hora"]];
const BUTTON: Record<CommandType, string> = { ...COMMAND_LABEL, run_script: "Ejecutar" };

/** Diálogo para lanzar una orden sobre varios equipos o un grupo. */
export default function RunDialog({ type, target, onClose, onDone }: Props) {
  const titleId = useId();
  const [delay, setDelay] = useState(0);
  const [message, setMessage] = useState("");
  const [scriptId, setScriptId] = useState("");
  const [version, setVersion] = useState(0);
  const [params, setParams] = useState<Record<string, string>>({});
  const [typed, setTyped] = useState("");
  const [password, setPassword] = useState("");
  const [lostMessage, setLostMessage] = useState("");
  const [lostPhone, setLostPhone] = useState("");
  const [wipeExternal, setWipeExternal] = useState(false);
  const android = target.platform === "android";
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const scripts = useQuery({ queryKey: ["scripts"], queryFn: () => api<Script[]>("GET", "/scripts"), enabled: type === "run_script" });
  const usable = (scripts.data ?? []).filter((s) => !s.archived && (!target.platform || s.platform === target.platform));
  const script = usable.find((s) => s.id === scriptId);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape" && !busy) onClose(); };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [busy, onClose]);

  const count = target.deviceIds?.length;
  // Varios equipos: se escribe cuántos; uno solo (o un grupo): su nombre.
  const confirmText = count !== undefined && count > 1 ? String(count) : target.label;
  const confirmed = (type !== "retire" && type !== "wipe") || typed.trim() === confirmText;
  const ready = confirmed && (type !== "reset_password" || password.length >= 4) && (type !== "lost_mode_start" || lostMessage.trim() !== "");

  const delayText = DELAYS.find(([s]) => s === delay)?.[1].toLowerCase() ?? "";
  const summary: Record<CommandType, string> = {
    reboot: `Se reiniciará ${delay ? "dentro de " + delayText : "ahora"}${message ? " con aviso" : ""}: ${target.label}.`,
    shutdown: `Se apagará ${delay ? "dentro de " + delayText : "ahora"}: ${target.label}.`,
    refresh_inventory: `Se pedirá un inventario nuevo a ${target.label}.`,
    evaluate_policies: `Se reevaluarán las políticas en ${target.label}.`,
    run_script: script ? `Se ejecutará «${script.name}» como administrador del sistema en ${target.label}.` : `Elige el script que se ejecutará en ${target.label}.`,
    retire: `Se desinstalará el agente y se revocará: ${target.label}. El equipo dejará de estar gestionado.`,
    lock: `Se bloqueará la pantalla ahora: ${target.label}.`,
    reset_password: `Se cambiará el código de desbloqueo y se bloqueará: ${target.label}. Comunica el código nuevo al usuario por otro medio.`,
    lost_mode_start: `La pantalla mostrará tu mensaje y solo permitirá llamar al teléfono indicado: ${target.label}.`,
    lost_mode_stop: `Se saldrá del modo perdido: ${target.label}.`,
    wipe: `Se restablecerá de fábrica y dejará de estar gestionado: ${target.label}. Se perderán todos sus datos.`,
  };

  async function submit() {
    setError("");
    const body: Record<string, unknown> = { type };
    if (target.deviceIds) body.device_ids = target.deviceIds;
    else body.group_id = target.groupId;
    if ((type === "reboot" && !android) || type === "shutdown") body.params = { delay_s: delay, message: message.trim() };
    if (type === "reset_password") body.params = { new_password: password };
    if (type === "lost_mode_start") body.params = { message: lostMessage.trim(), phone: lostPhone.trim() };
    if (type === "wipe") body.params = { wipe_external_storage: wipeExternal };
    if (type === "run_script") {
      if (!script) {
        setError("Elige un script.");
        return;
      }
      for (const p of script.parameters) {
        if (p.required && !(params[p.name] ?? p.default ?? "").trim()) {
          setError(`Falta el parámetro obligatorio ${p.name}.`);
          return;
        }
      }
      const values: Record<string, string> = {};
      for (const p of script.parameters) if (params[p.name] !== undefined) values[p.name] = params[p.name];
      body.params = { script_id: script.id, version, params: values };
    }
    setBusy(true);
    try {
      const r = await api<{ batch: { id: string }; skipped: unknown[] }>("POST", "/batches", body);
      onDone(r.batch.id);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "No se ha podido crear la orden.");
      setBusy(false);
    }
  }

  return (
    <div className="dialog-backdrop">
      <div className="confirm" role="dialog" aria-modal="true" aria-labelledby={titleId} style={{ width: "min(36rem, 100%)" }}>
        <h2 id={titleId}>{COMMAND_LABEL[type]}</h2>
        <p>{summary[type]}</p>
        {type === "reset_password" && (
          <label className="field">
            <span>Código de desbloqueo nuevo (4 a 16 caracteres)</span>
            <input value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="off" maxLength={16} />
          </label>
        )}
        {type === "lost_mode_start" && (
          <>
            <label className="field">
              <span>Mensaje en la pantalla</span>
              <input value={lostMessage} onChange={(e) => setLostMessage(e.target.value)} maxLength={200} placeholder="Este móvil es de Ejemplo SL. Llama para devolverlo." />
            </label>
            <label className="field">
              <span>Teléfono de contacto <span className="hint">opcional</span></span>
              <input value={lostPhone} onChange={(e) => setLostPhone(e.target.value)} maxLength={30} />
            </label>
          </>
        )}
        {type === "wipe" && (
          <label className="check-field">
            <input type="checkbox" checked={wipeExternal} onChange={(e) => setWipeExternal(e.target.checked)} />Borrar también la tarjeta de memoria
          </label>
        )}
        {((type === "reboot" && !android) || type === "shutdown") && (
          <div className="edit-grid">
            <label className="field">
              <span>Retraso</span>
              <select value={delay} onChange={(e) => setDelay(Number(e.target.value))}>
                {DELAYS.map(([s, l]) => <option key={s} value={s}>{l}</option>)}
              </select>
            </label>
            <label className="field">
              <span>Mensaje para el usuario</span>
              <input value={message} onChange={(e) => setMessage(e.target.value)} maxLength={200} />
            </label>
          </div>
        )}
        {type === "run_script" && (
          <>
            <div className="edit-grid">
              <label className="field">
                <span>Script</span>
                <select value={scriptId} onChange={(e) => { setScriptId(e.target.value); setParams({}); setVersion(0); }}>
                  <option value="">Elige…</option>
                  {usable.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
                </select>
              </label>
              {scripts.data && usable.length === 0 && (
                <p className="muted small">No hay scripts{target.platform ? ` para ${target.platform === "windows" ? "Windows" : "Linux"}` : ""}. Créalos en la sección Scripts.</p>
              )}
              {script && (
                <label className="field">
                  <span>Versión</span>
                  <select value={version} onChange={(e) => setVersion(Number(e.target.value))}>
                    <option value={0}>Vigente (v{script.latest_version})</option>
                    {Array.from({ length: script.latest_version }, (_, i) => script.latest_version - i).map((v) => <option key={v} value={v}>v{v}</option>)}
                  </select>
                </label>
              )}
            </div>
            {script?.parameters.map((p) => (
              <label key={p.name} className="field">
                <span>{p.name}{p.required ? "" : <span className="hint"> opcional</span>}{p.description && <span className="hint"> · {p.description}</span>}</span>
                <input aria-label={p.name} value={params[p.name] ?? ""} placeholder={p.default} onChange={(e) => setParams({ ...params, [p.name]: e.target.value })} />
              </label>
            ))}
          </>
        )}
        {(type === "retire" || type === "wipe") && (
          <label className="field">
            <span>Escribe <strong>{confirmText}</strong> para confirmar</span>
            <input value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
          </label>
        )}
        {error && <p className="form-error" role="alert">{error}</p>}
        <div className="dialog-actions">
          <button className="btn" onClick={onClose} disabled={busy}>Cancelar</button>
          <button className={type === "retire" || type === "shutdown" || type === "wipe" ? "btn btn-danger" : "btn btn-primary"} disabled={!ready || busy} onClick={() => void submit()}>
            {BUTTON[type]}
          </button>
        </div>
      </div>
    </div>
  );
}
