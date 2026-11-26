import { useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { Asset, AssetStatus, Category, PersonRef } from "../api/types";
import { ASSET_STATUS_LABEL } from "../lib/inventory";
import PersonPicker from "./PersonPicker";

export interface AssetBody {
  category_id: string;
  name: string;
  brand: string;
  model: string;
  serial: string;
  asset_tag: string;
  status: AssetStatus;
  owner_id: string;
  department: string;
  location: string;
  price: number | null;
  purchase_date: string;
  warranty_until: string;
  purchase_order: string;
  supplier: string;
  notes: string;
  owner_locked: boolean;
  department_locked: boolean;
}

function initial(a?: Asset): AssetBody {
  return {
    category_id: a?.category_id ?? "", name: a?.name ?? "", brand: a?.brand ?? "", model: a?.model ?? "", serial: a?.serial ?? "",
    asset_tag: a?.asset_tag ?? "", status: a?.status ?? "en_almacen", owner_id: a?.owner?.id ?? "", department: a?.department ?? "",
    location: a?.location ?? "", price: a?.price ?? null, purchase_date: a?.purchase_date ?? "", warranty_until: a?.warranty_until ?? "",
    purchase_order: a?.purchase_order ?? "", supplier: a?.supplier ?? "", notes: a?.notes ?? "",
    owner_locked: a?.owner_locked ?? false, department_locked: a?.department_locked ?? false,
  };
}

/** Formulario de alta y edición. onSave recibe el cuerpo ya validado. */
export default function AssetForm({ asset, onSave, onCancel, saveLabel = "Guardar" }: {
  asset?: Asset;
  onSave(body: AssetBody): Promise<void>;
  onCancel?(): void;
  saveLabel?: string;
}) {
  const cats = useQuery({ queryKey: ["asset-categories"], queryFn: () => api<Category[]>("GET", "/asset-categories") });
  const [f, setF] = useState<AssetBody>(() => initial(asset));
  const [owner, setOwner] = useState<PersonRef | null>(asset?.owner ?? null);
  const [price, setPrice] = useState(asset?.price != null ? String(asset.price).replace(".", ",") : "");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = <K extends keyof AssetBody>(k: K, v: AssetBody[K]) => setF((p) => ({ ...p, [k]: v }));
  const agent = asset?.origin === "agent";
  // Sin bloquear, el departamento es el de la persona en Entra ID (si lo tiene).
  const deptFollows = !!owner?.department && !f.department_locked;

  function changeOwner(p: PersonRef | null) {
    setOwner(p);
    // Elegir a mano la persona de un equipo con agente la bloquea: si no, la
    // siguiente sesión la cambiaría. Se puede desmarcar.
    if (agent) set("owner_locked", true);
  }

  function toggleDeptLock(locked: boolean) {
    // Al bloquear se parte del departamento que se estaba mostrando.
    setF((p) => ({ ...p, department_locked: locked, department: locked && owner?.department && !p.department_locked ? owner.department : p.department }));
  }

  const lock = (k: "owner_locked" | "department_locked", onToggle: (v: boolean) => void) => (
    <label className="lock-toggle" title={k === "owner_locked"
      ? "Bloqueada: no cambia aunque inicie sesión otra persona en el equipo."
      : "Bloqueado: no sigue al departamento de la persona en Entra ID."}>
      <input type="checkbox" checked={f[k]} onChange={(e) => onToggle(e.target.checked)} /> Bloquear
    </label>
  );

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    if (!f.category_id) return setError("Elige una categoría.");
    if (!f.name.trim()) return setError("Escribe un nombre para el activo.");
    let p: number | null = null;
    if (price.trim()) {
      p = Number(price.replace(/\s|€/g, "").replace(",", "."));
      if (!Number.isFinite(p) || p < 0) return setError("El precio debe ser un número, por ejemplo 199,90.");
    }
    setBusy(true);
    try {
      await onSave({ ...f, name: f.name.trim(), price: p, owner_id: owner?.id ?? "", department: deptFollows ? owner!.department! : f.department });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido guardar el activo.");
    } finally {
      setBusy(false);
    }
  }

  const text = (k: keyof AssetBody, label: string, opts: { mono?: boolean; disabled?: boolean } = {}) => (
    <label className="field">
      <span>{label}</span>
      <input value={f[k] as string} onChange={(e) => set(k, e.target.value as never)} maxLength={200} disabled={opts.disabled}
        className={opts.mono ? "mono" : undefined} />
    </label>
  );

  return (
    <form className="asset-form" onSubmit={submit} noValidate>
      <div className="edit-grid">
        <label className="field">
          <span>Categoría</span>
          <select value={f.category_id} onChange={(e) => set("category_id", e.target.value)}>
            <option value="">Elige…</option>
            {cats.data?.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
        </label>
        {text("name", "Nombre")}
        <label className="field">
          <span>Estado</span>
          <select value={f.status} onChange={(e) => set("status", e.target.value as AssetStatus)}>
            {(Object.keys(ASSET_STATUS_LABEL) as AssetStatus[]).map((s) => <option key={s} value={s}>{ASSET_STATUS_LABEL[s]}</option>)}
          </select>
        </label>
        {text("brand", "Marca")}
        {text("model", "Modelo")}
        {text("serial", "Número de serie", { mono: true })}
        {text("asset_tag", "Etiqueta interna", { mono: true })}
        <div className="field-with-lock">
          <PersonPicker label="Persona" value={owner} onChange={changeOwner} />
          {agent && lock("owner_locked", (v) => set("owner_locked", v))}
          {agent && !f.owner_locked && <span className="hint small">Sigue al usuario con sesión en el equipo</span>}
        </div>
        <div className="field-with-lock">
          {deptFollows ? (
            <label className="field">
              <span>Departamento <span className="hint">el de la persona en Entra ID</span></span>
              <input value={owner!.department} disabled />
            </label>
          ) : text("department", "Departamento")}
          {(agent || owner?.department) && lock("department_locked", toggleDeptLock)}
        </div>
        {text("location", "Ubicación")}
      </div>
      <fieldset className="params">
        <legend>Compra</legend>
        <div className="edit-grid">
          <label className="field"><span>Precio (€)</span><input inputMode="decimal" value={price} onChange={(e) => setPrice(e.target.value)} /></label>
          <label className="field"><span>Fecha de compra</span><input type="date" value={f.purchase_date} onChange={(e) => set("purchase_date", e.target.value)} /></label>
          <label className="field"><span>Garantía hasta</span><input type="date" value={f.warranty_until} onChange={(e) => set("warranty_until", e.target.value)} /></label>
          {text("purchase_order", "Pedido de compra")}
          {text("supplier", "Proveedor")}
        </div>
      </fieldset>
      <label className="field">
        <span>Notas</span>
        <textarea rows={3} value={f.notes} onChange={(e) => set("notes", e.target.value)} maxLength={4000} />
      </label>
      {agent && <p className="muted small">Los datos técnicos (sistema, hardware, software) los pone el agente; aquí se guardan los de inventario.</p>}
      {error && <p className="form-error" role="alert">{error}</p>}
      <div className="dialog-actions" style={{ justifyContent: "flex-start" }}>
        <button className="btn btn-primary" disabled={busy}>{saveLabel}</button>
        {onCancel && <button type="button" className="btn" onClick={onCancel} disabled={busy}>Cancelar</button>}
      </div>
    </form>
  );
}
