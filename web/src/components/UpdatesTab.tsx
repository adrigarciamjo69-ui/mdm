import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { formatDateTime, relativeTime } from "../lib/format";
import { CLASS_LABEL, describeWindow, updateSubtitle, type MaintWindow } from "../lib/patches";

interface DeviceUpdates {
  state: { last_scan_at: string | null; last_install_at: string | null; reboot_pending: boolean; window: MaintWindow | null };
  updates: { id: string; kb: string; title: string; classification: string; severity: string; security: boolean; state: string;
    approved: boolean; first_seen_at: string; installed_at: string | null; last_error: string }[];
}

/** Estado de parches del equipo: pendientes, instaladas y acciones. */
export default function UpdatesTab({ id }: { id: string }) {
  const { can } = useAuth();
  const data = useQuery({ queryKey: ["device-updates", id], queryFn: () => api<DeviceUpdates>("GET", `/devices/${id}/updates`) });
  const [msg, setMsg] = useState("");
  async function run(type: "scan_updates" | "install_updates_now") {
    setMsg("");
    try {
      await api("POST", "/batches", { type, device_ids: [id] });
      setMsg(type === "scan_updates" ? "Búsqueda pedida: el equipo la hará en su siguiente conexión." : "Instalación pedida: el equipo la hará en su siguiente conexión.");
    } catch (e) {
      setMsg(e instanceof ApiError ? e.message : "No se ha podido enviar la orden.");
    }
  }
  if (!data.data) return <p className="muted">Cargando…</p>;
  const { state, updates } = data.data;
  const pending = updates.filter((u) => u.state !== "installed");
  const installed = updates.filter((u) => u.state === "installed");
  return (
    <>
      <div className="panel">
        <p>
          Última búsqueda: {state.last_scan_at ? relativeTime(state.last_scan_at) : "nunca"} · Última instalación: {state.last_install_at ? relativeTime(state.last_install_at) : "nunca"}
          {state.reboot_pending && <strong style={{ color: "var(--warn)" }}> · Reinicio pendiente</strong>}
        </p>
        <p className="muted small">{state.window ? "Ventana de mantenimiento: " + describeWindow(state.window) + "." : "Sin política de parches: no se instala nada automáticamente."}</p>
        {can("operator") && (
          <div style={{ display: "flex", gap: "0.5rem", marginTop: "0.75rem" }}>
            <button className="btn btn-small" onClick={() => void run("scan_updates")}>Buscar ahora</button>
            <button className="btn btn-small" onClick={() => void run("install_updates_now")} disabled={!pending.some((u) => u.approved)}>Instalar ahora las aprobadas</button>
          </div>
        )}
        {msg && <p className="small" role="status" style={{ marginTop: "0.5rem" }}>{msg}</p>}
      </div>
      <h2 style={{ marginTop: "1.25rem" }}>Pendientes ({pending.length})</h2>
      {pending.length === 0 ? <p className="muted">Ninguna.</p> : (
        <div className="table-wrap">
          <table>
            <thead><tr><th scope="col">Actualización</th><th scope="col">Clasificación</th><th scope="col">Estado</th><th scope="col">Desde</th></tr></thead>
            <tbody>
              {pending.map((u) => (
                <tr key={u.id}>
                  <td><strong>{u.title}</strong><div className="muted small">{updateSubtitle(u, u.kb ? "windows" : "linux")}</div></td>
                  <td>{CLASS_LABEL[u.classification] ?? u.classification}</td>
                  <td>{u.approved ? <span className="tag">Aprobada</span> : <span className="muted small">Sin aprobar</span>}
                    {u.state === "failed" && <div className="small" style={{ color: "var(--bad)" }}>Falló: {u.last_error}</div>}</td>
                  <td className="nowrap">{formatDateTime(u.first_seen_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {installed.length > 0 && (
        <details style={{ marginTop: "1rem" }}>
          <summary>Instaladas recientemente ({installed.length})</summary>
          <ul className="small">{installed.map((u) => <li key={u.id}>{u.title} {u.installed_at && <span className="muted">· {formatDateTime(u.installed_at)}</span>}</li>)}</ul>
        </details>
      )}
    </>
  );
}
