import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import TrendChart from "../components/TrendChart";
import { api, ApiError } from "../api/client";
import type { Status, Summary as SummaryData } from "../api/types";
import NoEnrollments from "../components/NoEnrollments";
import StatusBadge from "../components/StatusBadge";
import { formatDate, formatNumber, statusLabel } from "../lib/format";

const ORDER: Status[] = ["online", "offline", "inactive", "revoked"];

const ISSUES: { key: "low_disk" | "unencrypted" | "stale"; label: string; detail: string }[] = [
  { key: "low_disk", label: "Poco espacio en disco", detail: "Algún volumen con menos del 10 % libre" },
  { key: "unencrypted", label: "Sin cifrar", detail: "Sin BitLocker ni LUKS en los volúmenes" },
  { key: "stale", label: "Sin conexión reciente", detail: "Una semana o más sin contactar con el servidor" },
];

export type SectionKey = "status" | "alerts" | "review" | "trends" | "recent";
export const SECTIONS: [SectionKey, string][] = [["status", "Estado del parque"], ["alerts", "Alertas abiertas"], ["review", "Pendiente de revisar"],
  ["trends", "Tendencias (90 días)"], ["recent", "Últimas inscripciones"]];
const DEFAULT_ORDER: SectionKey[] = SECTIONS.map(([k]) => k);

interface AlertRow { id: number; title: string; severity: string; opened_at: string }

const SEVERITY: Record<string, string> = { critical: "Críticas", warning: "Avisos", info: "Información" };

function AlertsSection() {
  const counts = useQuery({ queryKey: ["alert-counts"], queryFn: () => api<Record<string, number>>("GET", "/alerts/counts") });
  const top = useQuery({ queryKey: ["alerts-top"], queryFn: () => api<{ items: AlertRow[] }>("GET", "/alerts?state=unresolved&per_page=5") });
  const c = counts.data ?? {};
  return (
    <section className="section">
      <h2>Alertas abiertas</h2>
      <ul className="issues" aria-label="Alertas abiertas" style={{ marginTop: "0.75rem" }}>
        {(["critical", "warning", "info"] as const).map((k) => (
          <li key={k}><Link to={`/alertas?gravedad=${k}`}>
            <strong style={c[k] ? (k === "critical" ? undefined : { color: "var(--warn)" }) : { color: "var(--steel)" }}>{formatNumber(c[k] ?? 0)}</strong>
            <span className="issue-text">{SEVERITY[k]}</span>
          </Link></li>
        ))}
      </ul>
      {top.data && top.data.items.length > 0 && (
        <ul className="plain-list small" style={{ marginTop: "0.75rem" }}>
          {top.data.items.map((a) => <li key={a.id}><Link to={`/alertas/${a.id}`}>{a.title}</Link></li>)}
        </ul>
      )}
    </section>
  );
}

function TrendsSection() {
  const keys = ["devices.online", "devices.offline", "compliance.percent", "patches.security_pending", "alerts.open.critical"];
  const m = useQuery({ queryKey: ["metrics", 90], queryFn: () => api<Record<string, { day: string; value: number }[]>>("GET", `/metrics?keys=${keys.join(",")}&days=90`) });
  if (!m.data) return null;
  return (
    <section className="section">
      <h2>Tendencias (90 días)</h2>
      <div className="trend-grid-3">
        <TrendChart title="Equipos en línea y desconectados" series={[
          { label: "En línea", color: "var(--ok)", points: m.data["devices.online"] },
          { label: "Desconectados", color: "var(--warn)", points: m.data["devices.offline"] }]} />
        <TrendChart title="Cumplimiento (%)" unit=" %" series={[{ label: "Cumplen", color: "var(--accent)", points: m.data["compliance.percent"] }]} />
        <TrendChart title="Parches de seguridad pendientes y alertas críticas" series={[
          { label: "Con parches pendientes", color: "var(--warn)", points: m.data["patches.security_pending"] },
          { label: "Alertas críticas", color: "var(--bad)", points: m.data["alerts.open.critical"] }]} />
      </div>
    </section>
  );
}

function Personalize({ order, onClose }: { order: SectionKey[]; onClose(): void }) {
  const queryClient = useQueryClient();
  const [list, setList] = useState<{ key: SectionKey; on: boolean }[]>(() => {
    const rest = DEFAULT_ORDER.filter((k) => !order.includes(k));
    return [...order.map((k) => ({ key: k, on: true })), ...rest.map((k) => ({ key: k, on: false }))];
  });
  const move = (i: number, d: number) => {
    const n = [...list];
    [n[i], n[i + d]] = [n[i + d], n[i]];
    setList(n);
  };
  async function save(sections: SectionKey[] | null) {
    await api("PUT", "/me/preferences", { summary_sections: sections });
    await queryClient.invalidateQueries({ queryKey: ["preferences"] });
    onClose();
  }
  return (
    <div className="dialog-backdrop">
      <div className="dialog" role="dialog" aria-modal="true" aria-labelledby="personalize-title">
        <h2 id="personalize-title">Personalizar el resumen</h2>
        <ul className="plain-list">
          {list.map((x, i) => (
            <li key={x.key} className="personalize-row">
              <label className="check-field"><input type="checkbox" checked={x.on}
                onChange={(e) => setList(list.map((y) => (y.key === x.key ? { ...y, on: e.target.checked } : y)))} />
                {SECTIONS.find(([k]) => k === x.key)![1]}</label>
              <span>
                <button className="btn btn-small" disabled={i === 0} aria-label={`Subir ${x.key}`} onClick={() => move(i, -1)}>▲</button>{" "}
                <button className="btn btn-small" disabled={i === list.length - 1} aria-label={`Bajar ${x.key}`} onClick={() => move(i, 1)}>▼</button>
              </span>
            </li>
          ))}
        </ul>
        <div className="dialog-actions">
          <button className="btn btn-quiet" onClick={() => void save(null)}>Restablecer</button>
          <button className="btn" onClick={onClose}>Cancelar</button>
          <button className="btn btn-primary" onClick={() => void save(list.filter((x) => x.on).map((x) => x.key))}>Guardar</button>
        </div>
      </div>
    </div>
  );
}

export default function Summary() {
  const { data, error } = useQuery({
    queryKey: ["summary"],
    queryFn: () => api<SummaryData>("GET", "/summary"),
    refetchInterval: 30_000,
  });
  const prefs = useQuery({ queryKey: ["preferences"], queryFn: () => api<{ summary_sections: SectionKey[] }>("GET", "/me/preferences") });
  const [personalizing, setPersonalizing] = useState(false);
  const saved = (prefs.data?.summary_sections ?? []).filter((k) => DEFAULT_ORDER.includes(k));
  const order = saved.length > 0 ? saved : DEFAULT_ORDER;

  return (
    <>
      <div className="page-head">
        <h1>Resumen</h1>
        <div className="page-actions"><button className="btn btn-small" onClick={() => setPersonalizing(true)}>Personalizar</button></div>
      </div>
      {personalizing && <Personalize order={order} onClose={() => setPersonalizing(false)} />}
      {error ? (
        <p className="form-error" role="alert">{error instanceof ApiError ? error.message : "No se ha podido cargar el resumen."}</p>
      ) : !data ? (
        <p className="muted">Cargando…</p>
      ) : data.total === 0 ? (
        <NoEnrollments />
      ) : (
        <SummaryBody s={data} order={order} />
      )}
    </>
  );
}

function SummaryBody({ s, order }: { s: SummaryData; order: SectionKey[] }) {
  const maxOS = Math.max(1, ...s.os_versions.map((o) => o.count));
  const sections: Record<SectionKey, JSX.Element> = {
    status: (
      <>
      <section className="fleet-bar" aria-labelledby="fleet-title">
        <h2 id="fleet-title" className="visually-hidden">Estado del parque</h2>
        <p className="fleet-total">
          <strong>{formatNumber(s.total)}</strong>
          <span>
            equipos gestionados: {formatNumber(s.by_platform.windows ?? 0)} Windows{s.by_platform.android ? ", " : " y "}
            {formatNumber(s.by_platform.linux ?? 0)} Linux{s.by_platform.android ? ` y ${formatNumber(s.by_platform.android)} Android` : ""}
          </span>
        </p>
        <div className="fleet-strip" aria-hidden="true">
          {ORDER.filter((st) => s.by_status[st] > 0).map((st) => (
            <Link key={st} to={`/dispositivos?status=${st}`} tabIndex={-1} className={`seg-${st}`}
              style={{ flexGrow: s.by_status[st] }} title={`${s.by_status[st]} ${statusLabel(st)}`} />
          ))}
        </div>
        <ul className="fleet-legend" aria-label="Estado de los equipos">
          {ORDER.map((st) => (
            <li key={st}>
              <Link to={`/dispositivos?status=${st}`}>
                <strong>{formatNumber(s.by_status[st] ?? 0)}</strong>
                <StatusBadge status={st} />
              </Link>
            </li>
          ))}
        </ul>
      </section>

      </>
    ),
    alerts: <AlertsSection />,
    review: (
      <>
      <div className="two-col section">
        <section>
          <h2 id="issues-title">Pendiente de revisar</h2>
          <ul className="issues" aria-label="Pendiente de revisar" style={{ marginTop: "0.75rem" }}>
            {ISSUES.map((i) => {
              const n = s[i.key];
              const inner = (
                <>
                  <strong>{formatNumber(n)}</strong>
                  <span className="issue-text">{i.label}<span>{i.detail}</span></span>
                </>
              );
              return (
                <li key={i.key}>
                  {n > 0 ? <Link to={`/dispositivos?issue=${i.key}`}>{inner}</Link> : <div className="issue-clear">{inner}</div>}
                </li>
              );
            })}
          </ul>
        </section>

        {(s.security_updates_pending !== undefined || s.deployment_failures !== undefined) && (
          <section>
            <h2>Software y parches</h2>
            <ul className="issues" aria-label="Software y parches" style={{ marginTop: "0.75rem" }}>
              <li><Link to="/parches"><strong style={s.security_updates_pending ? undefined : { color: "var(--steel)" }}>{formatNumber(s.security_updates_pending ?? 0)}</strong>
                <span className="issue-text">Con actualizaciones de seguridad pendientes</span></Link></li>
              <li><Link to="/despliegues"><strong style={s.deployment_failures ? undefined : { color: "var(--steel)" }}>{formatNumber(s.deployment_failures ?? 0)}</strong>
                <span className="issue-text">Con algún despliegue fallido</span></Link></li>
            </ul>
          </section>
        )}

        {s.compliance && (
          <section>
            <h2>Cumplimiento</h2>
            <ul className="issues" aria-label="Cumplimiento" style={{ marginTop: "0.75rem" }}>
              {(["noncompliant", "unknown", "compliant"] as const).map((k) => (
                <li key={k}>
                  <Link to={`/dispositivos?compliance=${k}`}>
                    <strong style={k === "noncompliant" ? undefined : { color: "var(--steel)" }}>{formatNumber(s.compliance![k] ?? 0)}</strong>
                    <span className="issue-text">{{ noncompliant: "No cumplen", unknown: "Desconocido", compliant: "Cumplen" }[k]}</span>
                  </Link>
                </li>
              ))}
            </ul>
          </section>
        )}

        <section>
          <h2>Versiones de sistema</h2>
          <div className="table-wrap" style={{ marginTop: "0.75rem" }}>
            <table>
              <thead>
                <tr><th scope="col">Sistema</th><th scope="col" className="num">Equipos</th></tr>
              </thead>
              <tbody>
                {s.os_versions.map((o) => (
                  <tr key={`${o.platform}-${o.os_name}-${o.os_version}`}>
                    <td>
                      <Link className="row-link" to={`/dispositivos?platform=${o.platform}&os_version=${encodeURIComponent(o.os_version)}`}>
                        {`${o.os_name} ${o.os_version}`.trim()}
                      </Link>
                    </td>
                    <td className="num">
                      <span className="bar-cell">
                        <span className="bar" aria-hidden="true"><i style={{ width: `${(o.count / maxOS) * 100}%` }} /></span>
                        {formatNumber(o.count)}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </div>

      </>
    ),
    trends: <TrendsSection />,
    recent: (
      <>
      <section className="section">
        <h2>Últimas inscripciones</h2>
        <div className="table-wrap">
          <table>
            <thead>
              <tr><th scope="col">Nombre</th><th scope="col">Sistema</th><th scope="col">Estado</th><th scope="col">Inscrito</th></tr>
            </thead>
            <tbody>
              {s.recent_enrollments.map((d) => (
                <tr key={d.id}>
                  <td><Link className="row-link" to={`/dispositivos/${d.id}`}>{d.hostname}</Link></td>
                  <td>{d.os_name} <span className="muted">{d.os_version}</span></td>
                  <td><StatusBadge status={d.status} /></td>
                  <td className="nowrap">{formatDate(d.enrolled_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
      </>
    ),
  };
  return <>{order.map((k) => <div key={k} className="summary-block">{sections[k]}</div>)}</>;
}
