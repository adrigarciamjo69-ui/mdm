import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import EmptyState from "../components/EmptyState";
import { INTENT, type DeploymentRow } from "../lib/deployments";
import { assignmentSummary } from "./Policies";

export default function Deployments() {
  const { can } = useAuth();
  const deps = useQuery({ queryKey: ["deployments"], queryFn: () => api<DeploymentRow[]>("GET", "/deployments"), refetchInterval: 30_000 });
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Despliegues</h1>
          <p className="muted">Qué software debe estar instalado (o no) en cada grupo. Los equipos lo comprueban cada pocos minutos y lo corrigen.</p>
        </div>
        {can("admin") && <div className="page-actions"><Link className="btn btn-primary" to="/despliegues/nuevo">Nuevo despliegue</Link></div>}
      </div>
      {!deps.data ? <p className="muted">Cargando…</p> : deps.data.length === 0 ? <EmptyState message="Aún no hay despliegues." /> : (
        <div className="table-wrap">
          <table>
            <thead><tr><th scope="col">Aplicación</th><th scope="col">Acción</th><th scope="col">Se aplica a</th><th scope="col">Estado</th></tr></thead>
            <tbody>
              {deps.data.map((d) => {
                const done = (d.counts.installed ?? 0) + (d.counts.absent ?? 0);
                return (
                  <tr key={d.id}>
                    <td><Link className="row-link" to={`/despliegues/${d.id}`}>{d.app_name}</Link></td>
                    <td>{INTENT[d.intent]}{d.keep_updated && <div className="muted small">y mantener actualizada</div>}</td>
                    <td>{assignmentSummary({ assignments: d.assignments } as never)}</td>
                    <td className="small">
                      {done} {d.intent === "uninstall" ? "desinstalados" : "instalados"}
                      {(d.counts.pending ?? 0) + (d.counts.waiting_window ?? 0) + (d.counts.downloading ?? 0) + (d.counts.installing ?? 0) > 0 &&
                        ` · ${(d.counts.pending ?? 0) + (d.counts.waiting_window ?? 0) + (d.counts.downloading ?? 0) + (d.counts.installing ?? 0)} en curso`}
                      {(d.counts.failed ?? 0) > 0 && <span style={{ color: "var(--bad)" }}> · {d.counts.failed} con error</span>}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
