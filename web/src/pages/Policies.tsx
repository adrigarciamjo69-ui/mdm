import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { Policy } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import EmptyState from "../components/EmptyState";
import { platformLabel } from "../lib/format";

export function assignmentSummary(p: Policy): string {
  if (p.assignments.length === 0) return "Sin asignar";
  const inc = p.assignments.filter((a) => !a.exclude).map((a) => a.group_name);
  const exc = p.assignments.filter((a) => a.exclude).map((a) => a.group_name);
  return inc.join(", ") + (exc.length ? ` (excepto ${exc.join(", ")})` : "");
}

export default function Policies() {
  const { can } = useAuth();
  const isAdmin = can("admin");
  const queryClient = useQueryClient();
  const policies = useQuery({ queryKey: ["policies"], queryFn: () => api<Policy[]>("GET", "/policies") });

  async function move(index: number, delta: number) {
    const list = [...(policies.data ?? [])];
    const [p] = list.splice(index, 1);
    list.splice(index + delta, 0, p);
    queryClient.setQueryData(["policies"], list.map((x, i) => ({ ...x, priority: i + 1 })));
    try {
      await api("POST", "/policies/reorder", { ids: list.map((x) => x.id) });
    } finally {
      void queryClient.invalidateQueries({ queryKey: ["policies"] });
    }
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Políticas</h1>
          <p className="muted">Configuraciones que los equipos deben cumplir. Si dos políticas fijan el mismo ajuste, gana la de mayor prioridad (número más bajo).</p>
        </div>
        {isAdmin && <div className="page-actions"><Link className="btn btn-primary" to="/politicas/nueva">Nueva política</Link></div>}
      </div>
      {policies.error ? (
        <p className="form-error" role="alert">{policies.error instanceof ApiError ? policies.error.message : "No se han podido cargar las políticas."}</p>
      ) : !policies.data ? (
        <p className="muted">Cargando…</p>
      ) : policies.data.length === 0 ? (
        <EmptyState message="Aún no hay políticas." />
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th scope="col" className="num">Prioridad</th><th scope="col">Nombre</th><th scope="col">Plataforma</th>
                <th scope="col" className="num">Ajustes</th><th scope="col">Se aplica a</th>
                {isAdmin && <th scope="col"><span className="visually-hidden">Orden</span></th>}
              </tr>
            </thead>
            <tbody>
              {policies.data.map((p, i) => (
                <tr key={p.id}>
                  <td className="num">{p.priority}</td>
                  <td><Link className="row-link" to={`/politicas/${p.id}`}>{p.name}</Link></td>
                  <td>{platformLabel(p.platform)}</td>
                  <td className="num">{p.settings.length}</td>
                  <td>{assignmentSummary(p)}</td>
                  {isAdmin && (
                    <td className="num nowrap">
                      <button className="btn btn-small" disabled={i === 0} onClick={() => void move(i, -1)} aria-label={`Subir ${p.name}`}>▲</button>{" "}
                      <button className="btn btn-small" disabled={i === policies.data.length - 1} onClick={() => void move(i, 1)} aria-label={`Bajar ${p.name}`}>▼</button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
