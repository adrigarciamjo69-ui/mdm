import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import AssignmentPicker, { type Assign } from "../components/AssignmentPicker";
import ConfirmDialog from "../components/ConfirmDialog";
import { RULE_TYPES, SEVERITY_CLASS, SEVERITY_LABEL } from "../lib/alerts";

export interface Channel { id: string; name: string; type: "email" | "webhook"; recipients: string[]; url: string; last_error: string; secret?: string }

interface Rule {
  id: string; name: string; type: string; kind: string; params: Record<string, number | string>; severity: string; channels: string[];
  emails: string[]; notify_resolved: boolean; enabled: boolean; assignments: { group_id: string | null; group_name: string; exclude: boolean }[];
}

export function AlertRuleList() {
  const rules = useQuery({ queryKey: ["alert-rules"], queryFn: () => api<Rule[]>("GET", "/alert-rules") });
  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/alertas">Alertas</Link></p>
      <div className="page-head">
        <div><h1>Reglas de alerta</h1><p className="muted">Qué situaciones abren una alerta y a quién se avisa.</p></div>
        <div className="page-actions"><Link className="btn btn-primary" to="/alertas/reglas/nueva">Nueva regla</Link></div>
      </div>
      {!rules.data ? <p className="muted">Cargando…</p> : rules.data.length === 0 ? <p className="muted">Ninguna regla todavía.</p> : (
        <div className="table-wrap">
          <table>
            <thead><tr><th scope="col">Regla</th><th scope="col">Tipo</th><th scope="col">Gravedad</th><th scope="col">Estado</th></tr></thead>
            <tbody>
              {rules.data.map((r) => (
                <tr key={r.id}>
                  <td><Link className="row-link" to={`/alertas/reglas/${r.id}`}>{r.name}</Link></td>
                  <td>{RULE_TYPES.find((t) => t.type === r.type)?.label ?? r.type}</td>
                  <td><span className={SEVERITY_CLASS[r.severity]}>{SEVERITY_LABEL[r.severity]}</span></td>
                  <td>{r.enabled ? "Activa" : <span className="muted">Desactivada</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}

export function AlertRuleEdit() {
  const { id = "" } = useParams();
  const isNew = id === "nueva";
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const channels = useQuery({ queryKey: ["channels"], queryFn: () => api<Channel[]>("GET", "/channels") });
  const existing = useQuery({ queryKey: ["alert-rules"], queryFn: () => api<Rule[]>("GET", "/alert-rules"), enabled: !isNew });
  const [name, setName] = useState("");
  const [type, setType] = useState(RULE_TYPES[0].type);
  const [param, setParam] = useState<number>(RULE_TYPES[0].param!.def);
  const [severity, setSeverity] = useState("warning");
  const [chans, setChans] = useState<string[]>([]);
  const [emails, setEmails] = useState("");
  const [notifyResolved, setNotifyResolved] = useState(true);
  const [enabled, setEnabled] = useState(true);
  const [assign, setAssign] = useState<Assign[]>([{ group_id: null, exclude: false }]);
  const [error, setError] = useState("");
  const [deleting, setDeleting] = useState(false);
  const def = RULE_TYPES.find((t) => t.type === type)!;

  useEffect(() => {
    const r = existing.data?.find((x) => x.id === id);
    if (!r) return;
    setName(r.name); setType(r.type); setSeverity(r.severity); setChans(r.channels); setEmails(r.emails.join(", "));
    setNotifyResolved(r.notify_resolved); setEnabled(r.enabled); setAssign(r.assignments.map((a) => ({ group_id: a.group_id, exclude: a.exclude })));
    const p = RULE_TYPES.find((t) => t.type === r.type)?.param;
    if (p) setParam(Number(r.params[p.name]));
  }, [existing.data, id]);

  async function save(e: FormEvent) {
    e.preventDefault();
    setError("");
    const body = { name: name.trim(), type, params: def.param ? { [def.param.name]: param } : {}, severity, channels: chans,
      emails: emails.split(/[\s,;]+/).filter(Boolean), notify_resolved: notifyResolved, enabled, assignments: assign };
    try {
      if (isNew) await api("POST", "/alert-rules", body);
      else await api("PATCH", `/alert-rules/${id}`, body);
      void queryClient.invalidateQueries({ queryKey: ["alert-rules"] });
      navigate("/alertas/reglas");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido guardar la regla.");
    }
  }

  return (
    <form onSubmit={save} noValidate>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/alertas/reglas">Reglas de alerta</Link></p>
      <div className="page-head">
        <h1>{isNew ? "Nueva regla de alerta" : name}</h1>
        {!isNew && <div className="page-actions"><button type="button" className="btn" onClick={() => setDeleting(true)}>Eliminar</button></div>}
      </div>
      <div className="panel">
        <div className="edit-grid">
          <label className="field"><span>Nombre</span><input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} /></label>
          <label className="field">
            <span>Tipo</span>
            <select value={type} disabled={!isNew} onChange={(e) => { setType(e.target.value); const p = RULE_TYPES.find((t) => t.type === e.target.value)?.param; if (p) setParam(p.def); }}>
              <optgroup label="Situaciones (se resuelven solas)">{RULE_TYPES.filter((t) => t.kind === "state").map((t) => <option key={t.type} value={t.type}>{t.label}</option>)}</optgroup>
              <optgroup label="Sucesos (se cierran a mano o a los 7 días)">{RULE_TYPES.filter((t) => t.kind === "event").map((t) => <option key={t.type} value={t.type}>{t.label}</option>)}</optgroup>
            </select>
          </label>
          {def.param && (
            <label className="field"><span>{def.param.label}</span>
              <input type="number" min={def.param.min} max={def.param.max} value={param} onChange={(e) => setParam(Number(e.target.value))} /></label>
          )}
          <label className="field">
            <span>Gravedad</span>
            <select value={severity} onChange={(e) => setSeverity(e.target.value)}>
              <option value="critical">Crítica</option><option value="warning">Aviso</option><option value="info">Información</option>
            </select>
          </label>
        </div>
        <fieldset className="params">
          <legend>Avisar por</legend>
          {channels.data?.length === 0 && <p className="muted small">No hay canales: créalos en <Link to="/integraciones/avisos">Avisos</Link>.</p>}
          <div className="check-row">
            {channels.data?.map((c) => (
              <label key={c.id} className="check-field">
                <input type="checkbox" checked={chans.includes(c.id)} onChange={(e) => setChans(e.target.checked ? [...chans, c.id] : chans.filter((x) => x !== c.id))} />{c.name}
              </label>
            ))}
          </div>
          <label className="field"><span>Más direcciones de correo <span className="hint">opcional, separadas por comas</span></span>
            <input value={emails} onChange={(e) => setEmails(e.target.value)} /></label>
          {def.kind === "state" && <label className="check-field"><input type="checkbox" checked={notifyResolved} onChange={(e) => setNotifyResolved(e.target.checked)} />Avisar también cuando se resuelva</label>}
        </fieldset>
        {type !== "console.login_failures" && (
          <fieldset className="params"><legend>Equipos</legend><AssignmentPicker value={assign} onChange={setAssign} /></fieldset>
        )}
        <label className="check-field"><input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />Activa</label>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      <button className="btn btn-primary" style={{ marginTop: "1rem" }} disabled={!name.trim()}>Guardar regla</button>
      {deleting && (
        <ConfirmDialog title={`Eliminar «${name}»`} body="Sus alertas abiertas se resolverán. Las ya cerradas se conservan en el historial." confirmLabel="Eliminar" danger
          onConfirm={async () => { await api("DELETE", `/alert-rules/${id}`); navigate("/alertas/reglas", { replace: true }); }} onCancel={() => setDeleting(false)} />
      )}
    </form>
  );
}
