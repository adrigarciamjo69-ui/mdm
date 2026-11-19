import { useState, type FormEvent } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { Group } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import Pagination from "../components/Pagination";
import { formatDateTime } from "../lib/format";
import type { Channel } from "./AlertRules";

interface Definition { name: string; title: string; description: string; columns: string[]; filters: string[] }
interface Schedule { id: string; report: string; title: string; frequency: string; time: string; day: number; channel_name: string; enabled: boolean;
  last_run_at: string | null; last_status: string; next_run_at: string | null }

const DAYS = ["domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"];
function describe(s: Schedule) {
  if (s.frequency === "daily") return `Cada día a las ${s.time}`;
  if (s.frequency === "weekly") return `Cada ${DAYS[s.day]} a las ${s.time}`;
  return `El día ${s.day} de cada mes a las ${s.time}`;
}

export default function Reports() {
  const defs = useQuery({ queryKey: ["reports"], queryFn: () => api<Definition[]>("GET", "/reports") });
  const schedules = useQuery({ queryKey: ["report-schedules"], queryFn: () => api<Schedule[]>("GET", "/report-schedules") });
  const { can } = useAuth();
  const queryClient = useQueryClient();
  return (
    <>
      <div className="page-head"><div><h1>Informes</h1><p className="muted">Informes predefinidos: se consultan aquí, se exportan a CSV y se pueden recibir por correo periódicamente.</p></div></div>
      <div className="report-grid">
        {defs.data?.map((d) => (
          <Link key={d.name} to={`/informes/${d.name}`} className="panel report-card"><strong>{d.title}</strong><span className="muted small">{d.description}</span></Link>
        ))}
      </div>
      <section className="section">
        <h2>Envíos programados</h2>
        {!schedules.data || schedules.data.length === 0 ? <p className="muted">Ninguno. Prográmalos desde la página de cada informe.</p> : (
          <div className="table-wrap">
            <table>
              <thead><tr><th scope="col">Informe</th><th scope="col">Cuándo</th><th scope="col">Canal</th><th scope="col">Último envío</th><th scope="col"><span className="visually-hidden">Acciones</span></th></tr></thead>
              <tbody>
                {schedules.data.map((s) => (
                  <tr key={s.id}>
                    <td><Link className="row-link" to={`/informes/${s.report}`}>{s.title}</Link></td>
                    <td>{describe(s)}{!s.enabled && <span className="muted"> · pausado</span>}</td>
                    <td>{s.channel_name}</td>
                    <td className="small">{s.last_run_at ? `${formatDateTime(s.last_run_at)} · ${s.last_status}` : "—"}</td>
                    <td className="num">{can("operator") && <button className="btn btn-small" onClick={async () => {
                      await api("DELETE", `/report-schedules/${s.id}`); void queryClient.invalidateQueries({ queryKey: ["report-schedules"] });
                    }}>Eliminar</button>}</td>
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

function ScheduleForm({ report, filters, onDone }: { report: string; filters: Record<string, unknown>; onDone(): void }) {
  const channels = useQuery({ queryKey: ["channels"], queryFn: () => api<Channel[]>("GET", "/channels") });
  const [frequency, setFrequency] = useState("weekly");
  const [time, setTime] = useState("08:00");
  const [day, setDay] = useState(1);
  const [channel, setChannel] = useState("");
  const [error, setError] = useState("");
  async function save(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      await api("POST", "/report-schedules", { report, filters, frequency, time, day: frequency === "daily" ? 0 : day, channel_id: channel, enabled: true });
      onDone();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido programar.");
    }
  }
  return (
    <form className="panel" onSubmit={save} noValidate style={{ marginTop: "1rem" }}>
      <h2>Recibir este informe</h2>
      <p className="muted small">Con los filtros actuales. Por correo llega el CSV adjunto (hasta 10 MB); por webhook, un enlace.</p>
      <div className="edit-grid">
        <label className="field"><span>Frecuencia</span>
          <select value={frequency} onChange={(e) => setFrequency(e.target.value)}><option value="daily">Cada día</option><option value="weekly">Cada semana</option><option value="monthly">Cada mes</option></select></label>
        {frequency === "weekly" && <label className="field"><span>Día</span>
          <select value={day} onChange={(e) => setDay(Number(e.target.value))}>{[1, 2, 3, 4, 5, 6, 0].map((d) => <option key={d} value={d}>{DAYS[d]}</option>)}</select></label>}
        {frequency === "monthly" && <label className="field"><span>Día del mes</span><input type="number" min={1} max={31} value={day} onChange={(e) => setDay(Number(e.target.value))} /></label>}
        <label className="field"><span>Hora</span><input type="time" value={time} onChange={(e) => setTime(e.target.value)} /></label>
        <label className="field"><span>Canal</span>
          <select value={channel} onChange={(e) => setChannel(e.target.value)}><option value="">Elige…</option>{channels.data?.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}</select></label>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      <button className="btn btn-primary" disabled={!channel}>Programar</button>
    </form>
  );
}

export function ReportView() {
  const { name = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const [scheduling, setScheduling] = useState(false);
  const [done, setDone] = useState(false);
  const defs = useQuery({ queryKey: ["reports"], queryFn: () => api<Definition[]>("GET", "/reports") });
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });
  const def = defs.data?.find((d) => d.name === name);
  const group = params.get("grupo") ?? "", platform = params.get("plataforma") ?? "", q = params.get("q") ?? "", days = params.get("dias") ?? "";
  const pageNum = Number(params.get("pagina") ?? "1");
  const qs = new URLSearchParams(Object.entries({ group, platform, q, days }).filter(([, v]) => v)).toString();
  const data = useQuery({ queryKey: ["report", name, qs, pageNum], queryFn: () =>
    api<{ columns: string[]; rows: string[][]; total: number }>("GET", `/reports/${name}?${qs}${qs ? "&" : ""}page=${pageNum}&per_page=50`) });
  const set = (k: string, v: string) => { const n = new URLSearchParams(params); if (v) n.set(k, v); else n.delete(k); n.delete("pagina"); setParams(n); };
  if (defs.data && !def) return <div className="notice"><p>Este informe no existe.</p><Link to="/informes">Volver a informes</Link></div>;
  const filters: Record<string, unknown> = { group_id: group, platform, q, ...(days ? { days: Number(days) } : {}) };
  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/informes">Informes</Link></p>
      <div className="page-head">
        <div><h1>{def?.title ?? "Informe"}</h1><p className="muted">{def?.description}</p></div>
        <div className="page-actions">
          <a className="btn" href={`/api/v1/reports/${name}?${qs}${qs ? "&" : ""}format=csv`}>Exportar CSV</a>
          {can("operator") && <button className="btn" onClick={() => setScheduling(!scheduling)}>Programar envío</button>}
        </div>
      </div>
      {scheduling && <ScheduleForm report={name} filters={filters} onDone={() => { setScheduling(false); setDone(true); void queryClient.invalidateQueries({ queryKey: ["report-schedules"] }); }} />}
      {done && <p className="form-ok" role="status">Envío programado. Lo verás en la lista de Informes.</p>}
      <div className="filters filters-inline">
        <select aria-label="Grupo" value={group} onChange={(e) => set("grupo", e.target.value)}>
          <option value="">Todos los grupos</option>{groups.data?.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}</select>
        <select aria-label="Plataforma" value={platform} onChange={(e) => set("plataforma", e.target.value)}>
          <option value="">Todas las plataformas</option><option value="windows">Windows</option><option value="linux">Linux</option><option value="android">Android</option></select>
        {def?.filters.includes("q") && <input aria-label="Buscar programa" placeholder="Buscar programa" defaultValue={q} onKeyDown={(e) => { if (e.key === "Enter") set("q", (e.target as HTMLInputElement).value); }} />}
        {def?.filters.includes("days") && <label className="small">Más de <input type="number" min={1} style={{ width: "5rem" }} aria-label="Días" defaultValue={days || "30"}
          onBlur={(e) => set("dias", e.target.value)} /> días</label>}
      </div>
      {!data.data ? <p className="muted">Cargando…</p> : data.data.rows.length === 0 ? <p className="muted">Sin resultados.</p> : (
        <>
          <div className="table-wrap">
            <table>
              <thead><tr>{data.data.columns.map((c) => <th key={c} scope="col">{c}</th>)}</tr></thead>
              <tbody>{data.data.rows.map((r, i) => <tr key={i}>{r.map((v, j) => <td key={j}>{v}</td>)}</tr>)}</tbody>
            </table>
          </div>
          <Pagination page={pageNum} perPage={50} total={data.data.total} onChange={(p) => { const n = new URLSearchParams(params); n.set("pagina", String(p)); setParams(n); }} />
        </>
      )}
    </>
  );
}
