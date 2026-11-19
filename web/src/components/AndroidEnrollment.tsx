import { useEffect, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import QRCode from "qrcode";
import { api, ApiError } from "../api/client";
import type { Group } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { formatDateTime } from "../lib/format";

interface AndroidToken {
  id: string; name: string; qr_code: string; group_id: string | null; group_name: string;
  one_time: boolean; expires_at: string; revoked_at: string | null; created_at: string;
}

function QR({ token }: { token: AndroidToken }) {
  const [src, setSrc] = useState("");
  useEffect(() => {
    QRCode.toDataURL(token.qr_code, { errorCorrectionLevel: "M", margin: 2, width: 320 }).then(setSrc).catch(() => setSrc(""));
  }, [token.qr_code]);
  if (!src) return null;
  return (
    <figure className="qr">
      <img src={src} width={320} height={320} alt={`Código QR de inscripción para ${token.name}`} />
      <figcaption className="small">
        En un móvil de fábrica (o restablecido), toca seis veces la pantalla de bienvenida, conéctalo a una red Wi-Fi y escanea este código.
        Caduca el {formatDateTime(token.expires_at)}{token.one_time ? " y sirve para un solo móvil" : ""}.
      </figcaption>
    </figure>
  );
}

/** Tokens de inscripción de móviles Android (código QR). */
export default function AndroidEnrollment() {
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const tokens = useQuery({ queryKey: ["android-tokens"], queryFn: () => api<AndroidToken[]>("GET", "/android/enrollment-tokens") });
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });
  const [name, setName] = useState("");
  const [group, setGroup] = useState("");
  const [days, setDays] = useState(7);
  const [oneTime, setOneTime] = useState(false);
  const [shown, setShown] = useState<AndroidToken | null>(null);
  const [error, setError] = useState("");

  async function create(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      const t = await api<AndroidToken>("POST", "/android/enrollment-tokens", { name: name.trim(), group_id: group || null, duration_days: days, one_time: oneTime });
      setShown(t);
      setName("");
      void queryClient.invalidateQueries({ queryKey: ["android-tokens"] });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido crear el token.");
    }
  }

  async function revoke(t: AndroidToken) {
    await api("POST", `/android/enrollment-tokens/${t.id}/revoke`);
    if (shown?.id === t.id) setShown(null);
    void queryClient.invalidateQueries({ queryKey: ["android-tokens"] });
  }

  const now = Date.now();
  return (
    <>
      {can("operator") && (
        <form className="panel" onSubmit={create} noValidate>
          <h2>Nuevo token para móviles</h2>
          <div className="edit-grid">
            <label className="field"><span>Nombre del token</span><input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} /></label>
            <label className="field">
              <span>Grupo de destino</span>
              <select value={group} onChange={(e) => setGroup(e.target.value)}>
                <option value="">Sin grupo</option>
                {groups.data?.filter((g) => g.kind !== "dynamic").map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
              </select>
            </label>
            <label className="field"><span>Validez (días)</span><input type="number" min={1} max={90} value={days} onChange={(e) => setDays(Number(e.target.value))} /></label>
          </div>
          <label className="check-field"><input type="checkbox" checked={oneTime} onChange={(e) => setOneTime(e.target.checked)} />Un solo uso</label>
          {error && <p className="form-error" role="alert">{error}</p>}
          <button className="btn btn-primary" disabled={!name.trim()}>Crear token</button>
        </form>
      )}
      {shown && <QR token={shown} />}
      <section className="section">
        <h2>Tokens de móviles</h2>
        {!tokens.data ? <p className="muted">Cargando…</p> : tokens.data.length === 0 ? <p className="muted">Ninguno.</p> : (
          <div className="table-wrap">
            <table>
              <thead><tr><th scope="col">Nombre</th><th scope="col">Grupo</th><th scope="col">Caduca</th><th scope="col"><span className="visually-hidden">Acciones</span></th></tr></thead>
              <tbody>
                {tokens.data.map((t) => {
                  const active = !t.revoked_at && new Date(t.expires_at).getTime() > now;
                  return (
                    <tr key={t.id}>
                      <td><strong>{t.name}</strong>{t.one_time && <span className="muted small"> · un solo uso</span>}</td>
                      <td>{t.group_name || <span className="muted">—</span>}</td>
                      <td className="nowrap">{t.revoked_at ? <span className="muted">Revocado</span> : formatDateTime(t.expires_at)}</td>
                      <td className="num nowrap">
                        {active && <button className="btn btn-small" onClick={() => setShown(t)}>Ver QR</button>}{" "}
                        {active && can("operator") && <button className="btn btn-small" onClick={() => void revoke(t)}>Revocar</button>}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </>
  );
}
