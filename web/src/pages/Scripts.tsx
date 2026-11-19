import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import EmptyState from "../components/EmptyState";
import { platformLabel, relativeTime } from "../lib/format";

export interface ScriptRow {
  id: string;
  name: string;
  description: string;
  platform: "windows" | "linux";
  interpreter: string;
  timeout_s: number;
  latest_version: number;
  archived: boolean;
  parameters: { name: string; description: string; required: boolean; default: string }[];
  created_at: string;
  updated_at: string;
}

export default function Scripts() {
  const { can } = useAuth();
  const [params, setParams] = useSearchParams();
  const archived = params.get("archived") === "1";
  const scripts = useQuery({ queryKey: ["scripts", archived], queryFn: () => api<ScriptRow[]>("GET", `/scripts${archived ? "?archived=1" : ""}`) });
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Scripts</h1>
          <p className="muted">Se ejecutan como administrador del sistema (SYSTEM o root) en los equipos. Cada cambio crea una versión nueva.</p>
        </div>
        {can("admin") && <div className="page-actions"><Link className="btn btn-primary" to="/scripts/nuevo">Nuevo script</Link></div>}
      </div>
      <p className="small" style={{ marginBottom: "0.75rem" }}>
        <button className="btn btn-quiet" onClick={() => setParams(archived ? {} : { archived: "1" })}>{archived ? "Ver los activos" : "Ver los archivados"}</button>
      </p>
      {!scripts.data ? (
        <p className="muted">Cargando…</p>
      ) : scripts.data.length === 0 ? (
        <EmptyState message={archived ? "No hay scripts archivados." : "Aún no hay scripts."} />
      ) : (
        <div className="table-wrap">
          <table>
            <thead><tr><th scope="col">Nombre</th><th scope="col">Plataforma</th><th scope="col">Intérprete</th><th scope="col">Versión</th><th scope="col">Modificado</th></tr></thead>
            <tbody>
              {scripts.data.map((s) => (
                <tr key={s.id}>
                  <td><Link className="row-link" to={`/scripts/${s.id}`}>{s.name}</Link>{s.description && <div className="muted small">{s.description}</div>}</td>
                  <td>{platformLabel(s.platform)}</td>
                  <td className="mono">{s.interpreter}</td>
                  <td>v{s.latest_version}</td>
                  <td>{relativeTime(s.updated_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
