import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { AppStateBadge, INTENT, type DeviceDeploymentRow } from "../lib/deployments";

/** Despliegues que aplican al equipo, con su estado. */
export default function ManagedSoftwareTab({ id }: { id: string }) {
  const rows = useQuery({ queryKey: ["device-deployments", id], queryFn: () => api<DeviceDeploymentRow[]>("GET", `/devices/${id}/deployments`) });
  if (!rows.data) return <p className="muted">Cargando…</p>;
  if (rows.data.length === 0) return <p className="empty">Ningún despliegue aplica a este equipo.</p>;
  return (
    <div className="table-wrap">
      <table>
        <thead><tr><th scope="col">Aplicación</th><th scope="col">Acción</th><th scope="col">Estado</th><th scope="col">Detalle</th></tr></thead>
        <tbody>
          {rows.data.map((r) => (
            <tr key={r.deployment_id}>
              <td><Link className="row-link" to={`/despliegues/${r.deployment_id}`}>{r.app_name}</Link></td>
              <td>{INTENT[r.intent] ?? r.intent}</td>
              <td className="nowrap"><AppStateBadge state={r.state} /></td>
              <td className="small" style={{ overflowWrap: "anywhere" }}>{r.message}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
