import { Link, useParams, useSearchParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Page } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import EmptyState from "../components/EmptyState";
import { formatDateTime, relativeTime } from "../lib/format";
import { SEVERITY_CLASS, SEVERITY_LABEL, STATE_LABEL, type AlertRow } from "../lib/alerts";

function useAlertActions() {
  const queryClient = useQueryClient();
  return async (id: number, action: "acknowledge" | "resolve") => {
    await api("POST", `/alerts/${id}/${action}`);
    void queryClient.invalidateQueries({ queryKey: ["alerts"] });
    void queryClient.invalidateQueries({ queryKey: ["alert-counts"] });
  };
}

export function AlertDetail() {
  const { id = "" } = useParams();
  const { can } = useAuth();
  const act = useAlertActions();
  const a = useQuery({ queryKey: ["alerts", "one", id], queryFn: () => api<AlertRow>("GET", `/alerts/${id}`) });
  if (a.error) return <div className="notice"><p>Esta alerta no existe.</p><Link to="/alertas">Volver a alertas</Link></div>;
  if (!a.data) return <p className="muted">Cargando…</p>;
  const x = a.data;
  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/alertas">Alertas</Link></p>
      <div className="page-head"><div><h1>{x.title}</h1>
        <p className="muted"><span className={SEVERITY_CLASS[x.severity]}>{SEVERITY_LABEL[x.severity]}</span> · {STATE_LABEL[x.state]} · regla «{x.rule_name}»</p></div></div>
      <div className="panel">
        <dl className="facts">
          <dt>Abierta</dt><dd>{formatDateTime(x.opened_at)}</dd>
          {x.acknowledged_at && <><dt>Reconocida</dt><dd>{formatDateTime(x.acknowledged_at)}</dd></>}
          {x.resolved_at && <><dt>Resuelta</dt><dd>{formatDateTime(x.resolved_at)} {x.resolved_by_hand ? "(a mano)" : "(sola, al dejar de cumplirse)"}</dd></>}
          {x.device_id && <><dt>Equipo</dt><dd><Link to={`/dispositivos/${x.device_id}`}>{x.hostname}</Link></dd></>}
        </dl>
        {can("operator") && x.state !== "resolved" && (
          <div style={{ display: "flex", gap: "0.5rem", marginTop: "1rem" }}>
            {x.state === "open" && <button className="btn" onClick={() => void act(x.id, "acknowledge").then(() => a.refetch())}>Reconocer</button>}
            <button className="btn" onClick={() => void act(x.id, "resolve").then(() => a.refetch())}>Resolver</button>
          </div>
        )}
      </div>
    </>
  );
}

export default function Alerts() {
  const [params, setParams] = useSearchParams();
  const { can } = useAuth();
  const act = useAlertActions();
  const state = params.get("estado") ?? "unresolved";
  const severity = params.get("gravedad") ?? "";
  const rule = params.get("regla") ?? "";
  const qs = new URLSearchParams({ state: state === "todas" ? "" : state, severity, rule, per_page: "100" }).toString();
  const list = useQuery({ queryKey: ["alerts", qs], queryFn: () => api<Page<AlertRow>>("GET", `/alerts?${qs}`), refetchInterval: 30_000 });
  const set = (k: string, v: string) => { const n = new URLSearchParams(params); if (v) n.set(k, v); else n.delete(k); setParams(n); };
  return (
    <>
      <div className="page-head">
        <div><h1>Alertas</h1><p className="muted">Lo que requiere atención. Las de estado se resuelven solas cuando la situación se corrige.</p></div>
        {can("admin") && <div className="page-actions"><Link className="btn" to="/alertas/reglas">Reglas de alerta</Link></div>}
      </div>
      <div className="filters filters-inline">
        <select aria-label="Estado" value={state} onChange={(e) => set("estado", e.target.value === "unresolved" ? "" : e.target.value)}>
          <option value="unresolved">Sin resolver</option><option value="open">Abiertas</option><option value="acknowledged">Reconocidas</option>
          <option value="resolved">Resueltas</option><option value="todas">Todas</option>
        </select>
        <select aria-label="Gravedad" value={severity} onChange={(e) => set("gravedad", e.target.value)}>
          <option value="">Todas las gravedades</option><option value="critical">Críticas</option><option value="warning">Avisos</option><option value="info">Información</option>
        </select>
        {rule && <button className="btn btn-quiet" onClick={() => set("regla", "")}>Quitar el filtro de regla</button>}
      </div>
      {!list.data ? <p className="muted">Cargando…</p> : list.data.items.length === 0 ? <EmptyState message="No hay alertas con este filtro." /> : (
        <div className="table-wrap">
          <table>
            <thead><tr><th scope="col">Gravedad</th><th scope="col">Alerta</th><th scope="col">Estado</th><th scope="col">Desde</th><th scope="col"><span className="visually-hidden">Acciones</span></th></tr></thead>
            <tbody>
              {list.data.items.map((a) => (
                <tr key={a.id}>
                  <td className="nowrap"><span className={SEVERITY_CLASS[a.severity]}>{SEVERITY_LABEL[a.severity]}</span></td>
                  <td><Link className="row-link" to={`/alertas/${a.id}`}>{a.title}</Link><div className="muted small">{a.rule_name}</div></td>
                  <td>{STATE_LABEL[a.state]}</td>
                  <td className="nowrap" title={formatDateTime(a.opened_at)}>{relativeTime(a.opened_at)}</td>
                  <td className="num nowrap">
                    {can("operator") && a.state === "open" && <button className="btn btn-small" onClick={() => void act(a.id, "acknowledge")}>Reconocer</button>}{" "}
                    {can("operator") && a.state !== "resolved" && <button className="btn btn-small" onClick={() => void act(a.id, "resolve")}>Resolver</button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
