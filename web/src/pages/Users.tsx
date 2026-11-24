import { useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, query } from "../api/client";
import type { Asset, Delivery, DirectoryStatus, DirectoryUser, License, Page } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import Pagination from "../components/Pagination";
import EmptyState from "../components/EmptyState";
import { useDebounced } from "../lib/hooks";
import { formatDate, relativeTime } from "../lib/format";
import { ASSET_STATUS_CLASS, ASSET_STATUS_LABEL, LICENSE_STATUS_LABEL, PERSON_STATUS_LABEL } from "../lib/inventory";

function personStatusClass(s: string) {
  return s === "active" ? "status status-online" : s === "disabled" ? "status status-offline" : "status status-revoked";
}

export default function Users() {
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const [search, setSearch] = useState(params.get("q") ?? "");
  const debounced = useDebounced(search.trim(), 300);
  const [syncMsg, setSyncMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [syncing, setSyncing] = useState(false);
  const statusF = params.get("status") ?? "";
  const dept = params.get("department") ?? "";
  const page = Math.max(1, Number(params.get("page")) || 1);

  const set = (k: string, v: string) => setParams((prev) => {
    const n = new URLSearchParams(prev);
    if (v) n.set(k, v);
    else n.delete(k);
    if (k !== "page") n.delete("page");
    return n;
  }, { replace: true });
  useEffect(() => {
    if (debounced !== (params.get("q") ?? "")) set("q", debounced);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debounced]);

  const q = query({ q: params.get("q"), status: statusF, department: dept, page: page > 1 ? page : null });
  const users = useQuery({ queryKey: ["directory-users", q], queryFn: () => api<Page<DirectoryUser>>("GET", "/directory/users" + q), placeholderData: keepPreviousData });
  const st = useQuery({ queryKey: ["directory-status"], queryFn: () => api<DirectoryStatus>("GET", "/directory/status") });
  const deps = useQuery({ queryKey: ["directory-departments"], queryFn: () => api<string[]>("GET", "/directory/departments") });

  async function sync() {
    setSyncMsg(null);
    setSyncing(true);
    try {
      const r = await api<DirectoryStatus>("POST", "/directory/sync");
      queryClient.setQueryData(["directory-status"], r);
      void queryClient.invalidateQueries({ queryKey: ["directory-users"] });
      setSyncMsg(r.last_error ? { ok: false, text: r.last_error } : { ok: true, text: `Sincronizado: ${r.last_count} personas.` });
    } catch (err) {
      setSyncMsg({ ok: false, text: err instanceof ApiError ? err.message : "No se ha podido sincronizar." });
    } finally {
      setSyncing(false);
    }
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Usuarios</h1>
          <p className="muted">Personas de la empresa sincronizadas desde Entra ID (Microsoft 365), con el material que tiene cada una.</p>
        </div>
        <div className="page-actions">
          {can("operator") && <button className="btn" onClick={() => void sync()} disabled={syncing}>{syncing ? "Sincronizando…" : "Sincronizar ahora"}</button>}
          {can("admin") && <Link className="btn btn-quiet" to="/ajustes/inventario">Configurar Entra ID</Link>}
        </div>
      </div>
      {st.data && !st.data.configured && (
        <p className="banner">Entra ID aún no está conectado. {can("admin") ? <Link to="/ajustes/inventario">Configúralo en Ajustes del inventario</Link> : "Un administrador debe configurarlo."} para que aparezcan las personas.</p>
      )}
      {st.data?.configured && (
        <p className="muted small">
          {st.data.last_success_at ? <>Última sincronización correcta {relativeTime(st.data.last_success_at)}: {st.data.last_count} personas. </> : "Aún no se ha sincronizado. "}
          Se sincroniza cada hora.
        </p>
      )}
      {st.data?.last_error && <p className="banner banner-bad">La última sincronización falló: {st.data.last_error}</p>}
      {syncMsg && <p className={syncMsg.ok ? "form-ok" : "form-error"} role={syncMsg.ok ? "status" : "alert"}>{syncMsg.text}</p>}

      <div className="toolbar" role="search">
        <input type="search" aria-label="Buscar" placeholder="Buscar por nombre, correo o n.º de empleado" value={search} onChange={(e) => setSearch(e.target.value)} />
        <select aria-label="Estado" value={statusF === "leavers" ? "" : statusF} onChange={(e) => set("status", e.target.value)}>
          <option value="">Todas</option>
          <option value="active">Activas</option>
          <option value="disabled">Deshabilitadas en Entra</option>
          <option value="deleted">Eliminadas de Entra</option>
        </select>
        <select aria-label="Departamento" value={dept} onChange={(e) => set("department", e.target.value)}>
          <option value="">Todos los departamentos</option>
          {deps.data?.map((d) => <option key={d} value={d}>{d}</option>)}
        </select>
        <button className={statusF === "leavers" ? "btn btn-primary" : "btn"} aria-pressed={statusF === "leavers"}
          onClick={() => set("status", statusF === "leavers" ? "" : "leavers")}>Bajas con material</button>
      </div>

      {users.error ? <p className="form-error" role="alert">{users.error instanceof ApiError ? users.error.message : "No se ha podido cargar la lista."}</p>
        : !users.data ? <p className="muted">Cargando…</p>
          : users.data.total === 0 ? <EmptyState message={statusF === "leavers" ? "Nadie dado de baja tiene material asignado." : "No hay personas que coincidan."} />
            : (
              <>
                <div className="table-wrap">
                  <table>
                    <caption className="visually-hidden">Usuarios</caption>
                    <thead><tr><th scope="col">Nombre</th><th scope="col">Correo</th><th scope="col">Departamento</th><th scope="col">Puesto</th><th scope="col">Estado</th><th scope="col" className="num">Activos</th></tr></thead>
                    <tbody>
                      {users.data.items.map((u) => (
                        <tr key={u.id}>
                          <td><Link className="row-link" to={`/usuarios/${u.id}`}>{u.display_name}</Link></td>
                          <td>{u.email}</td>
                          <td>{u.department || <span className="muted">—</span>}</td>
                          <td>{u.job_title || <span className="muted">—</span>}</td>
                          <td><span className={personStatusClass(u.status)}>{PERSON_STATUS_LABEL[u.status]}</span></td>
                          <td className="num">{u.asset_count > 0 ? <Link to={`/inventario?owner_id=${u.id}`}>{u.asset_count}</Link> : <span className="muted">0</span>}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <Pagination page={users.data.page} perPage={users.data.per_page} total={users.data.total} onChange={(p) => set("page", p > 1 ? String(p) : "")} />
              </>
            )}
    </>
  );
}

type UserDetail = DirectoryUser & { assets: Asset[]; licenses: License[]; deliveries: Delivery[] };

export function UserDetailPage() {
  const { id = "" } = useParams();
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const q = useQuery({ queryKey: ["directory-user", id], queryFn: () => api<UserDetail>("GET", `/directory/users/${id}`) });
  const [notes, setNotes] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  if (q.error) return <p className="form-error" role="alert">{q.error instanceof ApiError ? q.error.message : "No se ha podido cargar la persona."}</p>;
  const u = q.data;
  if (!u) return <p className="muted">Cargando…</p>;

  async function saveNotes() {
    await api("PATCH", `/directory/users/${id}`, { notes: notes ?? "" });
    setSaved(true);
    void queryClient.invalidateQueries({ queryKey: ["directory-user", id] });
  }

  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/usuarios">Usuarios</Link></p>
      <div className="page-head">
        <div>
          <h1>{u.display_name}</h1>
          <p className="muted">{[u.job_title, u.department, u.office].filter(Boolean).join(" · ")}</p>
        </div>
        {can("operator") && (
          <div className="page-actions">
            <Link className="btn btn-primary" to={`/actas/nueva?tipo=entrega&persona=${u.id}`}>Nueva acta de entrega</Link>
            <Link className="btn" to={`/actas/nueva?tipo=devolucion&persona=${u.id}`}>Nueva acta de devolución</Link>
          </div>
        )}
      </div>
      {u.status !== "active" && (
        <p className="banner banner-bad">{PERSON_STATUS_LABEL[u.status]}{u.assets.length > 0 ? `: tiene ${u.assets.length} ${u.assets.length === 1 ? "activo" : "activos"} por recoger.` : "."}</p>
      )}
      <div className="panel">
        <dl className="facts">
          {([["Correo", u.email], ["UPN", u.upn], ["N.º de empleado", u.employee_id], ["Teléfono", u.phone], ["Oficina", u.office],
            ["Sincronizado", formatDate(u.synced_at)]] as [string, string][]).map(([k, v]) => (
            <div key={k}><dt>{k}</dt><dd>{v || <span className="muted">—</span>}</dd></div>
          ))}
        </dl>
      </div>

      <section className="panel">
        <h2>Activos ({u.assets.length})</h2>
        {u.assets.length === 0 ? <p className="muted">No tiene activos asignados.</p> : (
          <div className="table-wrap"><table>
            <thead><tr><th scope="col">Activo</th><th scope="col">Categoría</th><th scope="col">Serie</th><th scope="col">Estado</th></tr></thead>
            <tbody>{u.assets.map((a) => (
              <tr key={a.id}>
                <td><Link to={a.device_id ? `/dispositivos/${a.device_id}` : `/inventario/${a.id}`}>{a.name}</Link></td>
                <td>{a.category}</td><td className="mono">{a.serial || "—"}</td>
                <td><span className={ASSET_STATUS_CLASS[a.status]}>{ASSET_STATUS_LABEL[a.status]}</span></td>
              </tr>))}
            </tbody>
          </table></div>
        )}
      </section>

      <section className="panel">
        <h2>Licencias ({u.licenses.length})</h2>
        {u.licenses.length === 0 ? <p className="muted">Sin licencias asignadas a esta persona.</p> : (
          <ul className="plain-list">{u.licenses.map((l) => <li key={l.id}><Link to={`/licencias/${l.id}`}>{l.name}</Link> <span className="muted small">{LICENSE_STATUS_LABEL[l.effective_status]}</span></li>)}</ul>
        )}
      </section>

      <section className="panel">
        <h2>Actas ({u.deliveries.length})</h2>
        {u.deliveries.length === 0 ? <p className="muted">Sin actas.</p> : (
          <ul className="plain-list">{u.deliveries.map((d) => (
            <li key={d.id}><Link to={`/actas/${d.id}`}>{d.number || "Borrador"}</Link> <span className="muted small">{d.type === "entrega" ? "Entrega" : "Devolución"} · {formatDate(d.date)}{d.status === "anulada" ? " · anulada" : ""}</span></li>
          ))}</ul>
        )}
      </section>

      <section className="panel">
        <h2>Notas</h2>
        {can("operator") ? (
          <>
            <textarea rows={3} style={{ width: "100%" }} aria-label="Notas" value={notes ?? u.notes} onChange={(e) => { setNotes(e.target.value); setSaved(false); }} />
            <button className="btn btn-small" onClick={() => void saveNotes()} disabled={notes === null || notes === u.notes}>Guardar notas</button>
            {saved && <span className="form-ok" role="status"> Guardadas.</span>}
          </>
        ) : <p>{u.notes || <span className="muted">—</span>}</p>}
      </section>
    </>
  );
}
