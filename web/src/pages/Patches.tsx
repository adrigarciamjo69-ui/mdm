import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Page } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import EmptyState from "../components/EmptyState";
import { platformLabel } from "../lib/format";
import { CLASS_LABEL, CLASSES, describeWindow, updateSubtitle, type MaintWindow } from "../lib/patches";
import { assignmentSummary } from "./Policies";

interface UpdateRow {
  id: string; platform: string; kb: string; title: string; classification: string; severity: string; security: boolean;
  missing: number; installed: number; failed: number; decision: "approved" | "declined" | null;
}

export interface PatchPolicyRow {
  id: string; name: string; platform: "windows" | "linux"; priority: number; auto_approve: string[]; defer_days: number;
  window: MaintWindow; reboot: "never" | "notify" | "in_window"; notify_minutes: number; linux_scope: "security" | "all";
  excluded_kbs: string[]; assignments: { group_id: string | null; group_name: string; exclude: boolean }[];
}

const DECISION: Record<string, string> = { approved: "Aprobada", declined: "Rechazada" };

export default function Patches() {
  const { can } = useAuth();
  const isAdmin = can("admin");
  const queryClient = useQueryClient();
  const [platform, setPlatform] = useState("windows");
  const [cls, setCls] = useState("");
  const updates = useQuery({
    queryKey: ["updates", platform, cls],
    queryFn: () => api<Page<UpdateRow>>("GET", `/updates?platform=${platform}${cls ? "&classification=" + cls : ""}&per_page=100`),
  });
  const policies = useQuery({ queryKey: ["patch-policies"], queryFn: () => api<PatchPolicyRow[]>("GET", "/patch-policies") });

  async function decide(id: string, decision: "approved" | "declined" | null) {
    await api("PUT", `/updates/${encodeURIComponent(id)}/approval`, { decision });
    void queryClient.invalidateQueries({ queryKey: ["updates"] });
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Parches</h1>
          <p className="muted">Actualizaciones del sistema que les faltan a los equipos. Las políticas de parches deciden cuáles se instalan solas y cuándo.</p>
        </div>
      </div>

      <section>
        <div className="page-head" style={{ marginBottom: "0.75rem" }}>
          <h2>Políticas de parches</h2>
          {isAdmin && <Link className="btn" to="/parches/politicas/nueva">Nueva política de parches</Link>}
        </div>
        {!policies.data ? null : policies.data.length === 0 ? (
          <p className="muted">Ninguna: sin política de parches, los equipos informan de lo que les falta pero no instalan nada.</p>
        ) : (
          <div className="table-wrap">
            <table>
              <thead><tr><th scope="col" className="num">Prioridad</th><th scope="col">Nombre</th><th scope="col">Plataforma</th><th scope="col">Ventana</th><th scope="col">Se aplica a</th></tr></thead>
              <tbody>
                {policies.data.map((p) => (
                  <tr key={p.id}>
                    <td className="num">{p.priority}</td>
                    <td><Link className="row-link" to={`/parches/politicas/${p.id}`}>{p.name}</Link></td>
                    <td>{platformLabel(p.platform)}</td>
                    <td className="small">{describeWindow(p.window)}</td>
                    <td>{assignmentSummary(p as never)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="section">
        <h2>Actualizaciones</h2>
        <div className="filters filters-inline">
          <label><span className="visually-hidden">Plataforma</span>
            <select aria-label="Plataforma" value={platform} onChange={(e) => setPlatform(e.target.value)}>
              <option value="windows">Windows</option><option value="linux">Linux</option>
            </select>
          </label>
          <label><span className="visually-hidden">Clasificación</span>
            <select aria-label="Clasificación" value={cls} onChange={(e) => setCls(e.target.value)}>
              <option value="">Todas las clasificaciones</option>
              {CLASSES.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
            </select>
          </label>
        </div>
        {!updates.data ? <p className="muted">Cargando…</p> : updates.data.items.length === 0 ? (
          <EmptyState message="No hay actualizaciones pendientes conocidas. Los equipos buscan una vez al día." />
        ) : (
          <div className="table-wrap">
            <table>
              <thead><tr><th scope="col">Actualización</th><th scope="col">Clasificación</th><th scope="col" className="num">Faltan en</th>
                <th scope="col" className="num">Instalada en</th><th scope="col" className="num">Con error</th><th scope="col">Decisión</th></tr></thead>
              <tbody>
                {updates.data.items.map((u) => (
                  <tr key={u.id}>
                    <td><strong>{u.title}</strong><div className="muted small">{[updateSubtitle(u, u.platform), u.severity].filter(Boolean).join(" · ")}</div></td>
                    <td>{CLASS_LABEL[u.classification] ?? u.classification}</td>
                    <td className="num">{u.missing}</td>
                    <td className="num">{u.installed}</td>
                    <td className="num">{u.failed}</td>
                    <td className="nowrap">
                      {u.decision ? <span className={u.decision === "approved" ? "tag" : "tag tag-exclude"}>{DECISION[u.decision]}</span> : <span className="muted small">Según la regla</span>}
                      {isAdmin && u.platform === "windows" && (
                        <div style={{ display: "flex", gap: "0.35rem", marginTop: "0.3rem" }}>
                          {u.decision !== "approved" && <button className="btn btn-small" onClick={() => void decide(u.id, "approved")}>Aprobar</button>}
                          {u.decision !== "declined" && <button className="btn btn-small" onClick={() => void decide(u.id, "declined")}>Rechazar</button>}
                          {u.decision && <button className="btn btn-small btn-quiet" onClick={() => void decide(u.id, null)}>Volver a la regla</button>}
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </>
  );
}
