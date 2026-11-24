import { Link } from "react-router-dom";
import { useEffect, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { Group, Page } from "../api/types";
import { formatDateTime } from "../lib/format";

interface Values {
  agent_auto_update: boolean;
  max_offline_days: number;
  min_agent_version: string;
  min_os_version: Record<string, string>;
  max_security_update_age_days: number;
  min_android_patch_level?: string;
  schedule_timezone?: string;
}

interface Releases {
  version: string;
  binaries: string[];
  pins: { group_id: string; group_name: string; version: string }[];
}

interface Job {
  id: number;
  kind: string;
  attempts: number;
  last_error: string;
  failed_at: string | null;
}

/** Diferencias entre dos objetos de ajustes, campo a campo. */
function changes(before: Values, after: Values): Partial<Values> {
  const out: Partial<Values> = {};
  (Object.keys(after) as (keyof Values)[]).forEach((k) => {
    if (JSON.stringify(before[k]) !== JSON.stringify(after[k])) (out as Record<string, unknown>)[k] = after[k];
  });
  return out;
}

export default function Settings() {
  const queryClient = useQueryClient();
  const saved = useQuery({ queryKey: ["settings"], queryFn: () => api<Values>("GET", "/settings") });
  const releases = useQuery({ queryKey: ["agent-releases"], queryFn: () => api<Releases>("GET", "/agent-releases") });
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });
  const jobs = useQuery({ queryKey: ["jobs-failed"], queryFn: () => api<Page<Job>>("GET", "/jobs?state=failed&per_page=50") });

  const [v, setV] = useState<Values | null>(null);
  const [msg, setMsg] = useState<{ section: string; ok: boolean; text: string } | null>(null);
  const [pinGroup, setPinGroup] = useState("");
  useEffect(() => { if (saved.data) setV(saved.data); }, [saved.data]);

  async function save(section: string, e?: FormEvent) {
    e?.preventDefault();
    if (!v || !saved.data) return;
    setMsg(null);
    const diff = changes(saved.data, v);
    if (Object.keys(diff).length === 0) {
      setMsg({ section, ok: true, text: "No hay cambios que guardar." });
      return;
    }
    try {
      const next = await api<Values>("PUT", "/settings", diff);
      queryClient.setQueryData(["settings"], next);
      setV(next);
      setMsg({ section, ok: true, text: "Ajustes guardados." });
    } catch (err) {
      setMsg({ section, ok: false, text: err instanceof ApiError ? err.message : "No se han podido guardar los ajustes." });
    }
  }

  async function pin() {
    if (!pinGroup || !releases.data) return;
    try {
      await api("PUT", `/agent-pins/${pinGroup}`, { version: releases.data.version });
      setPinGroup("");
      void queryClient.invalidateQueries({ queryKey: ["agent-releases"] });
    } catch (err) {
      setMsg({ section: "agent", ok: false, text: err instanceof ApiError ? err.message : "No se ha podido congelar el grupo." });
    }
  }

  async function unpin(groupId: string) {
    await api("DELETE", `/agent-pins/${groupId}`);
    void queryClient.invalidateQueries({ queryKey: ["agent-releases"] });
  }

  const feedback = (section: string) =>
    msg?.section === section && <p className={msg.ok ? "form-ok" : "form-error"} role={msg.ok ? "status" : "alert"}>{msg.text}</p>;

  if (!v) return <p className="muted">Cargando…</p>;
  const pinned = new Set(releases.data?.pins.map((p) => p.group_id));
  const pinnable = (groups.data ?? []).filter((g) => g.kind !== "dynamic" && !pinned.has(g.id));

  return (
    <>
      <div className="page-head"><h1>Ajustes</h1>
        <div className="page-actions"><Link className="btn" to="/ajustes/inventario">Ajustes del inventario (Entra ID, categorías, actas)</Link></div>
      </div>

      <section className="panel" aria-labelledby="s-agent">
        <h2 id="s-agent">Agentes</h2>
        <p className="muted small">
          {releases.data?.version ? `Versión distribuida: ${releases.data.version}` : "El servidor no distribuye ninguna versión del agente (falta SHA256SUMS en la carpeta de descargas)."}
        </p>
        <label className="check-field">
          <input type="checkbox" checked={v.agent_auto_update} onChange={(e) => setV({ ...v, agent_auto_update: e.target.checked })} />
          <span>Actualizar los agentes automáticamente</span>
        </label>
        <button className="btn btn-small" onClick={() => void save("agent")}>Guardar</button>
        {feedback("agent")}
        <h3 style={{ marginTop: "1.25rem" }}>Grupos con la versión congelada</h3>
        <p className="muted small">No reciben actualizaciones. Sirve para probar una versión nueva primero en unos pocos equipos: congela el resto y descongélalo cuando estés conforme.</p>
        {releases.data && releases.data.pins.length > 0 ? (
          <ul className="assign-list">
            {releases.data.pins.map((p) => (
              <li key={p.group_id}>
                <span className="tag">{p.group_name} · {p.version}</span>
                <button className="btn btn-quiet" aria-label={`Descongelar ${p.group_name}`} onClick={() => void unpin(p.group_id)}>Descongelar</button>
              </li>
            ))}
          </ul>
        ) : <p className="muted small">Ninguno.</p>}
        <div className="inline-form">
          <label className="field">
            <span>Grupo a congelar</span>
            <select value={pinGroup} onChange={(e) => setPinGroup(e.target.value)}>
              <option value="">Elige…</option>
              {pinnable.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
            </select>
          </label>
          <button className="btn" disabled={!pinGroup || !releases.data?.version} onClick={() => void pin()}>Congelar</button>
        </div>
      </section>

      <form className="panel" style={{ marginTop: "1rem" }} onSubmit={(e) => void save("compliance", e)} noValidate aria-labelledby="s-comp">
        <h2 id="s-comp">Cumplimiento</h2>
        <p className="muted small">Reglas que evalúa el servidor además de las políticas. Un equipo que no las cumple aparece como «No cumple».</p>
        <div className="edit-grid">
          <label className="field">
            <span>Días máximos sin conexión</span>
            <input type="number" min={1} max={365} value={v.max_offline_days} onChange={(e) => setV({ ...v, max_offline_days: Number(e.target.value) })} />
          </label>
          <label className="field">
            <span>Máximo de días con parches de seguridad pendientes <span className="hint">0 = sin regla</span></span>
            <input type="number" min={0} max={365} value={v.max_security_update_age_days ?? 30} onChange={(e) => setV({ ...v, max_security_update_age_days: Number(e.target.value) })} />
          </label>
          <label className="field">
            <span>Zona horaria de las programaciones <span className="hint">informes y automatizaciones</span></span>
            <input value={v.schedule_timezone ?? ""} placeholder="Europe/Madrid" onChange={(e) => setV({ ...v, schedule_timezone: e.target.value.trim() })} />
          </label>
          <label className="field">
            <span>Parche de seguridad mínimo de Android <span className="hint">AAAA-MM-DD</span></span>
            <input value={v.min_android_patch_level ?? ""} placeholder="sin mínimo" onChange={(e) => setV({ ...v, min_android_patch_level: e.target.value.trim() })} />
          </label>
          <label className="field">
            <span>Versión mínima del agente</span>
            <input value={v.min_agent_version} placeholder="sin mínimo" onChange={(e) => setV({ ...v, min_agent_version: e.target.value.trim() })} />
          </label>
          <label className="field">
            <span>Versión mínima de Windows</span>
            <input value={v.min_os_version.windows ?? ""} placeholder="p. ej. 10.0.19045" onChange={(e) => setV({ ...v, min_os_version: { ...v.min_os_version, windows: e.target.value.trim() } })} />
          </label>
          <label className="field">
            <span>Versión mínima de Linux</span>
            <input value={v.min_os_version.linux ?? ""} placeholder="p. ej. 22.04" onChange={(e) => setV({ ...v, min_os_version: { ...v.min_os_version, linux: e.target.value.trim() } })} />
          </label>
        </div>
        <button className="btn btn-primary">Guardar cumplimiento</button>
        {feedback("compliance")}
      </form>

      <section className="section" aria-labelledby="s-jobs">
        <h2 id="s-jobs">Tareas en segundo plano con errores</h2>
        {!jobs.data ? <p className="muted">Cargando…</p> : jobs.data.items.length === 0 ? (
          <p className="muted">Ninguna tarea ha fallado en los últimos 7 días.</p>
        ) : (
          <div className="table-wrap">
            <table>
              <thead><tr><th scope="col">Tarea</th><th scope="col" className="num">Intentos</th><th scope="col">Último error</th><th scope="col">Fecha</th></tr></thead>
              <tbody>
                {jobs.data.items.map((j) => (
                  <tr key={j.id}>
                    <td className="mono">{j.kind}</td>
                    <td className="num">{j.attempts}</td>
                    <td className="small">{j.last_error}</td>
                    <td className="nowrap">{j.failed_at ? formatDateTime(j.failed_at) : "—"}</td>
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
