import { useId, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { Asset } from "../api/types";
import { ASSET_STATUS_LABEL, formatPrice, isJunkSerial } from "../lib/inventory";

type Field = "category" | "name" | "brand" | "model" | "serial" | "asset_tag" | "status" | "owner" | "department" | "location"
  | "price" | "purchase_date" | "warranty_until" | "purchase_order" | "supplier" | "notes";

const FIELDS: [Field, string, (a: Asset) => string][] = [
  ["category", "Categoría", (a) => a.category],
  ["name", "Nombre", (a) => a.name],
  ["brand", "Marca", (a) => a.brand],
  ["model", "Modelo", (a) => a.model],
  ["serial", "Número de serie", (a) => a.serial],
  ["asset_tag", "Etiqueta interna", (a) => a.asset_tag],
  ["status", "Estado", (a) => ASSET_STATUS_LABEL[a.status]],
  ["owner", "Persona", (a) => a.owner?.name ?? ""],
  ["department", "Departamento", (a) => a.department],
  ["location", "Ubicación", (a) => a.location],
  ["price", "Precio", (a) => (a.price == null ? "" : formatPrice(a.price))],
  ["purchase_date", "Fecha de compra", (a) => a.purchase_date],
  ["warranty_until", "Garantía hasta", (a) => a.warranty_until],
  ["purchase_order", "Pedido", (a) => a.purchase_order],
  ["supplier", "Proveedor", (a) => a.supplier],
  ["notes", "Notas", (a) => a.notes],
];

/**
 * Por defecto, el valor del conservado; si lo tiene vacío (o es un número de
 * serie de relleno como «Default string»), el del otro.
 */
function defaults(keep: Asset, other: Asset): Record<Field, "keep" | "other"> {
  const out = {} as Record<Field, "keep" | "other">;
  for (const [f, , get] of FIELDS) {
    const k = get(keep), o = get(other);
    const useless = !k || (f === "serial" && isJunkSerial(k) && !isJunkSerial(o));
    out[f] = useless && o ? "other" : "keep";
  }
  return out;
}

/** Fusiona dos activos que son el mismo objeto eligiendo cada campo. */
export default function MergeDialog({ a, b, onClose, onDone }: { a: Asset; b: Asset; onClose(): void; onDone(id: string): void }) {
  const titleId = useId();
  const qc = useQueryClient();
  const bothAgents = !!a.device_id && !!b.device_id;
  const [keepId, setKeepId] = useState(!a.device_id && b.device_id ? b.id : a.id);
  const keep = keepId === a.id ? a : b;
  const other = keepId === a.id ? b : a;
  const [choice, setChoice] = useState(() => defaults(keep, other));
  const [notesBoth, setNotesBoth] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  function changeKeep(id: string) {
    setKeepId(id);
    const k = id === a.id ? a : b;
    setChoice(defaults(k, k === a ? b : a));
  }

  async function submit() {
    setError("");
    setBusy(true);
    const fromOther = FIELDS.filter(([f, , get]) => choice[f] === "other" && get(keep) !== get(other) && !(f === "notes" && notesBoth)).map(([f]) => f);
    try {
      const r = await api<Asset>("POST", "/assets/merge", { keep_id: keep.id, other_id: other.id, from_other: fromOther, notes_both: notesBoth });
      await qc.invalidateQueries({ queryKey: ["assets"] });
      onDone(r.id);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "No se han podido fusionar.");
      setBusy(false);
    }
  }

  const show = (v: string) => v || "—";
  return (
    <div className="dialog-backdrop">
      <div className="confirm" role="dialog" aria-modal="true" aria-labelledby={titleId} style={{ width: "min(52rem, 100%)" }}>
        <h2 id={titleId}>Fusionar dos activos</h2>
        {bothAgents ? (
          <p className="form-error" role="alert">
            Son dos equipos con agente: para el MDM son equipos distintos y no se pueden fusionar. Si es el mismo equipo inscrito dos veces,
            elimina el que sobra desde su ficha.
          </p>
        ) : (
          <p className="muted small">
            Queda una sola ficha con los valores que elijas. Los documentos, las licencias y las actas pasan a ella, y la otra va a la papelera.
            {(a.device_id || b.device_id) && " El equipo con agente se queda con la ficha resultante."}
          </p>
        )}
        <fieldset className="params">
          <legend>Ficha que se conserva</legend>
          {[a, b].map((x) => (
            <label key={x.id} className="check-field" style={{ marginBottom: "0.4rem" }}>
              <input type="radio" name="keep" checked={keepId === x.id} onChange={() => changeKeep(x.id)} disabled={bothAgents} />
              Conservar la ficha de «{x.name}»{x.device_id ? " (con agente)" : " (manual)"}
            </label>
          ))}
        </fieldset>
        <div className="table-wrap">
          <table className="merge-table">
            <thead><tr><th scope="col">Campo</th><th scope="col">«{keep.name}» {keep.device_id ? "(con agente)" : "(manual)"} · se conserva</th><th scope="col">«{other.name}» {other.device_id ? "(con agente)" : "(manual)"}</th></tr></thead>
            <tbody>
              {FIELDS.map(([f, label, get]) => {
                const kv = get(keep), ov = get(other);
                const same = kv === ov;
                const disabled = bothAgents || (f === "notes" && notesBoth);
                return (
                  <tr key={f}>
                    <th scope="row">{label}</th>
                    {same ? (
                      <td colSpan={2} className="muted">{show(kv)}</td>
                    ) : (
                      (["keep", "other"] as const).map((side) => {
                        const v = side === "keep" ? kv : ov;
                        return (
                          <td key={side}>
                            <label className="check-field">
                              <input type="radio" name={`f-${f}`} aria-label={`${label}: ${show(v)}`} checked={choice[f] === side} disabled={disabled}
                                onChange={() => setChoice({ ...choice, [f]: side })} />
                              <span style={f === "notes" ? { whiteSpace: "pre-wrap" } : undefined}>{show(v)}
                                {f === "serial" && v && isJunkSerial(v) && <span className="hint small"> (de relleno)</span>}</span>
                            </label>
                          </td>
                        );
                      })
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        {keep.notes && other.notes && keep.notes !== other.notes && (
          <label className="check-field">
            <input type="checkbox" checked={notesBoth} onChange={(e) => setNotesBoth(e.target.checked)} disabled={bothAgents} />Juntar las notas de los dos
          </label>
        )}
        <p className="muted small">Si la persona elegida tiene departamento en Entra ID, el activo toma ese departamento.</p>
        {error && <p className="form-error" role="alert">{error}</p>}
        <div className="dialog-actions">
          <button className="btn" onClick={onClose} disabled={busy}>Cancelar</button>
          <button className="btn btn-primary" onClick={() => void submit()} disabled={busy || bothAgents}>Fusionar</button>
        </div>
      </div>
    </div>
  );
}
