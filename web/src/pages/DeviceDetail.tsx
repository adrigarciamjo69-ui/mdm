import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, query } from "../api/client";
import type { ActivityPoint, Asset, DeviceDetail as Detail, Group, Inventory, Page, SecurityState, Section, Software, Mobile } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import ActivityChart from "../components/ActivityChart";
import ComplianceTab from "../components/ComplianceTab";
import ManagedSoftwareTab from "../components/ManagedSoftwareTab";
import UpdatesTab from "../components/UpdatesTab";
import ActionBar from "../components/ActionBar";
import DeviceCommands from "../components/DeviceCommands";
import ConfirmDialog from "../components/ConfirmDialog";
import Pagination from "../components/Pagination";
import StatusBadge from "../components/StatusBadge";
import Tabs from "../components/Tabs";
import AssetPanel from "../components/AssetPanel";
import { useDebounced } from "../lib/hooks";
import { formatBytes, formatDate, formatDateTime, platformLabel, relativeTime } from "../lib/format";

const TABS = [
  { id: "general", label: "General" },
  { id: "asset", label: "Activo" },
  { id: "compliance", label: "Cumplimiento" },
  { id: "managed", label: "Software gestionado" },
  { id: "updates", label: "Actualizaciones" },
  { id: "hardware", label: "Hardware" },
  { id: "storage", label: "Almacenamiento" },
  { id: "network", label: "Red" },
  { id: "software", label: "Software" },
  { id: "security", label: "Seguridad" },
  { id: "commands", label: "Actividad" },
  { id: "activity", label: "Conexiones" },
];

// En un móvil no hay parches del agente, discos ni conexiones del agente.
const ANDROID_HIDDEN = new Set(["updates", "storage", "activity"]);

const LOW_DISK = 0.1;

export default function DeviceDetail() {
  const { id = "" } = useParams();
  const { can } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [params] = useSearchParams();
  const [tab, setTab] = useState(params.get("tab") === "asset" ? "asset" : "general");
  const [dialog, setDialog] = useState<"revoke" | "delete" | null>(null);

  const device = useQuery({
    queryKey: ["device", id],
    queryFn: () => api<Detail>("GET", `/devices/${id}`),
    refetchInterval: 30_000,
  });

  if (device.error) {
    const gone = device.error instanceof ApiError && (device.error.status === 404 || device.error.status === 400);
    return (
      <div className="notice">
        <p>{gone ? "Este dispositivo ya no existe." : device.error instanceof ApiError ? device.error.message : "No se ha podido cargar el dispositivo."}</p>
        <Link to="/inventario?origin=agent">Volver al inventario</Link>
      </div>
    );
  }
  const d = device.data;
  if (!d) return <p className="muted">Cargando…</p>;

  async function revoke() {
    await api("POST", `/devices/${id}/revoke`);
    setDialog(null);
    await queryClient.invalidateQueries({ queryKey: ["device", id] });
    void queryClient.invalidateQueries({ queryKey: ["devices"] });
  }

  async function remove() {
    await api("DELETE", `/devices/${id}`);
    queryClient.removeQueries({ queryKey: ["device", id] });
    void queryClient.invalidateQueries({ queryKey: ["devices"] });
    void queryClient.invalidateQueries({ queryKey: ["summary"] });
    void queryClient.invalidateQueries({ queryKey: ["assets"] });
    navigate("/inventario?origin=agent", { replace: true });
  }

  const makeModel = [d.manufacturer, d.model].filter(Boolean).join(" ");
  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/inventario">Inventario</Link></p>

      <header className="asset-tag">
        <div>
          <h1>{d.hostname}</h1>
          <p className="asset-model">{makeModel || "Fabricante y modelo desconocidos"}</p>
        </div>
        <dl>
          <dt>Estado</dt>
          <dd><StatusBadge status={d.status} /></dd>
          <dt>Número de serie</dt>
          <dd className="mono">{d.serial || "—"}</dd>
          <dt>Última conexión</dt>
          <dd title={d.last_seen_at ? formatDateTime(d.last_seen_at) : undefined}>{relativeTime(d.last_seen_at)}</dd>
        </dl>
        <div className="asset-strip">
          <span>{platformLabel(d.platform)} {d.arch}</span>
          <span className="barcode" aria-hidden="true" />
          <span title="Identificador del dispositivo">{d.id}</span>
        </div>
      </header>

      {d.status === "revoked" && (
        <p className="banner banner-bad">
          Este equipo está revocado: el agente no puede comunicarse con el servidor. Para volver a gestionarlo, inscríbelo de nuevo con un token.
        </p>
      )}

      {d.status !== "revoked" && <ActionBar target={{ deviceIds: [d.id], label: d.hostname, platform: d.platform }} label="" >{null}</ActionBar>}
      {can("admin") && (
        <div className="page-actions" style={{ marginBottom: "1.25rem" }}>
          {/* Un móvil activo se da de baja con «Borrar el móvil»: revocarlo aquí no lo saca de Google
              y eliminarlo no sirve (la sincronización lo vuelve a crear). */}
          {d.status !== "revoked" && d.platform !== "android" && <button className="btn" onClick={() => setDialog("revoke")}>Revocar</button>}
          {(d.platform !== "android" || d.status === "revoked") && <button className="btn" onClick={() => setDialog("delete")}>Eliminar</button>}
        </div>
      )}

      <Tabs tabs={d.platform === "android" ? TABS.filter((t) => !ANDROID_HIDDEN.has(t.id)) : TABS} active={tab} onChange={setTab} label="Secciones del dispositivo" />
      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === "general" && <General d={d} />}
        {tab === "asset" && <DeviceAsset id={id} />}
        {tab === "compliance" && <ComplianceTab d={d} />}
        {tab === "managed" && <ManagedSoftwareTab id={id} />}
        {tab === "updates" && <UpdatesTab id={id} />}
        {tab === "hardware" && <InventorySection d={d} section="hardware">{(inv) => <Hardware inv={inv} />}</InventorySection>}
        {tab === "storage" && <InventorySection d={d} section="disks">{(inv) => <Storage inv={inv} />}</InventorySection>}
        {tab === "network" && <InventorySection d={d} section="network">{(inv) => <Network inv={inv} />}</InventorySection>}
        {tab === "software" && <SoftwareTab id={id} unavailable={d.inventory?.unavailable.includes("software") ?? false} />}
        {tab === "security" && <InventorySection d={d} section="security">{(inv) => <Security inv={inv} />}</InventorySection>}
        {tab === "activity" && <Activity id={id} />}
        {tab === "commands" && <DeviceCommands id={id} />}
      </div>

      {dialog === "revoke" && (
        <ConfirmDialog
          title="Revocar este dispositivo"
          body="El agente dejará de comunicarse con el servidor hasta que se vuelva a inscribir. El equipo y su inventario se conservan."
          confirmLabel="Revocar"
          danger
          onConfirm={revoke}
          onCancel={() => setDialog(null)}
        />
      )}
      {dialog === "delete" && (
        <ConfirmDialog
          title="Eliminar este dispositivo"
          body="Se borrarán el equipo, su inventario y su actividad. Esta acción no se puede deshacer."
          confirmLabel="Eliminar"
          danger
          requireText={d.hostname}
          onConfirm={remove}
          onCancel={() => setDialog(null)}
        />
      )}
    </>
  );
}

/** Muestra el contenido de una sección del inventario, o explica por qué falta. */
function InventorySection({ d, section, children }: { d: Detail; section: Section; children(inv: Inventory): ReactNode }) {
  if (!d.inventory) {
    return <p className="empty">Este equipo todavía no ha enviado su inventario. Aparecerá aquí tras su próxima conexión.</p>;
  }
  if (d.inventory.unavailable.includes(section)) {
    return <p className="empty">No se pudo recoger este dato en el último inventario.</p>;
  }
  return (
    <>
      {children(d.inventory)}
      {d.inventory_collected_at && (
        <p className="muted small" style={{ marginTop: "1rem" }}>Inventario recogido {relativeTime(d.inventory_collected_at)} ({formatDateTime(d.inventory_collected_at)}).</p>
      )}
    </>
  );
}

function Facts({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <dl className="facts">
      {rows.map(([k, v]) => (
        <div key={k} style={{ display: "contents" }}>
          <dt>{k}</dt>
          <dd>{v === "" || v === null || v === undefined ? <span className="muted">—</span> : v}</dd>
        </div>
      ))}
    </dl>
  );
}

const INTEGRITY: Record<string, string> = {
  SECURE: "Correcta", AT_RISK: "En riesgo", POTENTIALLY_COMPROMISED: "Posiblemente comprometido", POSTURE_UNSPECIFIED: "Sin datos",
};

function MobileFacts({ m }: { m: Mobile }) {
  return (
    <div className="panel">
      <h2>Móvil</h2>
      <Facts
        rows={[
          ["IMEI", m.imei ? <span className="mono">{m.imei}</span> : ""],
          ["Teléfono", m.phone_number],
          ["Operador", m.carrier],
          ["Parche de seguridad", m.security_patch_level],
          ["Integridad (Google)", m.integrity ? <span style={m.integrity === "SECURE" ? undefined : { color: "var(--bad)" }}>{INTEGRITY[m.integrity] ?? m.integrity}</span> : ""],
          ["Bloqueo de pantalla", m.device_secure ? "Sí" : <span style={{ color: "var(--warn)" }}>No</span>],
          ["Opciones de desarrollador", m.developer_options ? "Activadas" : "Desactivadas"],
          ["Política aplicada", m.policy_applied ? "Sí" : "Aún no (el móvil la aplica en su siguiente informe)"],
          ["Último informe a Google", m.last_status_report ? formatDateTime(m.last_status_report) : ""],
        ]}
      />
    </div>
  );
}

function General({ d }: { d: Detail }) {
  const { can } = useAuth();
  const sys = d.inventory && !d.inventory.unavailable.includes("system") ? d.inventory.system : null;
  const mobile = d.inventory?.mobile;
  return (
    <>
      {mobile && <MobileFacts m={mobile} />}
      <div className="panel">
        <h2>Equipo</h2>
        <Facts
          rows={[
            ["Sistema", `${d.os_name} ${d.os_version}`.trim()],
            ...(mobile ? [] : ([
              ["Dominio", sys?.domain],
              ["Sesión iniciada", sys?.logged_in_user],
              ["Encendido desde", sys?.boot_time ? formatDateTime(sys.boot_time) : ""],
              ["Dirección IP", d.last_ip ? <span className="mono">{d.last_ip}</span> : ""],
            ] as [string, ReactNode][])),
            ...(mobile ? [] : [["Versión del agente", d.agent_version] as [string, ReactNode]]),
            ["Inscrito", formatDate(d.enrolled_at)],
          ]}
        />
      </div>
      <div className="panel">
        <h2>Asignación</h2>
        {can("operator") ? (
          <AssignmentForm d={d} />
        ) : (
          <Facts
            rows={[
              ["Grupo", d.group_name],
              ["Usuario asignado", d.assigned_user],
              ["Etiquetas", d.tags.length ? d.tags.map((t) => <span key={t} className="tag">{t}</span>) : ""],
            ]}
          />
        )}
      </div>
    </>
  );
}

const parseTags = (s: string) => s.split(",").map((t) => t.trim().toLowerCase()).filter(Boolean);

function AssignmentForm({ d }: { d: Detail }) {
  const queryClient = useQueryClient();
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });
  const [group, setGroup] = useState(d.group_id ?? "");
  const [tags, setTags] = useState(d.tags.join(", "));
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const changes = useMemo(() => {
    const c: Record<string, unknown> = {};
    if (group !== (d.group_id ?? "")) c.group_id = group || null;
    const t = parseTags(tags);
    if (t.join(",") !== d.tags.join(",")) c.tags = t;
    return c;
  }, [group, tags, d]);
  const dirty = Object.keys(changes).length > 0;

  useEffect(() => {
    if (dirty) setSaved(false);
  }, [dirty]);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      const updated = await api<Detail>("PATCH", `/devices/${d.id}`, changes);
      queryClient.setQueryData(["device", d.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["devices"] });
      setTags(updated.tags.join(", "));
      setGroup(updated.group_id ?? "");
      setSaved(true);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se han podido guardar los cambios.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={save}>
      <div className="edit-grid">
        <label className="field">
          <span>Grupo</span>
          <select value={group} onChange={(e) => setGroup(e.target.value)}>
            <option value="">Sin grupo</option>
            {groups.data?.filter((g) => g.kind !== "dynamic").map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
          </select>
        </label>
        <div className="field">
          <span>Usuario asignado</span>
          <span>{d.assigned_user || <span className="muted">—</span>} <span className="hint">se cambia en la pestaña Activo</span></span>
        </div>
        <label className="field">
          <span>Etiquetas <span className="hint">separadas por comas</span></span>
          <input value={tags} onChange={(e) => setTags(e.target.value)} aria-label="Etiquetas" />
        </label>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      <div style={{ display: "flex", gap: "1rem", alignItems: "center" }}>
        <button className="btn btn-primary" disabled={!dirty || busy}>Guardar cambios</button>
        {saved && !dirty && <span className="form-ok" role="status">Cambios guardados.</span>}
      </div>
    </form>
  );
}

function Hardware({ inv }: { inv: Inventory }) {
  const h = inv.hardware;
  return (
    <div className="panel">
      <Facts
        rows={[
          ["Fabricante", h.manufacturer],
          ["Modelo", h.model],
          ["Número de serie", h.serial ? <span className="mono">{h.serial}</span> : ""],
          ["Procesador", h.cpu_model],
          ["Núcleos", h.cpu_cores ? `${h.cpu_cores} núcleos, ${h.cpu_threads} hilos` : ""],
          ["Memoria", h.memory_bytes ? formatBytes(h.memory_bytes) : ""],
          ["BIOS", [h.bios_vendor, h.bios_version, h.bios_date && `(${h.bios_date})`].filter(Boolean).join(" ")],
        ]}
      />
    </div>
  );
}

function Storage({ inv }: { inv: Inventory }) {
  if (inv.disks.length === 0) return <p className="empty">No se ha encontrado ningún volumen.</p>;
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th scope="col">Volumen</th><th scope="col">Sistema de ficheros</th><th scope="col" className="num">Tamaño</th>
            <th scope="col">Uso</th><th scope="col">Cifrado</th>
          </tr>
        </thead>
        <tbody>
          {inv.disks.map((disk) => {
            const ratio = disk.size_bytes > 0 ? disk.free_bytes / disk.size_bytes : null;
            const low = ratio !== null && ratio < LOW_DISK;
            return (
              <tr key={disk.device + disk.mount}>
                <td>
                  <strong>{disk.mount}</strong>
                  {disk.device !== disk.mount && <div className="muted small mono">{disk.device}</div>}
                </td>
                <td>{disk.filesystem || "—"}</td>
                <td className="num">{disk.size_bytes ? formatBytes(disk.size_bytes) : "—"}</td>
                <td>
                  {ratio === null ? (
                    <span className="muted">—</span>
                  ) : (
                    <div className={low ? "usage low" : "usage"}>
                      <div className="usage-bar" aria-hidden="true"><i style={{ width: `${(1 - ratio) * 100}%` }} /></div>
                      <span className="small">
                        {formatBytes(disk.free_bytes)} libres ({Math.round(ratio * 100)} %){low && <strong>. Poco espacio</strong>}
                      </span>
                    </div>
                  )}
                </td>
                <td>{disk.encrypted ? <span className="state-on">Cifrado</span> : <span className="state-off">Sin cifrar</span>}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function Network({ inv }: { inv: Inventory }) {
  if (inv.network.length === 0) return <p className="empty">No se ha encontrado ninguna interfaz de red activa.</p>;
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr><th scope="col">Interfaz</th><th scope="col">MAC</th><th scope="col">IPv4</th><th scope="col">IPv6</th></tr>
        </thead>
        <tbody>
          {inv.network.map((n) => (
            <tr key={n.name}>
              <td><strong>{n.name}</strong></td>
              <td className="mono">{n.mac || "—"}</td>
              <td className="mono">{n.ipv4?.length ? n.ipv4.join(", ") : "—"}</td>
              <td className="mono small">{n.ipv6?.length ? n.ipv6.join(", ") : "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

const STATE_LABEL: Record<SecurityState, string> = { on: "Activado", off: "Desactivado", partial: "Parcial", unknown: "Desconocido" };
const ENCRYPTION_LABEL: Record<SecurityState, string> = {
  on: "Todos los volúmenes cifrados", off: "Ningún volumen cifrado", partial: "Algunos volúmenes sin cifrar", unknown: "Desconocido",
};

function State({ s, labels = STATE_LABEL }: { s: SecurityState; labels?: Record<SecurityState, string> }) {
  return <span className={`state-${s}`}>{labels[s] ?? s}</span>;
}

function Security({ inv }: { inv: Inventory }) {
  const s = inv.security;
  return (
    <div className="panel">
      <Facts
        rows={[
          ["Cifrado de disco", <State s={s.disk_encryption} labels={ENCRYPTION_LABEL} />],
          ["Cortafuegos", <State s={s.firewall} />],
          ["Antivirus", <>{<State s={s.antivirus} />}{s.antivirus_product && <span className="muted"> ({s.antivirus_product})</span>}</>],
          ["Arranque seguro", <State s={s.secure_boot} />],
        ]}
      />
    </div>
  );
}

function SoftwareTab({ id, unavailable }: { id: string; unavailable: boolean }) {
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const q = useDebounced(search.trim(), 300);
  useEffect(() => setPage(1), [q]);
  const path = `/devices/${id}/software${query({ q, page: page > 1 ? page : null, per_page: 50 })}`;
  const sw = useQuery({ queryKey: ["software", path], queryFn: () => api<Page<Software>>("GET", path), placeholderData: keepPreviousData });

  return (
    <>
      {unavailable && <p className="banner">El último inventario no pudo leer el software instalado. Se muestra la lista anterior.</p>}
      <div className="toolbar">
        <input type="search" aria-label="Buscar programa" placeholder="Buscar programa" value={search} onChange={(e) => setSearch(e.target.value)} />
      </div>
      {sw.error ? (
        <p className="form-error" role="alert">No se ha podido cargar el software.</p>
      ) : !sw.data ? (
        <p className="muted">Cargando…</p>
      ) : sw.data.total === 0 ? (
        <p className="empty">{q ? "Ningún programa coincide con la búsqueda." : "No hay software registrado para este equipo."}</p>
      ) : (
        <>
          <div className="table-wrap">
            <table className={sw.isPlaceholderData ? "loading" : undefined}>
              <thead>
                <tr><th scope="col">Programa</th><th scope="col">Versión</th><th scope="col">Editor</th><th scope="col">Instalado</th></tr>
              </thead>
              <tbody>
                {sw.data.items.map((s, i) => (
                  <tr key={`${s.name}-${s.version}-${i}`}>
                    <td>{s.name}</td>
                    <td className="mono small">{s.version || "—"}</td>
                    <td>{s.publisher || <span className="muted">—</span>}</td>
                    <td className="nowrap">{s.installed_at ? formatDate(s.installed_at) : <span className="muted">—</span>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination page={sw.data.page} perPage={sw.data.per_page} total={sw.data.total} onChange={setPage} />
        </>
      )}
    </>
  );
}

function Activity({ id }: { id: string }) {
  const a = useQuery({ queryKey: ["activity", id], queryFn: () => api<ActivityPoint[]>("GET", `/devices/${id}/activity`) });
  if (a.error) return <p className="form-error" role="alert">No se ha podido cargar la actividad.</p>;
  if (!a.data) return <p className="muted">Cargando…</p>;
  return (
    <div className="panel">
      <ActivityChart points={a.data} />
    </div>
  );
}

function DeviceAsset({ id }: { id: string }) {
  const q = useQuery({ queryKey: ["device-asset", id], queryFn: () => api<Asset>("GET", `/devices/${id}/asset`) });
  if (q.error) return <p className="muted">{q.error instanceof ApiError ? q.error.message : "No se ha podido cargar el activo."}</p>;
  if (!q.data) return <p className="muted">Cargando…</p>;
  return <AssetPanel asset={q.data} />;
}
