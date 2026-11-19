import { useEffect, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import ConfirmDialog from "../components/ConfirmDialog";
import type { Channel } from "./AlertRules";

interface EmailConfig { configured: boolean; host?: string; port?: number; security?: string; username?: string; has_password?: boolean; from_address?: string; from_name?: string }

function EmailForm() {
  const queryClient = useQueryClient();
  const cfg = useQuery({ queryKey: ["email-config"], queryFn: () => api<EmailConfig>("GET", "/integrations/email") });
  const [f, setF] = useState({ host: "", port: 587, security: "starttls", username: "", password: "", from_address: "", from_name: "MDM" });
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [testTo, setTestTo] = useState("");
  useEffect(() => {
    const c = cfg.data;
    if (c?.configured) setF({ host: c.host!, port: c.port!, security: c.security!, username: c.username ?? "", password: c.has_password ? "********" : "",
      from_address: c.from_address!, from_name: c.from_name ?? "" });
  }, [cfg.data]);
  async function save(e: FormEvent) {
    e.preventDefault();
    setMsg(null);
    try {
      await api("PUT", "/integrations/email", f);
      void queryClient.invalidateQueries({ queryKey: ["email-config"] });
      setMsg({ ok: true, text: "Configuración guardada." });
    } catch (err) {
      setMsg({ ok: false, text: err instanceof ApiError ? err.message : "No se ha podido guardar." });
    }
  }
  async function test() {
    setMsg(null);
    try {
      await api("POST", "/integrations/email/test", { to: testTo.trim() });
      setMsg({ ok: true, text: "Correo de prueba enviado a " + testTo.trim() + "." });
    } catch (err) {
      setMsg({ ok: false, text: err instanceof ApiError ? err.message : "No se ha podido enviar." });
    }
  }
  return (
    <form className="panel" onSubmit={save} noValidate>
      <h2>Correo (SMTP)</h2>
      <div className="edit-grid">
        <label className="field"><span>Servidor</span><input value={f.host} onChange={(e) => setF({ ...f, host: e.target.value })} placeholder="smtp.office365.com" /></label>
        <label className="field"><span>Puerto</span><input type="number" value={f.port} onChange={(e) => setF({ ...f, port: Number(e.target.value) })} /></label>
        <label className="field"><span>Seguridad</span>
          <select value={f.security} onChange={(e) => setF({ ...f, security: e.target.value })}>
            <option value="starttls">STARTTLS (puerto 587)</option><option value="tls">TLS (puerto 465)</option><option value="none">Sin cifrar (solo red interna)</option>
          </select></label>
        <label className="field"><span>Usuario <span className="hint">opcional</span></span><input value={f.username} onChange={(e) => setF({ ...f, username: e.target.value })} autoComplete="off" /></label>
        <label className="field"><span>Contraseña</span><input type="password" value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })}
          onFocus={() => { if (f.password === "********") setF({ ...f, password: "" }); }} autoComplete="new-password" /></label>
        <label className="field"><span>Remitente</span><input value={f.from_address} onChange={(e) => setF({ ...f, from_address: e.target.value })} placeholder="mdm@empresa.es" /></label>
        <label className="field"><span>Nombre del remitente</span><input value={f.from_name} onChange={(e) => setF({ ...f, from_name: e.target.value })} /></label>
      </div>
      <button className="btn btn-primary">Guardar correo</button>
      {cfg.data?.configured && (
        <div className="inline-form" style={{ marginTop: "1rem" }}>
          <label className="field"><span>Enviar un correo de prueba a</span><input value={testTo} onChange={(e) => setTestTo(e.target.value)} /></label>
          <button type="button" className="btn" disabled={!testTo.trim()} onClick={() => void test()}>Enviar prueba</button>
        </div>
      )}
      {msg && <p className={msg.ok ? "form-ok" : "form-error"} role={msg.ok ? "status" : "alert"}>{msg.text}</p>}
    </form>
  );
}

/** Integraciones › Avisos: correo y canales (correo o webhook). */
export default function Notifications() {
  const queryClient = useQueryClient();
  const channels = useQuery({ queryKey: ["channels"], queryFn: () => api<Channel[]>("GET", "/channels") });
  const [name, setName] = useState("");
  const [type, setType] = useState<"email" | "webhook">("email");
  const [recipients, setRecipients] = useState("");
  const [url, setUrl] = useState("");
  const [created, setCreated] = useState<Channel | null>(null);
  const [error, setError] = useState("");
  const [result, setResult] = useState<Record<string, string>>({});
  const [deleting, setDeleting] = useState<Channel | null>(null);

  async function create(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      const c = await api<Channel>("POST", "/channels", { name: name.trim(), type, recipients: recipients.split(/[\s,;]+/).filter(Boolean), url: url.trim() });
      setCreated(c);
      setName(""); setRecipients(""); setUrl("");
      void queryClient.invalidateQueries({ queryKey: ["channels"] });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido crear el canal.");
    }
  }
  async function test(c: Channel) {
    try {
      await api("POST", `/channels/${c.id}/test`);
      setResult({ ...result, [c.id]: "Enviado." });
    } catch (err) {
      setResult({ ...result, [c.id]: err instanceof ApiError ? err.message : "Ha fallado." });
    }
    void queryClient.invalidateQueries({ queryKey: ["channels"] });
  }

  return (
    <>
      <div className="page-head"><div><h1>Avisos</h1><p className="muted">Por dónde llegan las alertas, los informes programados y los avisos de las automatizaciones.</p></div></div>
      <EmailForm />
      <section className="section">
        <h2>Canales</h2>
        {channels.data && channels.data.length > 0 && (
          <div className="table-wrap">
            <table>
              <thead><tr><th scope="col">Canal</th><th scope="col">Destino</th><th scope="col">Estado</th><th scope="col"><span className="visually-hidden">Acciones</span></th></tr></thead>
              <tbody>
                {channels.data.map((c) => (
                  <tr key={c.id}>
                    <td><strong>{c.name}</strong><div className="muted small">{c.type === "email" ? "Correo" : "Webhook"}</div></td>
                    <td className="small" style={{ overflowWrap: "anywhere" }}>{c.type === "email" ? c.recipients.join(", ") : c.url}</td>
                    <td className="small">{result[c.id] ?? (c.last_error ? <span style={{ color: "var(--bad)" }}>Último error: {c.last_error}</span> : "Sin errores")}</td>
                    <td className="num nowrap"><button className="btn btn-small" onClick={() => void test(c)}>Enviar prueba</button>{" "}
                      <button className="btn btn-small" onClick={() => setDeleting(c)}>Eliminar</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <form className="panel" onSubmit={create} noValidate style={{ marginTop: "1rem" }}>
          <h3>Nuevo canal</h3>
          <div className="edit-grid">
            <label className="field"><span>Nombre del canal</span><input value={name} onChange={(e) => setName(e.target.value)} /></label>
            <label className="field"><span>Tipo de canal</span>
              <select value={type} onChange={(e) => setType(e.target.value as "email" | "webhook")}><option value="email">Correo</option><option value="webhook">Webhook (Teams, Slack u otro sistema)</option></select></label>
            {type === "email" ? (
              <label className="field"><span>Destinatarios <span className="hint">separados por comas</span></span><input value={recipients} onChange={(e) => setRecipients(e.target.value)} /></label>
            ) : (
              <label className="field"><span>Dirección (URL)</span><input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://…" /></label>
            )}
          </div>
          {error && <p className="form-error" role="alert">{error}</p>}
          <button className="btn btn-primary" disabled={!name.trim()}>Crear canal</button>
        </form>
        {created?.secret && (
          <div className="notice" role="status" style={{ marginTop: "1rem" }}>
            <p>Secreto de firma de «{created.name}» (cabecera <code>X-MDM-Signature</code>). Cópialo ahora: no se volverá a mostrar.</p>
            <p className="mono" style={{ userSelect: "all", overflowWrap: "anywhere" }}>{created.secret}</p>
          </div>
        )}
      </section>
      {deleting && (
        <ConfirmDialog title={`Eliminar «${deleting.name}»`} body="Las reglas y los informes que lo usan dejarán de avisar por este canal." confirmLabel="Eliminar" danger
          onConfirm={async () => { await api("DELETE", `/channels/${deleting.id}`); setDeleting(null); void queryClient.invalidateQueries({ queryKey: ["channels"] }); }}
          onCancel={() => setDeleting(null)} />
      )}
    </>
  );
}
