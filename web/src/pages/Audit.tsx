import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api, ApiError, query } from "../api/client";
import type { AuditEntry, Page } from "../api/types";
import Pagination from "../components/Pagination";
import { AUDIT_ACTIONS, auditLabel } from "../lib/auditLabels";
import { useDebounced } from "../lib/hooks";
import { formatDateTime } from "../lib/format";

const AGENT_ACTIONS = new Set(["device.enrolled", "device.reenrolled"]);

/** Inicio o final del día local, en formato RFC 3339 para el servidor. */
function dayBound(value: string, end: boolean): string | null {
  if (!value) return null;
  const [y, m, d] = value.split("-").map(Number);
  if (!y || !m || !d) return null;
  const date = end ? new Date(y, m - 1, d, 23, 59, 59, 999) : new Date(y, m - 1, d);
  return date.toISOString();
}

function Target({ e }: { e: AuditEntry }) {
  const d = e.details ?? {};
  const str = (k: string) => (typeof d[k] === "string" ? (d[k] as string) : "");
  switch (e.target_type) {
    case "device": {
      const name = str("hostname") || e.target_id;
      const clone = e.action === "device.reenrolled" && d.hostname_changed === true;
      return (
        <>
          {e.action === "device.deleted" ? name : <Link to={`/dispositivos/${e.target_id}`}>{name}</Link>}
          {clone && (
            <div className="state-partial small">
              Antes se llamaba {str("previous_hostname")}: puede ser un equipo clonado con el mismo identificador.
            </div>
          )}
        </>
      );
    }
    case "group":
      return <>{str("name") || e.target_id}</>;
    case "token":
      return <>{str("name") || "Token"}</>;
    case "admin":
      return <>{str("email") || e.target_id}</>;
  }
  return <span className="muted">—</span>;
}

export default function Audit() {
  const [action, setAction] = useState("");
  const [actor, setActor] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [page, setPage] = useState(1);
  const actorQ = useDebounced(actor.trim(), 300);

  useEffect(() => setPage(1), [action, actorQ, from, to]);

  const path = "/audit" + query({ action, actor: actorQ, from: dayBound(from, false), to: dayBound(to, true), page: page > 1 ? page : null, per_page: 50 });
  const log = useQuery({ queryKey: ["audit", path], queryFn: () => api<Page<AuditEntry>>("GET", path), placeholderData: keepPreviousData });

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Auditoría</h1>
          <p className="muted">Registro de lo que hacen los administradores y los agentes. No se puede modificar ni borrar.</p>
        </div>
      </div>

      <div className="toolbar">
        <label>
          <span className="visually-hidden">Acción</span>
          <select aria-label="Acción" value={action} onChange={(e) => setAction(e.target.value)}>
            <option value="">Todas las acciones</option>
            {AUDIT_ACTIONS.map((a) => <option key={a} value={a}>{auditLabel(a)}</option>)}
          </select>
        </label>
        <input aria-label="Autor" placeholder="Correo del autor" value={actor} onChange={(e) => setActor(e.target.value)} style={{ maxWidth: "16rem" }} />
        <label className="nowrap small">Desde <input type="date" aria-label="Desde" value={from} onChange={(e) => setFrom(e.target.value)} style={{ width: "auto" }} /></label>
        <label className="nowrap small">Hasta <input type="date" aria-label="Hasta" value={to} onChange={(e) => setTo(e.target.value)} style={{ width: "auto" }} /></label>
      </div>

      {log.error ? (
        <p className="form-error" role="alert">{log.error instanceof ApiError ? log.error.message : "No se ha podido cargar la auditoría."}</p>
      ) : !log.data ? (
        <p className="muted">Cargando…</p>
      ) : log.data.total === 0 ? (
        <p className="empty">No hay entradas que coincidan.</p>
      ) : (
        <>
          <div className="table-wrap">
            <table className={log.isPlaceholderData ? "loading" : undefined}>
              <thead>
                <tr>
                  <th scope="col">Fecha</th><th scope="col">Autor</th><th scope="col">Acción</th>
                  <th scope="col">Objetivo</th><th scope="col">IP</th><th scope="col"><span className="visually-hidden">Detalles</span></th>
                </tr>
              </thead>
              <tbody>
                {log.data.items.map((e) => (
                  <tr key={e.id}>
                    <td className="nowrap">{formatDateTime(e.at)}</td>
                    <td>{e.actor_email || <span className="muted">{AGENT_ACTIONS.has(e.action) ? "Agente" : "Sistema"}</span>}</td>
                    <td>{auditLabel(e.action)}</td>
                    <td><Target e={e} /></td>
                    <td className="mono small">{e.ip || "—"}</td>
                    <td>
                      {e.details && Object.keys(e.details).length > 0 && (
                        <details className="audit-row">
                          <summary className="small">Detalles</summary>
                          <pre className="details">{JSON.stringify(e.details, null, 2)}</pre>
                        </details>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination page={log.data.page} perPage={log.data.per_page} total={log.data.total} onChange={setPage} />
        </>
      )}
    </>
  );
}
