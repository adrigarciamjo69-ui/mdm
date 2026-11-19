import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { Group } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import AssignmentPicker, { type Assign } from "../components/AssignmentPicker";
import ConfirmDialog from "../components/ConfirmDialog";
import { formatDateTime } from "../lib/format";
import type { Channel } from "./AlertRules";

interface Trigger { type: "schedule" | "event"; frequency?: string; time?: string; day?: number; event?: string; alert_rule_id?: string }
interface Action { type: string; script_id?: string; version?: number; params?: Record<string, string>; tag?: string; group_id?: string; channel_id?: string; message?: string }
interface Automation { id: string; name: string; enabled: boolean; trigger: Trigger; actions: Action[]; max_devices: number;
  assignments: { group_id: string | null; group_name: string; exclude: boolean }[]; needs_admin: boolean; last_run_at: string | null; next_run_at: string | null }
interface Run { id: number; started_at: string; status: string; device_count: number; batch_ids: string[]; error: string; trigger_info: Record<string, string> }
interface Script { id: string; name: string; archived: boolean }

const ACTIONS: [string, string, boolean][] = [["run_script", "Ejecutar script", true], ["reboot", "Reiniciar", true], ["refresh_inventory", "Actualizar inventario", false],
  ["scan_updates", "Buscar actualizaciones", false], ["evaluate_policies", "Reevaluar políticas", false], ["add_tag", "Añadir etiqueta", false],
  ["remove_tag", "Quitar etiqueta", false], ["set_group", "Cambiar de grupo", true], ["notify", "Avisar", false]];
const LABEL = Object.fromEntries(ACTIONS.map(([k, l]) => [k, l]));
const RUN_STATUS: Record<string, string> = { succeeded: "Correcta", failed: "Fallida", blocked: "Bloqueada", partial: "Parcial" };
const EVENTS: [string, string][] = [["device.enrolled", "Se inscribe un equipo"], ["compliance.changed", "Un equipo deja de cumplir"], ["alert.opened", "Se abre una alerta"]];
const DAYS = ["domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"];

export function triggerText(t: Trigger): string {
  if (t.type === "event") return EVENTS.find(([k]) => k === t.event)?.[1] ?? t.event ?? "";
  if (t.frequency === "hourly") return `Cada hora, en el minuto ${t.time?.slice(3)}`;
  if (t.frequency === "daily") return `Cada día a las ${t.time}`;
  if (t.frequency === "weekly") return `Cada ${DAYS[t.day ?? 0]} a las ${t.time}`;
  return `El día ${t.day} de cada mes a las ${t.time}`;
}

export default function Automations() {
  const list = useQuery({ queryKey: ["automations"], queryFn: () => api<Automation[]>("GET", "/automations") });
  const { can } = useAuth();
  return (
    <>
      <div className="page-head">
        <div><h1>Automatizaciones</h1><p className="muted">Tareas que se repiten solas o que se disparan cuando pasa algo. Simúlalas antes de activarlas.</p></div>
        {can("operator") && <div className="page-actions"><Link className="btn btn-primary" to="/automatizaciones/nueva">Nueva automatización</Link></div>}
      </div>
      {!list.data ? <p className="muted">Cargando…</p> : list.data.length === 0 ? <p className="muted">Ninguna todavía.</p> : (
        <div className="table-wrap">
          <table>
            <thead><tr><th scope="col">Automatización</th><th scope="col">Cuándo</th><th scope="col">Acciones</th><th scope="col">Próxima</th></tr></thead>
            <tbody>
              {list.data.map((a) => (
                <tr key={a.id}>
                  <td><Link className="row-link" to={`/automatizaciones/${a.id}`}>{a.name}</Link>{!a.enabled && <span className="muted small"> · desactivada</span>}</td>
                  <td>{triggerText(a.trigger)}</td>
                  <td className="small">{a.actions.map((x) => LABEL[x.type]).join(", ")}</td>
                  <td className="nowrap small">{a.next_run_at ? formatDateTime(a.next_run_at) : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}

function ActionFields({ a, onChange, scripts, groups, channels }: { a: Action; onChange(a: Action): void; scripts: Script[]; groups: Group[]; channels: Channel[] }) {
  switch (a.type) {
    case "run_script":
      return <label className="field"><span>Script</span><select value={a.script_id ?? ""} onChange={(e) => onChange({ ...a, script_id: e.target.value })}>
        <option value="">Elige…</option>{scripts.filter((s) => !s.archived).map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}</select></label>;
    case "add_tag": case "remove_tag":
      return <label className="field"><span>Etiqueta</span><input value={a.tag ?? ""} onChange={(e) => onChange({ ...a, tag: e.target.value })} /></label>;
    case "set_group":
      return <label className="field"><span>Grupo</span><select value={a.group_id ?? ""} onChange={(e) => onChange({ ...a, group_id: e.target.value })}>
        <option value="">Elige…</option>{groups.filter((g) => g.kind !== "dynamic").map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}</select></label>;
    case "notify":
      return <>
        <label className="field"><span>Canal</span><select value={a.channel_id ?? ""} onChange={(e) => onChange({ ...a, channel_id: e.target.value })}>
          <option value="">Elige…</option>{channels.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}</select></label>
        <label className="field"><span>Mensaje <span className="hint">opcional</span></span><input value={a.message ?? ""} onChange={(e) => onChange({ ...a, message: e.target.value })} /></label>
      </>;
  }
  return null;
}

export function AutomationEdit() {
  const { id = "" } = useParams();
  const isNew = id === "nueva";
  const { can } = useAuth();
  const isAdmin = can("admin");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const existing = useQuery({ queryKey: ["automation", id], queryFn: () => api<Automation>("GET", `/automations/${id}`), enabled: !isNew });
  const runs = useQuery({ queryKey: ["automation-runs", id], queryFn: () => api<Run[]>("GET", `/automations/${id}/runs`), enabled: !isNew });
  const scripts = useQuery({ queryKey: ["scripts"], queryFn: () => api<Script[]>("GET", "/scripts") });
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });
  const channels = useQuery({ queryKey: ["channels"], queryFn: () => api<Channel[]>("GET", "/channels") });
  const rules = useQuery({ queryKey: ["alert-rules"], queryFn: () => api<{ id: string; name: string }[]>("GET", "/alert-rules") });
  const [name, setName] = useState("");
  const [enabled, setEnabled] = useState(false);
  const [trigger, setTrigger] = useState<Trigger>({ type: "schedule", frequency: "daily", time: "02:00", day: 1 });
  const [actions, setActions] = useState<Action[]>([]);
  const [max, setMax] = useState(500);
  const [assign, setAssign] = useState<Assign[]>([]);
  const [error, setError] = useState("");
  const [sim, setSim] = useState<{ device_count: number; devices: { id: string; hostname: string }[]; actions: string[]; blocked: boolean; max_devices: number } | null>(null);
  const [deleting, setDeleting] = useState(false);

  useEffect(() => {
    const a = existing.data;
    if (!a) return;
    setName(a.name); setEnabled(a.enabled); setTrigger(a.trigger); setActions(a.actions); setMax(a.max_devices);
    setAssign(a.assignments.map((x) => ({ group_id: x.group_id, exclude: x.exclude })));
  }, [existing.data]);

  const locked = !isAdmin && (existing.data?.needs_admin ?? false);
  async function save(e: FormEvent) {
    e.preventDefault();
    setError("");
    const body = { name: name.trim(), enabled, trigger, actions, max_devices: max, assignments: assign };
    try {
      if (isNew) {
        const a = await api<Automation>("POST", "/automations", body);
        navigate(`/automatizaciones/${a.id}`, { replace: true });
      } else {
        await api("PATCH", `/automations/${id}`, body);
        void existing.refetch();
      }
      void queryClient.invalidateQueries({ queryKey: ["automations"] });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido guardar.");
    }
  }
  async function simulate() {
    setSim(await api("POST", `/automations/${id}/simulate`));
  }

  return (
    <>
      <form onSubmit={save} noValidate>
        <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/automatizaciones">Automatizaciones</Link></p>
        <div className="page-head">
          <h1>{isNew ? "Nueva automatización" : name}</h1>
          {!isNew && <div className="page-actions">
            <button type="button" className="btn" onClick={() => void simulate()}>Simular</button>
            {!locked && <button type="button" className="btn" onClick={() => setDeleting(true)}>Eliminar</button>}
          </div>}
        </div>
        {locked && <p className="notice">Esta automatización ejecuta scripts, reinicia o cambia de grupo: solo un administrador puede modificarla.</p>}
        {sim && (
          <div className={sim.blocked ? "notice notice-warn" : "notice"} role="status">
            <p><strong>Afectaría a {sim.device_count} equipos</strong>{sim.blocked && ` — supera el máximo de ${sim.max_devices}: no se ejecutaría`}.</p>
            <p className="small">Acciones: {sim.actions.join("; ")}.</p>
            <p className="small">{sim.devices.slice(0, 30).map((d) => d.hostname).join(", ")}{sim.device_count > 30 ? `… y ${sim.device_count - 30} más` : ""}</p>
          </div>
        )}
        <fieldset className="panel" disabled={locked} style={{ border: 0 }}>
          <div className="edit-grid">
            <label className="field"><span>Nombre</span><input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} /></label>
            <label className="field"><span>Cuándo</span>
              <select value={trigger.type} onChange={(e) => setTrigger(e.target.value === "event" ? { type: "event", event: "device.enrolled" } : { type: "schedule", frequency: "daily", time: "02:00", day: 1 })}>
                <option value="schedule">Programada</option><option value="event">Cuando pasa algo</option></select></label>
            {trigger.type === "schedule" ? (
              <>
                <label className="field"><span>Frecuencia</span><select value={trigger.frequency} onChange={(e) => setTrigger({ ...trigger, frequency: e.target.value })}>
                  <option value="hourly">Cada hora</option><option value="daily">Cada día</option><option value="weekly">Cada semana</option><option value="monthly">Cada mes</option></select></label>
                <label className="field"><span>Hora</span><input type="time" value={trigger.time} onChange={(e) => setTrigger({ ...trigger, time: e.target.value })} /></label>
                {trigger.frequency === "weekly" && <label className="field"><span>Día</span><select value={trigger.day} onChange={(e) => setTrigger({ ...trigger, day: Number(e.target.value) })}>
                  {[1, 2, 3, 4, 5, 6, 0].map((d) => <option key={d} value={d}>{DAYS[d]}</option>)}</select></label>}
                {trigger.frequency === "monthly" && <label className="field"><span>Día del mes</span><input type="number" min={1} max={31} value={trigger.day}
                  onChange={(e) => setTrigger({ ...trigger, day: Number(e.target.value) })} /></label>}
              </>
            ) : (
              <>
                <label className="field"><span>Suceso</span><select value={trigger.event} onChange={(e) => setTrigger({ type: "event", event: e.target.value })}>
                  {EVENTS.map(([k, l]) => <option key={k} value={k}>{l}</option>)}</select></label>
                {trigger.event === "alert.opened" && <label className="field"><span>De la regla</span>
                  <select value={trigger.alert_rule_id ?? ""} onChange={(e) => setTrigger({ ...trigger, alert_rule_id: e.target.value || undefined })}>
                    <option value="">Cualquiera</option>{rules.data?.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}</select></label>}
              </>
            )}
            <label className="field"><span>Máximo de equipos por ejecución</span><input type="number" min={1} max={1000} value={max} onChange={(e) => setMax(Number(e.target.value))} /></label>
          </div>
          <fieldset className="params">
            <legend>Acciones (en orden; si una falla, se detiene)</legend>
            <ol className="action-list">
              {actions.map((a, i) => (
                <li key={i}>
                  <strong>{LABEL[a.type]}</strong>
                  <ActionFields a={a} onChange={(n) => setActions(actions.map((x, j) => (j === i ? n : x)))} scripts={scripts.data ?? []} groups={groups.data ?? []} channels={channels.data ?? []} />
                  <button type="button" className="btn btn-quiet" onClick={() => setActions(actions.filter((_, j) => j !== i))}>Quitar</button>
                </li>
              ))}
            </ol>
            <label className="field" style={{ maxWidth: "20rem" }}><span>Añadir acción</span>
              <select aria-label="Añadir acción" value="" onChange={(e) => e.target.value && setActions([...actions, { type: e.target.value }])}>
                <option value="">Elige…</option>
                {ACTIONS.map(([k, l, admin]) => <option key={k} value={k} disabled={admin && !isAdmin}>{l}{admin && !isAdmin ? " (solo administradores)" : ""}</option>)}
              </select></label>
          </fieldset>
          <fieldset className="params"><legend>Equipos</legend><AssignmentPicker value={assign} onChange={setAssign} readOnly={locked} /></fieldset>
          <label className="check-field"><input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />Activa</label>
        </fieldset>
        {error && <p className="form-error" role="alert">{error}</p>}
        {!locked && <button className="btn btn-primary" style={{ marginTop: "1rem" }} disabled={!name.trim() || actions.length === 0}>Guardar automatización</button>}
      </form>
      {!isNew && (
        <section className="section">
          <h2>Historial</h2>
          {!runs.data || runs.data.length === 0 ? <p className="muted">Aún no se ha ejecutado.</p> : (
            <div className="table-wrap">
              <table>
                <thead><tr><th scope="col">Fecha</th><th scope="col">Resultado</th><th scope="col" className="num">Equipos</th><th scope="col">Detalle</th></tr></thead>
                <tbody>
                  {runs.data.map((r) => (
                    <tr key={r.id}>
                      <td className="nowrap">{formatDateTime(r.started_at)}</td>
                      <td>{RUN_STATUS[r.status] ?? r.status}</td>
                      <td className="num">{r.device_count}</td>
                      <td className="small">{r.error}{r.batch_ids.map((b) => <span key={b}> <Link to={`/actividad/${b}`}>ver lote</Link></span>)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}
      {deleting && (
        <ConfirmDialog title={`Eliminar «${name}»`} body="Se borra también su historial." confirmLabel="Eliminar" danger
          onConfirm={async () => { await api("DELETE", `/automations/${id}`); navigate("/automatizaciones", { replace: true }); }} onCancel={() => setDeleting(false)} />
      )}
    </>
  );
}
