import { Link, Navigate, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { Asset } from "../api/types";
import AssetPanel from "../components/AssetPanel";
import AssetForm, { type AssetBody } from "../components/AssetForm";
import { ASSET_STATUS_CLASS, ASSET_STATUS_LABEL } from "../lib/inventory";

/** Ficha de un activo manual (los de agente usan la ficha del equipo). */
export default function AssetDetail() {
  const { id = "" } = useParams();
  const q = useQuery({ queryKey: ["asset", id], queryFn: () => api<Asset>("GET", `/assets/${id}`) });
  if (q.error) {
    return <p className="form-error" role="alert">{q.error instanceof ApiError ? q.error.message : "No se ha podido cargar el activo."}</p>;
  }
  const a = q.data;
  if (!a) return <p className="muted">Cargando…</p>;
  if (a.device_id && !a.deleted_at) return <Navigate to={`/dispositivos/${a.device_id}?tab=asset`} replace />;
  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/inventario">Inventario</Link></p>
      <header className="asset-tag">
        <div>
          <h1>{a.name}</h1>
          <p className="asset-model">{[a.category, a.brand, a.model].filter(Boolean).join(" · ")}</p>
        </div>
        <dl>
          <dt>Estado</dt>
          <dd><span className={ASSET_STATUS_CLASS[a.status]}>{ASSET_STATUS_LABEL[a.status]}</span></dd>
          <dt>Número de serie</dt>
          <dd className="mono">{a.serial || "—"}</dd>
          <dt>Persona</dt>
          <dd>{a.owner ? <Link to={`/usuarios/${a.owner.id}`}>{a.owner.name}</Link> : "—"}</dd>
        </dl>
        <div className="asset-strip">
          <span>Activo manual</span>
          <span className="barcode" aria-hidden="true" />
          <span>{a.asset_tag || a.id}</span>
        </div>
      </header>
      {a.deleted_at ? (
        <p className="banner banner-bad">Este activo está en la <Link to="/inventario/papelera">papelera</Link>.</p>
      ) : (
        <AssetPanel asset={a} />
      )}
    </>
  );
}

export function NewAsset() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  async function save(body: AssetBody) {
    const created = await api<Asset>("POST", "/assets", body);
    void queryClient.invalidateQueries({ queryKey: ["assets"] });
    navigate(`/inventario/${created.id}`, { replace: true });
  }
  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/inventario">Inventario</Link></p>
      <h1>Nuevo activo</h1>
      <p className="muted">Para monitores, móviles, impresoras, material de red y todo lo que no lleva agente. Si das de alta un portátil con su número de serie, al inscribirlo con el agente se unirá a esta ficha.</p>
      <div className="panel"><AssetForm onSave={save} onCancel={() => navigate("/inventario")} /></div>
    </>
  );
}
