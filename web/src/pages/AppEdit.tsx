import { Fragment, useEffect, useState, type FormEvent, type ReactNode } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { App, AppVersion, Detection, Platform } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import ConfirmDialog from "../components/ConfirmDialog";
import DetectionEditor from "../components/DetectionEditor";
import Upload from "../components/Upload";
import { formatBytes, formatDateTime } from "../lib/format";
import { sourceLabel } from "./Software";

const SOURCES: Record<string, [string, string][]> = {
  windows: [["winget", "winget (repositorio de Microsoft)"], ["upload", "Instalador propio (MSI o EXE)"]],
  linux: [["apt", "apt (Debian, Ubuntu)"], ["dnf", "dnf (RHEL, Rocky, Alma, Fedora)"], ["upload", "Instalador propio (DEB o RPM)"]],
  android: [["google_play", "Google Play (gestionada)"]],
};
const TYPES: Record<string, string[]> = { windows: ["msi", "exe"], linux: ["deb", "rpm"], android: [] };
const DEFAULTS: Record<string, [string, string]> = {
  msi: [`msiexec /i "{file}" /qn /norestart`, `msiexec /x "{file}" /qn /norestart`],
  deb: [`apt-get install -y "{file}"`, `apt-get remove -y "$(dpkg-deb -f "{file}" Package)"`],
  rpm: [`dnf install -y "{file}"`, `dnf remove -y "$(rpm -qp --qf '%{NAME}' "{file}")"`],
};

function NewApp() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [publisher, setPublisher] = useState("");
  const [platform, setPlatform] = useState<Platform>("windows");
  const [source, setSource] = useState("winget");
  const [pkg, setPkg] = useState("");
  const [error, setError] = useState("");
  const pkgLabel = source === "winget" ? "Identificador de winget" : source === "google_play" ? "Nombre del paquete de Android" : "Nombre del paquete";

  async function create(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      const a = await api<App>("POST", "/apps", { name: name.trim(), publisher: publisher.trim(), description: "", platform, source, package_id: pkg.trim() });
      void queryClient.invalidateQueries({ queryKey: ["apps"] });
      navigate(`/software/${a.id}`, { replace: true });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido crear la aplicación.");
    }
  }

  return (
    <form onSubmit={create} noValidate>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/software">Software</Link></p>
      <div className="page-head"><h1>Añadir aplicación</h1></div>
      <div className="panel">
        <div className="edit-grid">
          <label className="field"><span>Nombre</span><input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} /></label>
          <label className="field"><span>Fabricante <span className="hint">opcional</span></span><input value={publisher} onChange={(e) => setPublisher(e.target.value)} /></label>
          <label className="field">
            <span>Plataforma</span>
            <select value={platform} onChange={(e) => { setPlatform(e.target.value as Platform); setSource(SOURCES[e.target.value][0][0]); }}>
              <option value="windows">Windows</option><option value="linux">Linux</option><option value="android">Android</option>
            </select>
          </label>
          <label className="field">
            <span>Origen</span>
            <select value={source} onChange={(e) => setSource(e.target.value)}>
              {SOURCES[platform].map(([v, l]) => <option key={v} value={v}>{l}</option>)}
            </select>
          </label>
          {source !== "upload" && (
            <label className="field">
              <span>{pkgLabel}</span>
              <input value={pkg} onChange={(e) => setPkg(e.target.value)} placeholder={source === "winget" ? "Mozilla.Firefox" : source === "google_play" ? "com.microsoft.teams" : "nginx"} className="mono" />
            </label>
          )}
        </div>
        {source === "upload" && <p className="muted small">Después de crearla podrás subir el instalador y añadir sus versiones.</p>}
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      <button className="btn btn-primary" style={{ marginTop: "1rem" }} disabled={!name.trim()}>Crear aplicación</button>
    </form>
  );
}

function VersionForm({ app, onDone }: { app: App; onDone(): void }) {
  const [version, setVersion] = useState("");
  const [type, setType] = useState(TYPES[app.platform][0]);
  const [sha, setSha] = useState("");
  const [install, setInstall] = useState("");
  const [uninstall, setUninstall] = useState("");
  const [codes, setCodes] = useState("0, 3010, 1641");
  const [detection, setDetection] = useState<Detection>({ type: "inventory", name_contains: app.name });
  const [error, setError] = useState("");

  async function save(e: FormEvent) {
    e.preventDefault();
    setError("");
    if (type === "exe" && (!install.trim() || !uninstall.trim())) {
      setError("Un instalador EXE necesita las órdenes de instalación y desinstalación silenciosas.");
      return;
    }
    const det = detection.type === "inventory" && !detection.min_version ? { ...detection, min_version: version.trim() } : detection;
    try {
      await api("POST", `/apps/${app.id}/versions`, {
        version: version.trim(), installer_type: type, file_sha256: sha.trim(), install_command: install.trim(), uninstall_command: uninstall.trim(),
        success_codes: codes.split(",").map((c) => Number(c.trim())).filter((n) => Number.isInteger(n)), detection: det,
      });
      onDone();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido guardar la versión.");
    }
  }

  const def = DEFAULTS[type];
  return (
    <form className="panel" onSubmit={save} noValidate style={{ marginTop: "1rem" }}>
      <h2>Nueva versión</h2>
      <Upload onUploaded={(f) => { setSha(f.sha256); const m = f.original_name.match(/\.(msi|exe|deb|rpm)$/i); if (m) setType(m[1].toLowerCase()); }} />
      <div className="edit-grid">
        <label className="field"><span>Versión</span><input value={version} onChange={(e) => setVersion(e.target.value)} maxLength={50} /></label>
        <label className="field">
          <span>Tipo de instalador</span>
          <select value={type} onChange={(e) => setType(e.target.value)}>{TYPES[app.platform].map((t) => <option key={t} value={t}>{t.toUpperCase()}</option>)}</select>
        </label>
        <label className="field"><span>SHA-256 del fichero</span><input className="mono" value={sha} onChange={(e) => setSha(e.target.value)} /></label>
        <label className="field"><span>Códigos de éxito</span><input value={codes} onChange={(e) => setCodes(e.target.value)} /></label>
      </div>
      <label className="field">
        <span>Orden de instalación {def && <span className="hint">por defecto: {def[0]}</span>}</span>
        <input className="mono" value={install} onChange={(e) => setInstall(e.target.value)} placeholder={def?.[0] ?? '"{file}" /S'} />
      </label>
      <label className="field">
        <span>Orden de desinstalación {def && <span className="hint">por defecto: {def[1]}</span>}</span>
        <input className="mono" value={uninstall} onChange={(e) => setUninstall(e.target.value)} placeholder={def?.[1] ?? ""} />
      </label>
      <p className="muted small">{`{file}`} se sustituye por la ruta del instalador en el equipo. Las órdenes se ejecutan como administrador del sistema con {app.platform === "windows" ? "cmd" : "sh"}.</p>
      <DetectionEditor value={detection} onChange={setDetection} platform={app.platform} />
      {error && <p className="form-error" role="alert">{error}</p>}
      <div style={{ display: "flex", gap: "0.5rem" }}>
        <button className="btn btn-primary">Guardar versión</button>
        <button type="button" className="btn" onClick={onDone}>Cancelar</button>
      </div>
    </form>
  );
}

const DETECTION_LABEL: Record<Detection["type"], string> = {
  inventory: "Software instalado (inventario)", file: "Existe un fichero", registry: "Valor del registro", script: "Script",
};

/** Ajustes guardados de una versión, solo lectura. */
function VersionSettings({ v }: { v: AppVersion }) {
  const d = v.detection;
  const mono = (x?: string) => (x ? <span className="mono" style={{ whiteSpace: "pre-wrap" }}>{x}</span> : <span className="muted">—</span>);
  const rows: [string, ReactNode][] = [
    ["Tipo de instalador", (v.installer_type ?? "").toUpperCase() || "—"],
    ["Fichero", <>{v.file_name || "—"} · {formatBytes(v.file_size ?? 0)}</>],
    ["SHA-256", mono(v.file_sha256)],
    ["Instalación", mono(v.install_command)],
    ["Desinstalación", mono(v.uninstall_command)],
    ["Códigos de éxito", v.success_codes?.length ? v.success_codes.join(", ") : <span className="muted">—</span>],
    ["Detección", DETECTION_LABEL[d?.type] ?? d?.type ?? "—"],
  ];
  if (d?.type === "inventory") rows.push(["Nombre contiene", mono(d.name_contains)], ["Versión mínima", mono(d.min_version)]);
  if (d?.type === "file") rows.push(["Ruta", mono(d.path)], ["Versión mínima", mono(d.min_version)]);
  if (d?.type === "registry") rows.push(["Clave", mono(d.registry_key)], ["Valor", mono(d.registry_value)], ["Versión mínima", mono(d.min_version)]);
  if (d?.type === "script") rows.push(["Intérprete", mono(d.interpreter)], ["Script", mono(d.script)]);
  return (
    <dl className="facts small" style={{ padding: "0.5rem 0" }}>
      {rows.map(([k, val]) => <div key={k} style={{ display: "contents" }}><dt>{k}</dt><dd>{val}</dd></div>)}
    </dl>
  );
}

export default function AppEdit() {
  const { id = "" } = useParams();
  const isNew = id === "nueva";
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const app = useQuery({ queryKey: ["app", id], queryFn: () => api<App>("GET", `/apps/${id}`), enabled: !isNew });
  const [adding, setAdding] = useState(false);
  const [archiving, setArchiving] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  useEffect(() => setAdding(false), [id]);

  if (isNew) return <NewApp />;
  if (app.error) return <div className="notice"><p>Esta aplicación no existe.</p><Link to="/software">Volver a software</Link></div>;
  if (!app.data) return <p className="muted">Cargando…</p>;
  const a = app.data;
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ["app", id] });

  async function setCurrent(vid: string) {
    await api("POST", `/apps/${a.id}/versions/${vid}/current`);
    refresh();
  }

  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/software">Software</Link></p>
      <div className="page-head">
        <div>
          <h1>{a.name}</h1>
          <p className="muted">{a.platform === "windows" ? "Windows" : "Linux"} · {sourceLabel(a)}{a.archived ? " · archivada" : ""}</p>
        </div>
        {can("admin") && !a.archived && (
          <div className="page-actions">
            <Link className="btn" to={`/despliegues/nuevo?app=${a.id}`}>Desplegar</Link>
            <button className="btn" onClick={() => setArchiving(true)}>Archivar</button>
          </div>
        )}
      </div>
      {a.source === "upload" ? (
        <section className="section">
          <h2>Versiones</h2>
          {a.versions.length === 0 ? <p className="muted">Aún no hay versiones: sube el instalador para poder desplegarla.</p> : (
            <div className="table-wrap">
              <table>
                <thead><tr><th scope="col">Versión</th><th scope="col">Fichero</th><th scope="col">Instalación</th><th scope="col">Añadida</th><th scope="col"><span className="visually-hidden">Vigente</span></th></tr></thead>
                <tbody>
                  {a.versions.map((v) => (
                    <Fragment key={v.id}>
                    <tr>
                      <td><strong>{v.version}</strong>{v.is_current && <> <span className="tag">Vigente</span></>}</td>
                      <td className="small">{v.file_name || v.file_sha256?.slice(0, 12)} · {formatBytes(v.file_size ?? 0)}</td>
                      <td className="mono small">{v.install_command}</td>
                      <td className="nowrap">{v.created_at ? formatDateTime(v.created_at) : ""}</td>
                      <td className="num nowrap">
                        <button className="btn btn-small btn-quiet" aria-expanded={open === v.id}
                          onClick={() => setOpen(open === v.id ? null : v.id)}>{open === v.id ? "Ocultar ajustes" : "Ver ajustes"}</button>{" "}
                        {!v.is_current && can("admin") && <button className="btn btn-small" onClick={() => void setCurrent(v.id)}>Marcar como vigente</button>}
                      </td>
                    </tr>
                    {open === v.id && <tr><td colSpan={5}><VersionSettings v={v} /></td></tr>}
                    </Fragment>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {can("admin") && !a.archived && (adding ? <VersionForm app={a} onDone={() => { setAdding(false); refresh(); }} /> :
            <button className="btn" style={{ marginTop: "1rem" }} onClick={() => setAdding(true)}>Añadir versión</button>)}
        </section>
      ) : (
        <p className="muted">Se instala desde el repositorio de {a.source}: no tiene versiones propias. En el despliegue puedes pedir que se mantenga actualizada.</p>
      )}
      {archiving && (
        <ConfirmDialog title={`Archivar «${a.name}»`} body="No se podrá desplegar más. Los despliegues existentes dejan de aplicarse a equipos nuevos."
          confirmLabel="Archivar" onConfirm={async () => { await api("POST", `/apps/${a.id}/archive`); navigate("/software", { replace: true }); }}
          onCancel={() => setArchiving(false)} />
      )}
    </>
  );
}
