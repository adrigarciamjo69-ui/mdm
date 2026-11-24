import { useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { Category, DeliverySettings } from "../api/types";
import { relativeTime } from "../lib/format";

interface EntraConfig {
  tenant_id: string; client_id: string; client_secret: string; secret_expires: string; configured: boolean;
  last_sync_at?: string | null; last_success_at?: string | null; last_error?: string; last_count?: number;
}

export default function InventorySettings() {
  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/ajustes">Ajustes</Link></p>
      <div className="page-head"><h1>Ajustes del inventario</h1></div>
      <EntraSection />
      <CategoriesSection />
      <DeliverySection />
    </>
  );
}

function EntraSection() {
  const queryClient = useQueryClient();
  const q = useQuery({ queryKey: ["entra-config"], queryFn: () => api<EntraConfig>("GET", "/integrations/entra") });
  const [f, setF] = useState({ tenant_id: "", client_id: "", client_secret: "", secret_expires: "" });
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (q.data) setF({ tenant_id: q.data.tenant_id, client_id: q.data.client_id, client_secret: q.data.client_secret, secret_expires: q.data.secret_expires });
  }, [q.data]);
  const set = (k: keyof typeof f, v: string) => setF((p) => ({ ...p, [k]: v }));

  async function run(kind: "test" | "save") {
    setMsg(null);
    setBusy(true);
    try {
      if (kind === "test") {
        const r = await api<{ users: number }>("POST", "/integrations/entra/test", f);
        setMsg({ ok: true, text: `Conexión correcta: Entra ID devuelve ${r.users} personas.` });
      } else {
        const r = await api<EntraConfig>("PUT", "/integrations/entra", f);
        queryClient.setQueryData(["entra-config"], r);
        void queryClient.invalidateQueries({ queryKey: ["directory-status"] });
        setMsg({ ok: true, text: "Conexión guardada. La primera sincronización se hará en la próxima hora, o pulsa «Sincronizar ahora» en Usuarios." });
      }
    } catch (err) {
      setMsg({ ok: false, text: err instanceof ApiError ? err.message : "No se ha podido completar la acción." });
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="panel" aria-labelledby="s-entra">
      <h2 id="s-entra">Usuarios de Entra ID</h2>
      <details>
        <summary>Cómo registrar la aplicación en Entra (una vez)</summary>
        <ol className="small">
          <li>En el centro de administración de Entra, ve a <strong>Aplicaciones › Registros de aplicaciones › Nuevo registro</strong>, por ejemplo «MDM inventario». Cuentas de este directorio solamente.</li>
          <li>En <strong>Permisos de API › Agregar un permiso › Microsoft Graph › Permisos de la aplicación</strong>, añade <strong>User.Read.All</strong> y pulsa <strong>Conceder consentimiento de administrador</strong>.</li>
          <li>En <strong>Certificados y secretos › Nuevo secreto de cliente</strong>, crea uno y copia su <em>valor</em> (solo se ve al crearlo). Apunta su caducidad.</li>
          <li>Copia aquí el <strong>Id. de directorio (inquilino)</strong> y el <strong>Id. de aplicación (cliente)</strong> de la página de información general.</li>
        </ol>
      </details>
      <form onSubmit={(e: FormEvent) => { e.preventDefault(); void run("save"); }} noValidate>
        <div className="edit-grid">
          <label className="field"><span>Identificador del inquilino (tenant)</span><input value={f.tenant_id} onChange={(e) => set("tenant_id", e.target.value)} autoComplete="off" /></label>
          <label className="field"><span>Identificador de la aplicación (client ID)</span><input value={f.client_id} onChange={(e) => set("client_id", e.target.value)} autoComplete="off" /></label>
          <label className="field"><span>Secreto de cliente</span><input type="password" value={f.client_secret} onChange={(e) => set("client_secret", e.target.value)} autoComplete="new-password" /></label>
          <label className="field"><span>Caducidad del secreto <span className="hint">para avisar 14 días antes</span></span><input type="date" value={f.secret_expires} onChange={(e) => set("secret_expires", e.target.value)} /></label>
        </div>
        {q.data?.configured && (
          <p className="muted small">
            {q.data.last_success_at ? <>Última sincronización correcta {relativeTime(q.data.last_success_at)} ({q.data.last_count} personas). </> : "Aún no se ha sincronizado. "}
            {q.data.last_error && <span className="form-error">Último error: {q.data.last_error}</span>}
          </p>
        )}
        {msg && <p className={msg.ok ? "form-ok" : "form-error"} role={msg.ok ? "status" : "alert"}>{msg.text}</p>}
        <div className="dialog-actions" style={{ justifyContent: "flex-start" }}>
          <button type="button" className="btn" onClick={() => void run("test")} disabled={busy}>Probar conexión</button>
          <button className="btn btn-primary" disabled={busy}>Guardar conexión</button>
        </div>
      </form>
    </section>
  );
}

function CategoriesSection() {
  const queryClient = useQueryClient();
  const q = useQuery({ queryKey: ["asset-categories"], queryFn: () => api<Category[]>("GET", "/asset-categories") });
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["asset-categories"] });

  async function act(fn: () => Promise<unknown>) {
    setError("");
    try {
      await fn();
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido guardar.");
    }
  }
  return (
    <section className="panel" aria-labelledby="s-cats">
      <h2 id="s-cats">Categorías de activos</h2>
      {error && <p className="form-error" role="alert">{error}</p>}
      {q.data && (
        <ul className="plain-list">
          {q.data.map((c) => (
            <li key={c.id}>
              <strong>{c.name}</strong> <span className="muted small">{c.assets} {c.assets === 1 ? "activo" : "activos"}</span>{" "}
              <button className="btn btn-quiet btn-small" onClick={() => {
                const n = window.prompt?.("Nuevo nombre", c.name);
                if (n && n.trim() && n !== c.name) void act(() => api("PATCH", `/asset-categories/${c.id}`, { name: n.trim() }));
              }}>Renombrar</button>
              {c.assets === 0 && <button className="btn btn-quiet btn-small" onClick={() => void act(() => api("DELETE", `/asset-categories/${c.id}`))}>Borrar</button>}
            </li>
          ))}
        </ul>
      )}
      <form className="inline-form" onSubmit={(e) => { e.preventDefault(); if (name.trim()) void act(async () => { await api("POST", "/asset-categories", { name: name.trim() }); setName(""); }); }}>
        <label className="field"><span>Nueva categoría</span><input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} /></label>
        <button className="btn" disabled={!name.trim()}>Añadir</button>
      </form>
    </section>
  );
}

function DeliverySection() {
  const queryClient = useQueryClient();
  const q = useQuery({ queryKey: ["delivery-settings"], queryFn: () => api<DeliverySettings>("GET", "/delivery-settings") });
  const [s, setS] = useState<DeliverySettings | null>(null);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  useEffect(() => {
    if (q.data) setS(q.data);
  }, [q.data]);
  if (!s) return <section className="panel"><h2>Actas de entrega</h2><p className="muted">Cargando…</p></section>;
  const set = (k: keyof DeliverySettings, v: string) => setS({ ...s, [k]: v });

  function onLogo(file: File) {
    setMsg(null);
    if (!/^image\/(png|jpeg)$/.test(file.type)) return setMsg({ ok: false, text: "El logotipo debe ser PNG o JPG." });
    if (file.size > 500 * 1024) return setMsg({ ok: false, text: "El logotipo no puede superar los 500 KB." });
    const r = new FileReader();
    r.onload = () => setS((p) => (p ? { ...p, logo: String(r.result) } : p));
    r.readAsDataURL(file);
  }

  async function save(e: FormEvent) {
    e.preventDefault();
    setMsg(null);
    try {
      const saved = await api<DeliverySettings>("PUT", "/delivery-settings", s);
      queryClient.setQueryData(["delivery-settings"], saved);
      setMsg({ ok: true, text: "Datos de las actas guardados." });
    } catch (err) {
      setMsg({ ok: false, text: err instanceof ApiError ? err.message : "No se han podido guardar." });
    }
  }
  const text = (k: keyof DeliverySettings, label: string) => (
    <label className="field"><span>{label}</span><input value={s[k] as string} onChange={(e) => set(k, e.target.value)} maxLength={200} /></label>
  );
  const clauses = (type: "entrega" | "devolucion", label: string) => (
    <label className="field clause-list"><span>{label} <span className="hint">una por línea; puedes usar {"{empresa}"}, {"{trabajador}"} y {"{ciudad}"}</span></span>
      <textarea rows={6} value={s.clauses[type].join("\n")}
        onChange={(e) => setS({ ...s, clauses: { ...s.clauses, [type]: e.target.value.split("\n").filter((l, i, all) => l.trim() || i < all.length - 1) } })} />
    </label>
  );
  const color = (type: "entrega" | "devolucion", part: "primary" | "footer", label: string) => (
    <label className="field"><span>{label}</span>
      <input type="color" value={s.colors[type][part]} onChange={(e) => setS({ ...s, colors: { ...s.colors, [type]: { ...s.colors[type], [part]: e.target.value } } })} />
    </label>
  );
  return (
    <form className="panel" aria-labelledby="s-actas" onSubmit={save} noValidate>
      <h2 id="s-actas">Actas de entrega</h2>
      <p className="muted small">Estos datos aparecen en el PDF de las actas de entrega y devolución.</p>
      <div className="edit-grid">
        {text("company", "Empresa")}
        {text("nif", "NIF")}
        {text("address1", "Dirección (línea 1)")}
        {text("address2", "Dirección (línea 2)")}
        {text("phone", "Teléfono")}
        {text("web", "Web")}
        {text("email", "Correo")}
        {text("city", "Ciudad")}
        <label className="field"><span>Logotipo <span className="hint">PNG o JPG, hasta 500 KB</span></span>
          <input type="file" accept="image/png,image/jpeg" onChange={(e) => e.target.files?.[0] && onLogo(e.target.files[0])} /></label>
        {s.logo && <div className="field"><span>Logotipo actual</span><span><img className="logo-preview" src={s.logo} alt="Logotipo" />{" "}
          <button type="button" className="btn btn-quiet btn-small" onClick={() => setS({ ...s, logo: "" })}>Quitar</button></span></div>}
        {color("entrega", "primary", "Color de las actas de entrega")}
        {color("devolucion", "primary", "Color de las actas de devolución")}
      </div>
      {clauses("entrega", "Cláusulas de entrega")}
      {clauses("devolucion", "Cláusulas de devolución")}
      {msg && <p className={msg.ok ? "form-ok" : "form-error"} role={msg.ok ? "status" : "alert"}>{msg.text}</p>}
      <button className="btn btn-primary">Guardar datos de las actas</button>
    </form>
  );
}
