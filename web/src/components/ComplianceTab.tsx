import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { ComplianceDetail, DeviceDetail, RecoveryKeyMeta } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { formatDateTime, relativeTime } from "../lib/format";
import ComplianceBadge from "./ComplianceBadge";
import ConfirmDialog from "./ConfirmDialog";

const REVEAL_SECONDS = 60;

export default function ComplianceTab({ d }: { d: DeviceDetail }) {
  const c = useQuery({ queryKey: ["compliance", d.id], queryFn: () => api<ComplianceDetail>("GET", `/devices/${d.id}/compliance`) });
  if (c.error) return <p className="form-error" role="alert">No se ha podido cargar el cumplimiento.</p>;
  if (!c.data) return <p className="muted">Cargando…</p>;
  const x = c.data;
  return (
    <>
      <div className="panel">
        <p style={{ display: "flex", gap: "1rem", alignItems: "baseline", flexWrap: "wrap" }}>
          <ComplianceBadge state={x.state} />
          {x.noncompliant_since && <span className="muted">No cumple desde {formatDateTime(x.noncompliant_since)}</span>}
          {x.reported_at && <span className="muted small">Último informe del equipo {relativeTime(x.reported_at)}</span>}
        </p>
        {!x.up_to_date && x.results.length > 0 && (
          <p className="banner" style={{ marginTop: "0.75rem", marginBottom: 0 }}>La política ha cambiado y el equipo aún no ha informado con la versión nueva.</p>
        )}
        {x.server_reasons.length > 0 && (
          <ul style={{ margin: "0.75rem 0 0", paddingLeft: "1.2rem" }}>
            {x.server_reasons.map((r) => <li key={r}>{r}</li>)}
          </ul>
        )}
      </div>

      {x.results.length === 0 ? (
        <p className="empty" style={{ marginTop: "1rem" }}>Este equipo no tiene políticas asignadas o aún no ha informado.</p>
      ) : (
        <div className="table-wrap" style={{ marginTop: "1rem" }}>
          <table>
            <thead><tr><th scope="col">Ajuste</th><th scope="col">Política</th><th scope="col">Estado</th><th scope="col">Detalle</th></tr></thead>
            <tbody>
              {x.results.map((r) => (
                <tr key={r.policy_id + r.key}>
                  <td><strong>{r.title}</strong></td>
                  <td>{r.policy_name || <span className="muted">Política eliminada</span>}</td>
                  <td className="nowrap"><ComplianceBadge state={r.status} />{r.applied && <div className="muted small">Corregido por el agente (aplicado)</div>}</td>
                  <td className="small">{r.message || <span className="muted">—</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {x.conflicts.length > 0 && (
        <div className="panel" style={{ marginTop: "1rem" }}>
          <h2>Conflictos entre políticas</h2>
          <ul style={{ margin: 0, paddingLeft: "1.2rem" }}>
            {x.conflicts.map((k) => (
              <li key={k.key + k.loser_id}>
                {k.title}: «{k.winner_name}» gana sobre «{k.loser_name}» por tener mayor prioridad.
              </li>
            ))}
          </ul>
        </div>
      )}

      {d.platform === "windows" && <RecoveryKeys deviceId={d.id} />}
    </>
  );
}

function RecoveryKeys({ deviceId }: { deviceId: string }) {
  const { can } = useAuth();
  const keys = useQuery({ queryKey: ["recovery-keys", deviceId], queryFn: () => api<RecoveryKeyMeta[]>("GET", `/devices/${deviceId}/recovery-keys`) });
  const [asking, setAsking] = useState<RecoveryKeyMeta | null>(null);
  const [shown, setShown] = useState<{ key_id: string; password: string } | null>(null);
  const [left, setLeft] = useState(0);

  useEffect(() => {
    if (!shown) return;
    if (left <= 0) {
      setShown(null);
      return;
    }
    const t = setTimeout(() => setLeft(left - 1), 1000);
    return () => clearTimeout(t);
  }, [shown, left]);

  async function reveal(k: RecoveryKeyMeta) {
    const r = await api<{ recovery_password: string }>("POST", `/devices/${deviceId}/recovery-keys/${encodeURIComponent(k.key_id)}/reveal`);
    setAsking(null);
    setShown({ key_id: k.key_id, password: r.recovery_password });
    setLeft(REVEAL_SECONDS);
  }

  if (!keys.data || keys.data.length === 0) return null;
  return (
    <div className="panel" style={{ marginTop: "1rem" }}>
      <h2>BitLocker</h2>
      {keys.data.map((k) => (
        <div key={k.key_id} style={{ marginBottom: "0.75rem" }}>
          <p>Clave de recuperación custodiada para la unidad {k.volume} <span className="muted small">({relativeTime(k.reported_at)})</span></p>
          <p className="muted small mono">{k.key_id}</p>
          {shown?.key_id === k.key_id ? (
            <p className="secret" style={{ textAlign: "left", marginTop: "0.5rem" }}>
              <span className="secret-value">{shown.password}</span>{" "}
              <span className="muted small" style={{ fontFamily: "var(--font)", letterSpacing: 0 }}>(se oculta en {left} s)</span>
            </p>
          ) : (
            can("admin") && <button className="btn btn-small" onClick={() => setAsking(k)}>Mostrar clave</button>
          )}
        </div>
      ))}
      {asking && (
        <ConfirmDialog title="Mostrar la clave de recuperación" confirmLabel="Mostrar clave"
          body="La consulta quedará registrada en la auditoría con tu nombre. La clave se ocultará al cabo de un minuto."
          onConfirm={() => reveal(asking)} onCancel={() => setAsking(null)} />
      )}
      {keys.error instanceof ApiError && <p className="form-error">{keys.error.message}</p>}
    </div>
  );
}
