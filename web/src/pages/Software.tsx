import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { App } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import EmptyState from "../components/EmptyState";
import { platformLabel } from "../lib/format";

export function sourceLabel(a: Pick<App, "source" | "package_id">): string {
  if (a.source === "upload") return "Instalador propio";
  return `${a.source === "google_play" ? "Google Play" : a.source} · ${a.package_id}`;
}

export default function Software() {
  const { can } = useAuth();
  const apps = useQuery({ queryKey: ["apps"], queryFn: () => api<App[]>("GET", "/apps") });
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Software</h1>
          <p className="muted">Aplicaciones que se pueden instalar o desinstalar en los equipos desde un despliegue.</p>
        </div>
        {can("admin") && <div className="page-actions"><Link className="btn btn-primary" to="/software/nueva">Añadir aplicación</Link></div>}
      </div>
      {!apps.data ? <p className="muted">Cargando…</p> : apps.data.length === 0 ? (
        <EmptyState message="El catálogo está vacío." />
      ) : (
        <div className="table-wrap">
          <table>
            <thead><tr><th scope="col">Aplicación</th><th scope="col">Plataforma</th><th scope="col">Origen</th><th scope="col">Versión vigente</th></tr></thead>
            <tbody>
              {apps.data.map((a) => (
                <tr key={a.id}>
                  <td><Link className="row-link" to={`/software/${a.id}`}>{a.name}</Link>{a.publisher && <div className="muted small">{a.publisher}</div>}</td>
                  <td>{platformLabel(a.platform)}</td>
                  <td className={a.source === "upload" ? "" : "mono small"}>{sourceLabel(a)}</td>
                  <td>{a.source === "upload" ? (a.current_version?.version ?? <span className="muted">Sin versiones</span>) : <span className="muted">La del repositorio</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
