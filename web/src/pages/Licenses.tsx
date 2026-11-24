import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, query } from "../api/client";
import type { Asset, License, LicenseStatus, LicenseType, Page, PersonRef } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import Pagination from "../components/Pagination";
import EmptyState from "../components/EmptyState";
import PersonPicker from "../components/PersonPicker";
import ConfirmDialog from "../components/ConfirmDialog";
import { useDebounced } from "../lib/hooks";
import { formatDate } from "../lib/format";
import { formatPrice, LICENSE_STATUS_LABEL, LICENSE_TYPE_LABEL } from "../lib/inventory";

const statusClass = (s: LicenseStatus) => (s === "activa" ? "status status-online" : s === "expirada" ? "status status-offline" : "status status-inactive");

export default function Licenses() {
  const { can } = useAuth();
  const [params, setParams] = useSearchParams();
  const [search, setSearch] = useState(params.get("q") ?? "");
  const debounced = useDebounced(search.trim(), 300);
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
  const q = query({ q: params.get("q"), license_type: params.get("license_type"), status: params.get("status"), expiring: params.get("expiring"), page: page > 1 ? page : null });
  const list = useQuery({ queryKey: ["licenses", q], queryFn: () => api<Page<License>>("GET", "/licenses" + q), placeholderData: keepPreviousData });

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Licencias</h1>
          <p className="muted">Licencias de software registradas a mano, con sus puestos y las instalaciones que detectan los agentes.</p>
        </div>
        {can("operator") && <div className="page-actions"><Link className="btn btn-primary" to="/licencias/nueva">Nueva licencia</Link></div>}
      </div>
      <div className="toolbar" role="search">
        <input type="search" aria-label="Buscar" placeholder="Buscar por nombre, fabricante o pedido" value={search} onChange={(e) => setSearch(e.target.value)} />
        <select aria-label="Tipo" value={params.get("license_type") ?? ""} onChange={(e) => set("license_type", e.target.value)}>
          <option value="">Todos los tipos</option>
          {(Object.keys(LICENSE_TYPE_LABEL) as LicenseType[]).map((t) => <option key={t} value={t}>{LICENSE_TYPE_LABEL[t]}</option>)}
        </select>
        <select aria-label="Estado" value={params.get("status") ?? ""} onChange={(e) => set("status", e.target.value)}>
          <option value="">Todos los estados</option>
          {(Object.keys(LICENSE_STATUS_LABEL) as LicenseStatus[]).map((t) => <option key={t} value={t}>{LICENSE_STATUS_LABEL[t]}</option>)}
        </select>
        <button className={params.get("expiring") ? "btn btn-primary" : "btn"} aria-pressed={!!params.get("expiring")}
          onClick={() => set("expiring", params.get("expiring") ? "" : "30")}>Caducan en 30 días</button>
      </div>
      {list.error ? <p className="form-error" role="alert">{list.error instanceof ApiError ? list.error.message : "No se ha podido cargar la lista."}</p>
        : !list.data ? <p className="muted">Cargando…</p>
          : list.data.total === 0 ? <EmptyState message="No hay licencias que coincidan." />
            : (
              <>
                <div className="table-wrap">
                  <table>
                    <caption className="visually-hidden">Licencias</caption>
                    <thead><tr><th scope="col">Licencia</th><th scope="col">Tipo</th><th scope="col" className="num">Puestos</th><th scope="col" className="num">Asignadas</th>
                      <th scope="col" className="num">Instaladas</th><th scope="col">Caducidad</th><th scope="col">Estado</th></tr></thead>
                    <tbody>
                      {list.data.items.map((l) => {
                        const over = (n: number) => (l.seats > 0 && n > l.seats ? "num seat-over" : "num");
                        return (
                          <tr key={l.id}>
                            <td><Link className="row-link" to={`/licencias/${l.id}`}>{l.name}</Link> <span className="muted small">{l.vendor}</span></td>
                            <td>{LICENSE_TYPE_LABEL[l.license_type]}</td>
                            <td className="num">{l.seats || "Ilimitados"}</td>
                            <td className={over(l.assigned)}>{l.assigned}</td>
                            <td className={over(l.installed)}>{l.match_pattern ? l.installed : <span className="muted" title="Sin texto de detección">—</span>}</td>
                            <td className="nowrap">{l.expiry_date ? formatDate(l.expiry_date) : <span className="muted">—</span>}</td>
                            <td><span className={statusClass(l.effective_status)}>{LICENSE_STATUS_LABEL[l.effective_status]}</span></td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
                <Pagination page={list.data.page} perPage={list.data.per_page} total={list.data.total} onChange={(p) => set("page", p > 1 ? String(p) : "")} />
              </>
            )}
    </>
  );
}

interface LicenseBody {
  name: string; vendor: string; version: string; license_type: LicenseType; seats: number; license_key?: string;
  purchase_date: string; expiry_date: string; purchase_order: string; price: number | null; department: string;
  status: LicenseStatus; match_pattern: string; notes: string;
}

function LicenseForm({ l, onSave, onCancel }: { l?: License; onSave(b: LicenseBody): Promise<void>; onCancel(): void }) {
  const [f, setF] = useState<LicenseBody>({
    name: l?.name ?? "", vendor: l?.vendor ?? "", version: l?.version ?? "", license_type: l?.license_type ?? "perpetua",
    seats: l?.seats ?? 1, purchase_date: l?.purchase_date ?? "", expiry_date: l?.expiry_date ?? "", purchase_order: l?.purchase_order ?? "",
    price: l?.price ?? null, department: l?.department ?? "", status: l?.status ?? "activa", match_pattern: l?.match_pattern ?? "", notes: l?.notes ?? "",
  });
  const [key, setKey] = useState("");
  const [removeKey, setRemoveKey] = useState(false);
  const [price, setPrice] = useState(l?.price != null ? String(l.price).replace(".", ",") : "");
  const [seats, setSeats] = useState(String(l?.seats ?? 1));
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = <K extends keyof LicenseBody>(k: K, v: LicenseBody[K]) => setF((p) => ({ ...p, [k]: v }));

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    if (!f.name.trim()) return setError("Escribe el nombre de la licencia.");
    const n = Number(seats);
    if (!Number.isInteger(n) || n < 0) return setError("Los puestos deben ser un número entero (0 = ilimitados).");
    let p: number | null = null;
    if (price.trim()) {
      p = Number(price.replace(/\s|€/g, "").replace(",", "."));
      if (!Number.isFinite(p) || p < 0) return setError("El precio debe ser un número, por ejemplo 199,90.");
    }
    const body: LicenseBody = { ...f, name: f.name.trim(), seats: n, price: p };
    if (key.trim()) body.license_key = key.trim();
    else if (removeKey) body.license_key = "";
    setBusy(true);
    try {
      await onSave(body);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido guardar la licencia.");
    } finally {
      setBusy(false);
    }
  }
  const text = (k: keyof LicenseBody, label: string) => (
    <label className="field"><span>{label}</span><input value={f[k] as string} onChange={(e) => set(k, e.target.value as never)} maxLength={200} /></label>
  );
  return (
    <form onSubmit={submit} noValidate>
      <div className="edit-grid">
        {text("name", "Nombre")}
        {text("vendor", "Fabricante")}
        {text("version", "Versión")}
        <label className="field"><span>Tipo</span>
          <select value={f.license_type} onChange={(e) => set("license_type", e.target.value as LicenseType)}>
            {(Object.keys(LICENSE_TYPE_LABEL) as LicenseType[]).map((t) => <option key={t} value={t}>{LICENSE_TYPE_LABEL[t]}</option>)}
          </select>
        </label>
        <label className="field"><span>Puestos <span className="hint">0 = ilimitados</span></span><input inputMode="numeric" value={seats} onChange={(e) => setSeats(e.target.value)} /></label>
        <label className="field"><span>Estado</span>
          <select value={f.status} onChange={(e) => set("status", e.target.value as LicenseStatus)}>
            {(Object.keys(LICENSE_STATUS_LABEL) as LicenseStatus[]).map((t) => <option key={t} value={t}>{LICENSE_STATUS_LABEL[t]}</option>)}
          </select>
        </label>
        <label className="field"><span>Clave de licencia</span>
          <input value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" placeholder={l?.has_key ? "Guardada (escribe para cambiarla)" : ""} className="mono" />
        </label>
        {l?.has_key && <label className="check-field"><input type="checkbox" checked={removeKey} onChange={(e) => setRemoveKey(e.target.checked)} />Quitar la clave guardada</label>}
        <label className="field"><span>Detectar instalaciones <span className="hint">texto del nombre del programa, p. ej. «Office»</span></span>
          <input value={f.match_pattern} onChange={(e) => set("match_pattern", e.target.value)} maxLength={200} /></label>
        <label className="field"><span>Fecha de compra</span><input type="date" value={f.purchase_date} onChange={(e) => set("purchase_date", e.target.value)} /></label>
        <label className="field"><span>Caducidad</span><input type="date" value={f.expiry_date} onChange={(e) => set("expiry_date", e.target.value)} /></label>
        <label className="field"><span>Precio (€)</span><input inputMode="decimal" value={price} onChange={(e) => setPrice(e.target.value)} /></label>
        {text("purchase_order", "Pedido de compra")}
        {text("department", "Departamento")}
      </div>
      <label className="field"><span>Notas</span><textarea rows={3} style={{ width: "100%" }} value={f.notes} onChange={(e) => set("notes", e.target.value)} /></label>
      {error && <p className="form-error" role="alert">{error}</p>}
      <div className="dialog-actions" style={{ justifyContent: "flex-start" }}>
        <button className="btn btn-primary" disabled={busy}>Guardar</button>
        <button type="button" className="btn" onClick={onCancel} disabled={busy}>Cancelar</button>
      </div>
    </form>
  );
}

export function NewLicense() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/licencias">Licencias</Link></p>
      <h1>Nueva licencia</h1>
      <div className="panel">
        <LicenseForm onCancel={() => navigate("/licencias")} onSave={async (b) => {
          const l = await api<License>("POST", "/licenses", b);
          void queryClient.invalidateQueries({ queryKey: ["licenses"] });
          navigate(`/licencias/${l.id}`, { replace: true });
        }} />
      </div>
    </>
  );
}

export function LicenseDetail() {
  const { id = "" } = useParams();
  const { can } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const q = useQuery({ queryKey: ["license", id], queryFn: () => api<License>("GET", `/licenses/${id}`) });
  const inst = useQuery({ queryKey: ["license-installations", id], queryFn: () => api<{ device_id: string; hostname: string; software: string; version: string }[]>("GET", `/licenses/${id}/installations`) });
  const [editing, setEditing] = useState(false);
  const [key, setKey] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [msg, setMsg] = useState("");
  const [person, setPerson] = useState<PersonRef | null>(null);
  const [assetQ, setAssetQ] = useState("");
  const aq = useDebounced(assetQ.trim(), 250);
  const assets = useQuery({ queryKey: ["asset-search", aq], queryFn: () => api<Page<Asset>>("GET", "/assets" + query({ q: aq, per_page: 8 })), enabled: aq.length >= 2 });

  useEffect(() => {
    if (!key) return;
    const t = setTimeout(() => setKey(null), 60_000);
    return () => clearTimeout(t);
  }, [key]);

  if (q.error) return <p className="form-error" role="alert">{q.error instanceof ApiError ? q.error.message : "No se ha podido cargar la licencia."}</p>;
  const l = q.data;
  if (!l) return <p className="muted">Cargando…</p>;
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["license", id] });
    void queryClient.invalidateQueries({ queryKey: ["licenses"] });
  };

  async function reveal() {
    setMsg("");
    try {
      setKey((await api<{ key: string }>("POST", `/licenses/${id}/key/reveal`)).key);
    } catch (err) {
      setMsg(err instanceof ApiError ? err.message : "No se ha podido mostrar la clave.");
    }
  }
  async function assign(body: { asset_id?: string; user_id?: string }) {
    setMsg("");
    try {
      await api("POST", `/licenses/${id}/assignments`, body);
      setAssetQ("");
      setPerson(null);
      refresh();
    } catch (err) {
      setMsg(err instanceof ApiError ? err.message : "No se ha podido asignar.");
    }
  }
  async function unassign(aid: string) {
    await api("DELETE", `/licenses/${id}/assignments/${aid}`);
    refresh();
  }

  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/licencias">Licencias</Link></p>
      <div className="page-head">
        <div>
          <h1>{l.name}</h1>
          <p className="muted">{[l.vendor, l.version, LICENSE_TYPE_LABEL[l.license_type]].filter(Boolean).join(" · ")}</p>
        </div>
        <div className="page-actions">
          {can("operator") && !editing && <button className="btn" onClick={() => setEditing(true)}>Editar</button>}
          {can("admin") && <button className="btn btn-danger-outline" onClick={() => setDeleting(true)}>Eliminar</button>}
        </div>
      </div>
      {l.over && <p className="banner">Se usan más puestos de los comprados: {l.seats} puestos, {l.assigned} asignados, {l.installed} instalaciones detectadas.</p>}
      {msg && <p className="form-error" role="alert">{msg}</p>}
      <div className="panel">
        {editing ? (
          <LicenseForm l={l} onCancel={() => setEditing(false)} onSave={async (b) => {
            await api("PATCH", `/licenses/${id}`, b);
            setEditing(false);
            refresh();
          }} />
        ) : (
          <dl className="facts">
            {([
              ["Estado", <span key="s" className={statusClass(l.effective_status)}>{LICENSE_STATUS_LABEL[l.effective_status]}</span>],
              ["Puestos", l.seats || "Ilimitados"],
              ["Asignadas", l.assigned],
              ["Instalaciones detectadas", l.match_pattern ? <>{l.installed} <span className="muted small">(programas que contienen «{l.match_pattern}»)</span></> : "Sin detección"],
              ["Clave", !l.has_key ? "" : key ? <span className="mono">{key}</span> : can("admin")
                ? <button className="btn btn-small" onClick={() => void reveal()}>Mostrar clave</button> : <span className="muted">Guardada (solo administradores)</span>],
              ["Fecha de compra", l.purchase_date ? formatDate(l.purchase_date) : ""],
              ["Caducidad", l.expiry_date ? formatDate(l.expiry_date) : ""],
              ["Pedido", l.purchase_order],
              ["Precio", formatPrice(l.price)],
              ["Departamento", l.department],
              ["Notas", l.notes],
            ] as [string, React.ReactNode][]).map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v === "" || v === null ? <span className="muted">—</span> : v}</dd></div>)}
          </dl>
        )}
      </div>

      <section className="panel">
        <h2>Asignaciones ({l.assignments?.length ?? 0})</h2>
        {l.assignments?.length ? (
          <ul className="plain-list">
            {l.assignments.map((a) => (
              <li key={a.id}>
                {a.asset ? <>Activo: <Link to={`/inventario/${a.asset.id}`}>{a.asset.name}</Link></> : a.user ? <>Persona: <Link to={`/usuarios/${a.user.id}`}>{a.user.name}</Link></> : "—"}
                {can("operator") && <> <button className="btn btn-quiet btn-small" onClick={() => void unassign(a.id)}>Quitar</button></>}
              </li>
            ))}
          </ul>
        ) : <p className="muted">Sin asignaciones.</p>}
        {can("operator") && (
          <div className="edit-grid" style={{ marginTop: "1rem" }}>
            <div className="field person-picker">
              <label htmlFor="asset-search">Buscar activo</label>
              <input id="asset-search" value={assetQ} onChange={(e) => setAssetQ(e.target.value)} placeholder="Nombre o número de serie…" autoComplete="off" />
              {aq.length >= 2 && assets.data && (
                <ul className="picker-list" role="listbox">
                  {assets.data.items.length === 0 && <li className="muted small">Ningún activo coincide.</li>}
                  {assets.data.items.map((a) => (
                    <li key={a.id} role="option" aria-selected={false}>
                      <button type="button" onClick={() => void assign({ asset_id: a.id })}>{a.name} <span className="muted small">{a.category}{a.serial ? ` · ${a.serial}` : ""}</span></button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <div>
              <PersonPicker label="Asignar a una persona" value={person} onChange={(p) => { setPerson(p); if (p) void assign({ user_id: p.id }); }} />
            </div>
          </div>
        )}
      </section>

      <section className="panel">
        <h2>Instalaciones detectadas ({inst.data?.length ?? 0})</h2>
        {!l.match_pattern ? <p className="muted">Escribe un texto en «Detectar instalaciones» para contar los equipos que tienen este programa.</p>
          : !inst.data ? <p className="muted">Cargando…</p> : inst.data.length === 0 ? <p className="muted">Ningún equipo con agente tiene un programa que contenga «{l.match_pattern}».</p> : (
            <ul className="plain-list">{inst.data.map((d) => <li key={d.device_id}><Link to={`/dispositivos/${d.device_id}`}>{d.hostname}</Link> <span className="muted small">{d.software} {d.version}</span></li>)}</ul>
          )}
      </section>
      {deleting && <ConfirmDialog title={`Eliminar la licencia «${l.name}»`} body="Se borran también sus asignaciones. No se puede deshacer." confirmLabel="Eliminar" danger
        onConfirm={async () => { await api("DELETE", `/licenses/${id}`); void queryClient.invalidateQueries({ queryKey: ["licenses"] }); navigate("/licencias", { replace: true }); }}
        onCancel={() => setDeleting(false)} />}
    </>
  );
}
