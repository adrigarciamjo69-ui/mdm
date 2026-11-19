import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Page } from "../api/types";
import EmptyState from "../components/EmptyState";
import Pagination from "../components/Pagination";
import { formatDateTime } from "../lib/format";
import { commandTitle, FINISHED, type BatchRow } from "../lib/commands";

export function batchProgress(b: BatchRow) {
  const done = FINISHED.reduce((n, s) => n + (b.counts[s] ?? 0), 0);
  return { done, total: b.device_count, failed: (b.counts.failed ?? 0) + (b.counts.expired ?? 0) };
}

export function ProgressBar({ b }: { b: BatchRow }) {
  const { done, total, failed } = batchProgress(b);
  const pct = (n: number) => `${total ? (n / total) * 100 : 0}%`;
  return (
    <div className="progress" aria-hidden="true">
      <span className="progress-ok" style={{ width: pct(done - failed) }} />
      <span className="progress-bad" style={{ width: pct(failed) }} />
    </div>
  );
}

export default function Activity() {
  const [params, setParams] = useSearchParams();
  const page = Number(params.get("page") ?? "1");
  const batches = useQuery({
    queryKey: ["batches", page],
    queryFn: () => api<Page<BatchRow>>("GET", `/batches?page=${page}&per_page=50`),
    refetchInterval: 10_000,
  });
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Actividad</h1>
          <p className="muted">Órdenes enviadas a los equipos. Cada equipo las recibe en su siguiente conexión (como mucho, un minuto).</p>
        </div>
      </div>
      {!batches.data ? (
        <p className="muted">Cargando…</p>
      ) : batches.data.items.length === 0 ? (
        <EmptyState message="Aún no se ha enviado ninguna orden. Se lanzan desde el listado de dispositivos o desde la ficha de un equipo." />
      ) : (
        <>
          <div className="table-wrap">
            <table>
              <thead><tr><th scope="col">Fecha</th><th scope="col">Acción</th><th scope="col">Destino</th><th scope="col">Progreso</th><th scope="col">Lanzada por</th></tr></thead>
              <tbody>
                {batches.data.items.map((b) => {
                  const p = batchProgress(b);
                  return (
                    <tr key={b.id}>
                      <td className="nowrap">{formatDateTime(b.created_at)}</td>
                      <td><Link className="row-link" to={`/actividad/${b.id}`}>{commandTitle(b.type, b.params)}</Link></td>
                      <td>{b.target_description}</td>
                      <td style={{ minWidth: "10rem" }}>
                        <ProgressBar b={b} />
                        <span className="small">{p.done} de {p.total} terminadas</span>
                        {p.failed > 0 && <span className="small" style={{ color: "var(--bad)" }}> · {p.failed} con error</span>}
                      </td>
                      <td>{b.created_by_email}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <Pagination page={page} perPage={50} total={batches.data.total} onChange={(n: number) => setParams({ page: String(n) })} />
        </>
      )}
    </>
  );
}
