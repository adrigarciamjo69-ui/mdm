import { useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, getCsrfToken } from "../api/client";
import type { Asset, AssetDocument, Delivery, License } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { formatDate, formatDateTime } from "../lib/format";
import { auditLabel } from "../lib/auditLabels";
import { ASSET_STATUS_CLASS, ASSET_STATUS_LABEL, formatPrice, formatSize, LICENSE_STATUS_LABEL } from "../lib/inventory";
import AssetForm, { type AssetBody } from "./AssetForm";
import ConfirmDialog from "./ConfirmDialog";

const DOC_ACCEPT = ".pdf,.doc,.docx,.xls,.xlsx,.jpg,.jpeg,.png,.gif,.webp,.txt,.zip";

function Facts({ rows }: { rows: [string, React.ReactNode][] }) {
  return (
    <dl className="facts">
      {rows.map(([k, v]) => (
        <div key={k}><dt>{k}</dt><dd>{v === "" || v === null || v === undefined ? <span className="muted">—</span> : v}</dd></div>
      ))}
    </dl>
  );
}

/** Datos de inventario de un activo: ficha, documentos, licencias, actas e historial. */
export default function AssetPanel({ asset }: { asset: Asset }) {
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [editing, setEditing] = useState(false);
  const [dialog, setDialog] = useState<"trash" | "unlink" | null>(null);
  const [reason, setReason] = useState("");
  const id = asset.id;

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ["asset", id] });
    if (asset.device_id) await queryClient.invalidateQueries({ queryKey: ["device-asset", asset.device_id] });
    void queryClient.invalidateQueries({ queryKey: ["assets"] });
    void queryClient.invalidateQueries({ queryKey: ["asset-history", id] });
  };

  async function save(body: AssetBody) {
    await api("PATCH", `/assets/${id}`, body);
    await refresh();
    setEditing(false);
  }

  async function trash() {
    await api("DELETE", `/assets/${id}`, { reason: reason.trim() });
    void queryClient.invalidateQueries({ queryKey: ["assets"] });
    navigate("/inventario", { replace: true });
  }

  async function unlink() {
    await api("POST", `/assets/${id}/unlink`);
    setDialog(null);
    await refresh();
    navigate(`/inventario/${id}`, { replace: true });
  }

  return (
    <div className="asset-panel">
      <div className="panel">
        <div className="panel-head">
          <h2>Inventario</h2>
          {can("operator") && !editing && <button className="btn btn-small" onClick={() => setEditing(true)}>Editar</button>}
        </div>
        {editing ? (
          <AssetForm asset={asset} onSave={save} onCancel={() => setEditing(false)} saveLabel="Guardar cambios" />
        ) : (
          <Facts rows={[
            ["Categoría", <>{asset.category}{asset.category_auto && <span className="hint"> (automática)</span>}</>],
            ["Estado", <span className={ASSET_STATUS_CLASS[asset.status]}>{ASSET_STATUS_LABEL[asset.status]}</span>],
            ["Persona", asset.owner ? <Link to={`/usuarios/${asset.owner.id}`}>{asset.owner.name}</Link> : ""],
            ["Departamento", asset.department],
            ["Ubicación", asset.location],
            ["Marca y modelo", [asset.brand, asset.model].filter(Boolean).join(" ")],
            ["Número de serie", asset.serial ? <span className="mono">{asset.serial}</span> : ""],
            ["Etiqueta interna", asset.asset_tag ? <span className="mono">{asset.asset_tag}</span> : ""],
            ["Precio", formatPrice(asset.price)],
            ["Fecha de compra", asset.purchase_date ? formatDate(asset.purchase_date) : ""],
            ["Garantía hasta", asset.warranty_until ? formatDate(asset.warranty_until) : ""],
            ["Pedido", asset.purchase_order],
            ["Proveedor", asset.supplier],
            ["Notas", asset.notes ? <span style={{ whiteSpace: "pre-wrap" }}>{asset.notes}</span> : ""],
          ]} />
        )}
        {asset.device && asset.device.serial && asset.serial && asset.device.serial.trim().toUpperCase() !== asset.serial.trim().toUpperCase() && (
          <p className="banner">El agente informa de otro número de serie: <span className="mono">{asset.device.serial}</span>.</p>
        )}
      </div>

      <Documents asset={asset} />
      <Licenses assetId={id} />
      <Deliveries assetId={id} />
      <History assetId={id} />

      {can("admin") && (
        <div className="page-actions" style={{ marginTop: "1rem" }}>
          {asset.device_id ? (
            <button className="btn" onClick={() => setDialog("unlink")}>Desenlazar del equipo</button>
          ) : (
            <button className="btn btn-danger-outline" onClick={() => setDialog("trash")}>Enviar a la papelera</button>
          )}
        </div>
      )}
      {dialog === "trash" && (
        <div className="dialog-backdrop">
          <div className="confirm" role="dialog" aria-modal="true" aria-labelledby="trash-title">
            <h2 id="trash-title">Enviar «{asset.name}» a la papelera</h2>
            <p>Desaparece del inventario y pierde su persona. Se puede restaurar desde la papelera.</p>
            <label className="field"><span>Motivo <span className="hint">opcional, queda en la auditoría</span></span>
              <input value={reason} onChange={(e) => setReason(e.target.value)} maxLength={200} /></label>
            <div className="dialog-actions">
              <button className="btn" onClick={() => setDialog(null)}>Cancelar</button>
              <button className="btn btn-danger" onClick={() => void trash()}>Enviar a la papelera</button>
            </div>
          </div>
        </div>
      )}
      {dialog === "unlink" && (
        <ConfirmDialog title="Desenlazar este activo de su equipo"
          body="El activo queda como manual (sin unirse por número de serie) y el equipo recibe un activo nuevo en el inventario. Úsalo si la unión fue un error."
          confirmLabel="Desenlazar" onConfirm={unlink} onCancel={() => setDialog(null)} />
      )}
    </div>
  );
}

function Documents({ asset }: { asset: Asset }) {
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const docs = useQuery({ queryKey: ["asset-docs", asset.id], queryFn: () => api<AssetDocument[]>("GET", `/assets/${asset.id}/documents`) });
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const input = useRef<HTMLInputElement>(null);

  async function upload(file: File) {
    setMsg("");
    if (file.size > 20 * 1024 * 1024) return setMsg("El fichero supera los 20 MB.");
    setBusy(true);
    const form = new FormData();
    form.append("file", file);
    const csrf = getCsrfToken();
    try {
      const res = await fetch(`/api/v1/assets/${asset.id}/documents`, { method: "POST", body: form, credentials: "same-origin",
        headers: csrf ? { "X-CSRF-Token": csrf } : {} });
      if (!res.ok) {
        const e = await res.json().catch(() => null) as { error?: { message?: string } } | null;
        setMsg(e?.error?.message ?? "No se ha podido subir el fichero.");
      } else {
        void queryClient.invalidateQueries({ queryKey: ["asset-docs", asset.id] });
      }
    } catch {
      setMsg("La subida se interrumpió. Comprueba la conexión y vuelve a intentarlo.");
    } finally {
      setBusy(false);
      if (input.current) input.current.value = "";
    }
  }

  async function remove(d: AssetDocument) {
    await api("DELETE", `/assets/${asset.id}/documents/${d.id}`);
    void queryClient.invalidateQueries({ queryKey: ["asset-docs", asset.id] });
  }

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>Documentos</h2>
        {can("operator") && (
          <label className="btn btn-small">
            {busy ? "Subiendo…" : "Subir documento"}
            <input ref={input} type="file" className="visually-hidden" accept={DOC_ACCEPT} aria-label="Subir documento" disabled={busy}
              onChange={(e) => e.target.files?.[0] && void upload(e.target.files[0])} />
          </label>
        )}
      </div>
      <p className="muted small">Facturas, garantías, albaranes o fotos: PDF, Word, Excel, imágenes, texto o ZIP, hasta 20 MB.</p>
      {msg && <p className="form-error" role="alert">{msg}</p>}
      {!docs.data ? <p className="muted">Cargando…</p> : docs.data.length === 0 ? <p className="muted">Sin documentos.</p> : (
        <ul className="doc-list">
          {docs.data.map((d) => (
            <li key={d.id}>
              <a href={`/api/v1/assets/${asset.id}/documents/${d.id}`} download={d.filename}>{d.filename}</a>
              <span className="muted small"> {formatSize(d.size)} · {formatDateTime(d.uploaded_at)}{d.uploaded_by ? ` · ${d.uploaded_by}` : ""}</span>
              {can("operator") && <button className="btn btn-quiet btn-small" onClick={() => void remove(d)} aria-label={`Borrar ${d.filename}`}>Borrar</button>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function Licenses({ assetId }: { assetId: string }) {
  const q = useQuery({ queryKey: ["asset-licenses", assetId], queryFn: () => api<License[]>("GET", `/assets/${assetId}/licenses`) });
  return (
    <div className="panel">
      <h2>Licencias</h2>
      {!q.data ? <p className="muted">Cargando…</p> : q.data.length === 0 ? <p className="muted">Sin licencias asignadas. Se asignan desde Licencias.</p> : (
        <ul className="plain-list">
          {q.data.map((l) => <li key={l.id}><Link to={`/licencias/${l.id}`}>{l.name}</Link> <span className="muted small">{LICENSE_STATUS_LABEL[l.effective_status]}</span></li>)}
        </ul>
      )}
    </div>
  );
}

function Deliveries({ assetId }: { assetId: string }) {
  const q = useQuery({ queryKey: ["asset-deliveries", assetId], queryFn: () => api<Delivery[]>("GET", `/assets/${assetId}/deliveries`) });
  return (
    <div className="panel">
      <h2>Actas</h2>
      {!q.data ? <p className="muted">Cargando…</p> : q.data.length === 0 ? <p className="muted">Este activo no aparece en ninguna acta.</p> : (
        <ul className="plain-list">
          {q.data.map((d) => (
            <li key={d.id}>
              <Link to={`/actas/${d.id}`}>{d.number || "Borrador"}</Link>{" "}
              <span className="muted small">{d.type === "entrega" ? "Entrega" : "Devolución"} a {d.user.name} · {formatDate(d.date)}{d.status === "anulada" ? " · anulada" : ""}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function History({ assetId }: { assetId: string }) {
  const q = useQuery({
    queryKey: ["asset-history", assetId],
    queryFn: () => api<{ id: number; at: string; actor_email: string; action: string }[]>("GET", `/assets/${assetId}/history`),
  });
  if (q.error) return null; // la auditoría completa es solo para administradores; aquí basta con no mostrarla
  return (
    <details className="panel">
      <summary><h2 style={{ display: "inline" }}>Historial</h2></summary>
      {!q.data ? <p className="muted">Cargando…</p> : q.data.length === 0 ? <p className="muted">Sin cambios registrados.</p> : (
        <ul className="plain-list">
          {q.data.map((e) => <li key={e.id}><span className="muted small">{formatDateTime(e.at)}</span> {auditLabel(e.action)} <span className="muted small">{e.actor_email}</span></li>)}
        </ul>
      )}
    </details>
  );
}

export function useAssetError(err: unknown): string {
  return err instanceof ApiError ? err.message : "No se ha podido cargar el activo.";
}
