import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import ConfirmDialog from "../components/ConfirmDialog";
import { formatDateTime, relativeTime } from "../lib/format";

interface Status {
  configured: boolean;
  linked: boolean;
  project_id?: string;
  client_email?: string;
  enterprise_name?: string;
  enterprise_display_name?: string;
  linked_at?: string | null;
  last_sync_at?: string | null;
  last_sync_error?: string;
}

/** Integración con Google para gestionar móviles Android de empresa. */
export default function AndroidIntegration() {
  const queryClient = useQueryClient();
  const status = useQuery({ queryKey: ["android-integration"], queryFn: () => api<Status>("GET", "/integrations/android") });
  const [key, setKey] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [unlinking, setUnlinking] = useState(false);
  const justLinked = new URLSearchParams(window.location.search).get("vinculada") === "1";

  async function saveKey() {
    setError(""); setBusy(true);
    try {
      const s = await api<Status>("PUT", "/integrations/android/key", { key_json: key.trim() });
      queryClient.setQueryData(["android-integration"], s);
      setKey("");
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "No se ha podido guardar la clave.");
    } finally {
      setBusy(false);
    }
  }

  async function link() {
    setError(""); setBusy(true);
    try {
      const r = await api<{ url: string }>("POST", "/integrations/android/signup");
      window.location.assign(r.url);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "No se ha podido iniciar la vinculación.");
      setBusy(false);
    }
  }

  if (!status.data) return <p className="muted">Cargando…</p>;
  const s = status.data;
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Android</h1>
          <p className="muted">Móviles y tabletas Android de la empresa, gestionados a través de Google (Android Enterprise). Solo para dispositivos propiedad de la empresa.</p>
        </div>
      </div>
      {justLinked && s.linked && <p className="notice" role="status">Empresa vinculada con Google. Ya puedes crear tokens de inscripción en Inscripción › Android.</p>}

      <section className="panel">
        <h2>1. Cuenta de servicio de Google</h2>
        {s.configured ? (
          <p>Proyecto <strong className="mono">{s.project_id}</strong> · <span className="mono small">{s.client_email}</span></p>
        ) : (
          <p className="muted small">Crea un proyecto en Google Cloud, activa la «Android Management API», crea una cuenta de servicio y descarga su clave en JSON. Los pasos detallados están en la documentación (docs/android.md).</p>
        )}
        <label className="field">
          <span>{s.configured ? "Sustituir la clave" : "Clave JSON de la cuenta de servicio"}</span>
          <textarea className="code" rows={4} value={key} onChange={(e) => setKey(e.target.value)} spellCheck={false}
            aria-label="Clave JSON de la cuenta de servicio" placeholder='{"type": "service_account", ...}' />
        </label>
        <button className="btn" disabled={!key.trim() || busy} onClick={() => void saveKey()}>Guardar clave</button>
        <p className="muted small" style={{ marginTop: "0.5rem" }}>Se guarda cifrada en el servidor y nunca se vuelve a mostrar.</p>
      </section>

      <section className="panel" style={{ marginTop: "1rem" }}>
        <h2>2. Empresa en Google</h2>
        {s.linked ? (
          <>
            <p><strong>{s.enterprise_display_name || s.enterprise_name}</strong> <span className="muted small mono">{s.enterprise_name}</span></p>
            <p className="small">
              {s.linked_at && <>Vinculada el {formatDateTime(s.linked_at)} · </>}
              Última sincronización: {s.last_sync_at ? relativeTime(s.last_sync_at) : "aún ninguna"} (cada 15 minutos)
            </p>
            {s.last_sync_error && <p className="form-error">Error en la última sincronización: {s.last_sync_error}</p>}
            <button className="btn btn-danger-outline" style={{ marginTop: "0.75rem" }} onClick={() => setUnlinking(true)}>Desvincular</button>
          </>
        ) : (
          <>
            <p className="muted small">Al pulsar el botón irás a Google para crear la empresa gestionada (con una cuenta de Google de la empresa) y volverás aquí.</p>
            <button className="btn btn-primary" disabled={!s.configured || busy} onClick={() => void link()}>Vincular con Google</button>
          </>
        )}
      </section>
      {error && <p className="form-error" role="alert" style={{ marginTop: "1rem" }}>{error}</p>}
      {unlinking && (
        <ConfirmDialog title="Desvincular la empresa de Google" danger confirmLabel="Desvincular"
          body="Google borrará la empresa: todos los móviles dejarán de estar gestionados y se restablecerán. No se puede deshacer."
          onConfirm={async () => { await api("DELETE", "/integrations/android"); setUnlinking(false); void status.refetch(); }}
          onCancel={() => setUnlinking(false)} />
      )}
    </>
  );
}
