import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { App, Group } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import ConfirmDialog from "../components/ConfirmDialog";
import { AppStateBadge, type DeploymentRow, type DeviceDeploymentRow } from "../lib/deployments";

interface Assign { group_id: string | null; exclude: boolean }

function Status({ id }: { id: string }) {
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const st = useQuery({
    queryKey: ["deployment-status", id],
    queryFn: () => api<{ counts: Record<string, number>; devices: DeviceDeploymentRow[] }>("GET", `/deployments/${id}/status`),
    refetchInterval: 30_000,
  });
  async function retry(deviceId?: string) {
    await api("POST", `/deployments/${id}/retry`, deviceId ? { device_id: deviceId } : {});
    void queryClient.invalidateQueries({ queryKey: ["deployment-status", id] });
  }
  if (!st.data) return <p className="muted">Cargando…</p>;
  // Primero lo que requiere atención: fallidos, luego en curso, luego terminados.
  const order: Record<string, number> = { failed: 0, downloading: 1, installing: 1, pending: 2, waiting_window: 2, not_applicable: 3, installed: 4, absent: 4 };
  const devices = [...st.data.devices].sort((a, b) => (order[a.state] ?? 5) - (order[b.state] ?? 5) || a.hostname.localeCompare(b.hostname));
  const failed = st.data.counts.failed ?? 0;
  return (
    <section className="section">
      <div className="page-head" style={{ marginBottom: "0.75rem" }}>
        <h2>Estado en los equipos ({st.data.devices.length})</h2>
        {failed > 0 && can("operator") && <button className="btn" onClick={() => void retry()}>Reintentar los fallidos</button>}
      </div>
      {st.data.devices.length === 0 ? <p className="muted">Ningún equipo recibe este despliegue: revisa la asignación.</p> : (
        <div className="table-wrap">
          <table>
            <thead><tr><th scope="col">Equipo</th><th scope="col">Estado</th><th scope="col">Detalle</th><th scope="col"><span className="visually-hidden">Acciones</span></th></tr></thead>
            <tbody>
              {devices.map((d) => (
                <tr key={d.device_id}>
                  <td><Link className="row-link" to={`/dispositivos/${d.device_id}`}>{d.hostname}</Link></td>
                  <td className="nowrap"><AppStateBadge state={d.state} />{d.attempts > 0 && <div className="muted small">{d.attempts} intentos</div>}</td>
                  <td className="small" style={{ maxWidth: "32rem", overflowWrap: "anywhere" }}>{d.message}</td>
                  <td className="num">{d.state === "failed" && can("operator") && <button className="btn btn-small" onClick={() => void retry(d.device_id)}>Reintentar</button>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

export default function DeploymentEdit() {
  const { id = "" } = useParams();
  const isNew = id === "nuevo";
  const [params] = useSearchParams();
  const readOnly = !useAuth().can("admin");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const apps = useQuery({ queryKey: ["apps"], queryFn: () => api<App[]>("GET", "/apps") });
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });
  const existing = useQuery({ queryKey: ["deployment", id], queryFn: () => api<DeploymentRow>("GET", `/deployments/${id}`), enabled: !isNew });

  const [appId, setAppId] = useState(params.get("app") ?? "");
  const [intent, setIntent] = useState<"required" | "uninstall" | "available">("required");
  const [keep, setKeep] = useState(false);
  const [startAt, setStartAt] = useState("");
  const [respectWindow, setRespectWindow] = useState(false);
  const [reboot, setReboot] = useState<"never" | "notify" | "in_window">("never");
  const [assign, setAssign] = useState<Assign[]>([]);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [deleting, setDeleting] = useState(false);

  useEffect(() => {
    const d = existing.data;
    if (!d) return;
    setAppId(d.app_id); setIntent(d.intent); setKeep(d.keep_updated); setRespectWindow(d.respect_window); setReboot(d.reboot);
    setStartAt(d.start_at ? d.start_at.slice(0, 16) : "");
    setAssign(d.assignments.map((a) => ({ group_id: a.group_id, exclude: a.exclude })));
  }, [existing.data]);

  const app = apps.data?.find((a) => a.id === appId);
  const packaged = app ? app.source !== "upload" && app.source !== "google_play" : false;
  const groupName = (gid: string | null) => (gid === null ? "Todos los equipos" : groups.data?.find((g) => g.id === gid)?.name ?? "Grupo eliminado");
  const assignable = (groups.data ?? []).filter((g) => !assign.some((a) => a.group_id === g.id));

  async function save(e: FormEvent) {
    e.preventDefault();
    setError(""); setSaved(false);
    if (!appId) { setError("Elige la aplicación."); return; }
    const body = { app_id: appId, intent, keep_updated: packaged && intent === "required" && keep, start_at: startAt ? new Date(startAt).toISOString() : null,
      respect_window: respectWindow, reboot };
    try {
      const d = isNew ? await api<DeploymentRow>("POST", "/deployments", body) : await api<DeploymentRow>("PATCH", `/deployments/${id}`, body);
      await api("PUT", `/deployments/${d.id}/assignments`, { assignments: assign });
      void queryClient.invalidateQueries({ queryKey: ["deployments"] });
      void queryClient.invalidateQueries({ queryKey: ["deployment-status", d.id] });
      setSaved(true);
      if (isNew) navigate(`/despliegues/${d.id}`, { replace: true });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido guardar el despliegue.");
    }
  }

  if (!isNew && existing.error) return <div className="notice"><p>Este despliegue no existe.</p><Link to="/despliegues">Volver a despliegues</Link></div>;

  return (
    <>
      <form onSubmit={save} noValidate>
        <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/despliegues">Despliegues</Link></p>
        <div className="page-head">
          <h1>{isNew ? "Nuevo despliegue" : existing.data?.app_name ?? "Despliegue"}</h1>
          {!readOnly && !isNew && <div className="page-actions"><button type="button" className="btn" onClick={() => setDeleting(true)}>Eliminar</button></div>}
        </div>
        <fieldset className="panel" disabled={readOnly} style={{ border: 0 }}>
          <div className="edit-grid">
            <label className="field">
              <span>Aplicación</span>
              <select value={appId} disabled={!isNew} onChange={(e) => setAppId(e.target.value)}>
                <option value="">Elige…</option>
                {apps.data?.filter((a) => !a.archived).map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
              </select>
            </label>
            <label className="field">
              <span>Acción</span>
              <select value={intent} onChange={(e) => setIntent(e.target.value as "required" | "uninstall" | "available")}>
                <option value="required">Instalar y mantener instalada</option>
                {app?.source === "google_play" && <option value="available">Disponible (el usuario decide si la instala)</option>}
                <option value="uninstall">Desinstalar</option>
              </select>
            </label>
            <label className="field">
              <span>A partir de <span className="hint">opcional</span></span>
              <input type="datetime-local" value={startAt} onChange={(e) => setStartAt(e.target.value)} />
            </label>
            <label className="field">
              <span>Reinicio</span>
              <select value={reboot} onChange={(e) => setReboot(e.target.value as typeof reboot)}>
                <option value="never">Nunca</option>
                <option value="notify">Avisar y reiniciar a los 10 min</option>
                <option value="in_window">Dentro de la ventana</option>
              </select>
            </label>
          </div>
          {app && app.source === "upload" && app.current_version && <p className="muted small">Se instala la versión vigente: {app.current_version.version}.</p>}
          {app && app.source === "upload" && !app.current_version && <p className="form-error">La aplicación no tiene ninguna versión: súbela antes de desplegarla.</p>}
          {packaged && intent === "required" && (
            <label className="check-field"><input type="checkbox" checked={keep} onChange={(e) => setKeep(e.target.checked)} />Mantener actualizada (comprueba si hay versión nueva una vez al día)</label>
          )}
          <label className="check-field"><input type="checkbox" checked={respectWindow} onChange={(e) => setRespectWindow(e.target.checked)} />Solo dentro de la ventana de mantenimiento (la de la política de parches del equipo)</label>
        </fieldset>

        <section className="section">
          <h2>Asignación</h2>
          <ul className="assign-list">
            {assign.map((a) => (
              <li key={`${a.group_id}-${a.exclude}`}>
                <span className={a.exclude ? "tag tag-exclude" : "tag"}>{a.exclude ? "Excepto " : ""}{groupName(a.group_id)}</span>
                {!readOnly && <button type="button" className="btn btn-quiet" onClick={() => setAssign(assign.filter((x) => x !== a))}>Quitar</button>}
              </li>
            ))}
            {assign.length === 0 && <li className="muted">Sin asignar: no se aplica a ningún equipo.</li>}
          </ul>
          {!readOnly && (
            <div className="edit-grid">
              <label className="field">
                <span>Incluir grupo</span>
                <select value="" onChange={(e) => e.target.value && setAssign([...assign, { group_id: e.target.value === "*" ? null : e.target.value, exclude: false }])}>
                  <option value="">Elige…</option>
                  {!assign.some((a) => a.group_id === null) && <option value="*">Todos los equipos</option>}
                  {assignable.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
                </select>
              </label>
              <label className="field">
                <span>Excluir grupo</span>
                <select value="" onChange={(e) => e.target.value && setAssign([...assign, { group_id: e.target.value, exclude: true }])}>
                  <option value="">Elige…</option>
                  {assignable.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
                </select>
              </label>
            </div>
          )}
        </section>
        {error && <p className="form-error" role="alert">{error}</p>}
        {!readOnly && (
          <div style={{ display: "flex", gap: "1rem", alignItems: "center", marginTop: "1rem" }}>
            <button className="btn btn-primary">Guardar despliegue</button>
            {saved && <span className="form-ok" role="status">Despliegue guardado.</span>}
          </div>
        )}
      </form>
      {!isNew && <Status id={id} />}
      {deleting && (
        <ConfirmDialog title="Eliminar el despliegue" body="Los equipos dejarán de comprobarlo. Lo que ya se instaló o desinstaló no se deshace."
          confirmLabel="Eliminar" danger onConfirm={async () => { await api("DELETE", `/deployments/${id}`); navigate("/despliegues", { replace: true }); }}
          onCancel={() => setDeleting(false)} />
      )}
    </>
  );
}
