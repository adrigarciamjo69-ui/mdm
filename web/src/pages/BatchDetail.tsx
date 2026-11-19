import { Fragment, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { formatDateTime } from "../lib/format";
import { commandTitle, COMMAND_STATUS, FINISHED, type BatchRow, type CommandRow } from "../lib/commands";
import { ProgressBar, batchProgress } from "./Activity";

export function CommandStatus({ status }: { status: string }) {
  const s = COMMAND_STATUS[status] ?? { label: status, cls: "status" };
  return <span className={s.cls}>{s.label}</span>;
}

export function CommandOutput({ id }: { id: string }) {
  const c = useQuery({ queryKey: ["command", id], queryFn: () => api<CommandRow>("GET", `/commands/${id}`) });
  if (!c.data) return <p className="muted small">Cargando…</p>;
  return (
    <div className="output">
      {c.data.error && <p className="form-error">{c.data.error}</p>}
      {c.data.stdout ? <pre>{c.data.stdout}</pre> : <p className="muted small">Sin salida estándar.</p>}
      {c.data.stderr && <><p className="small" style={{ fontWeight: 600 }}>Errores</p><pre>{c.data.stderr}</pre></>}
    </div>
  );
}

export default function BatchDetail() {
  const { id = "" } = useParams();
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState<string | null>(null);
  const [error, setError] = useState("");
  const b = useQuery({
    queryKey: ["batch", id],
    queryFn: () => api<BatchRow>("GET", `/batches/${id}`),
    refetchInterval: (q) => (q.state.data && batchProgress(q.state.data).done < q.state.data.device_count ? 5_000 : false),
  });

  async function cancel(c: CommandRow) {
    setError("");
    try {
      await api("POST", `/commands/${c.id}/cancel`);
      await queryClient.invalidateQueries({ queryKey: ["batch", id] });
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "No se ha podido cancelar.");
    }
  }

  if (b.error) return <div className="notice"><p>Este lote no existe.</p><Link to="/actividad">Volver a actividad</Link></div>;
  if (!b.data) return <p className="muted">Cargando…</p>;
  const x = b.data;
  const p = batchProgress(x);
  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/actividad">Actividad</Link></p>
      <div className="page-head">
        <div>
          <h1>{commandTitle(x.type, x.params)}</h1>
          <p className="muted">{x.target_description} · {formatDateTime(x.created_at)} · {x.created_by_email}</p>
        </div>
      </div>
      <div className="panel" style={{ marginBottom: "1rem" }}>
        <ProgressBar b={x} />
        <p className="small">{p.done} de {p.total} terminadas{p.failed > 0 ? ` · ${p.failed} con error` : ""}</p>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      <div className="table-wrap">
        <table>
          <thead><tr><th scope="col">Equipo</th><th scope="col">Estado</th><th scope="col" className="num">Código</th><th scope="col">Error</th><th scope="col"><span className="visually-hidden">Acciones</span></th></tr></thead>
          <tbody>
            {(x.commands ?? []).map((c) => (
              <Fragment key={c.id}>
                <tr>
                  <td><Link className="row-link" to={`/dispositivos/${c.device_id}`}>{c.hostname}</Link></td>
                  <td><CommandStatus status={c.status} /></td>
                  <td className="num">{c.exit_code ?? "—"}</td>
                  <td className="small">{c.error || ""}</td>
                  <td className="num nowrap">
                    {FINISHED.includes(c.status) && c.status !== "cancelled" && (
                      <button className="btn btn-small" aria-expanded={open === c.id} onClick={() => setOpen(open === c.id ? null : c.id)}>
                        {open === c.id ? "Ocultar salida" : "Ver salida"}
                      </button>
                    )}
                    {(c.status === "pending" || c.status === "sent") && can("operator") && (
                      <button className="btn btn-small" onClick={() => void cancel(c)}>Cancelar</button>
                    )}
                  </td>
                </tr>
                {open === c.id && (
                  <tr><td colSpan={5}><CommandOutput id={c.id} /></td></tr>
                )}
              </Fragment>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
