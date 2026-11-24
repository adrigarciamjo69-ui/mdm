import { useEffect, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, query } from "../api/client";
import type { Asset, Delivery, DeliveryItem, DeliverySettings, DirectoryUser, Page, PersonRef } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import Pagination from "../components/Pagination";
import EmptyState from "../components/EmptyState";
import PersonPicker from "../components/PersonPicker";
import ConfirmDialog from "../components/ConfirmDialog";
import { useDebounced } from "../lib/hooks";
import { formatDate } from "../lib/format";
import type { PdfRecord } from "../lib/deliveryPdf";

const TYPE_LABEL = { entrega: "Entrega", devolucion: "Devolución" } as const;
const STATUS_LABEL = { borrador: "Borrador", confirmada: "Confirmada", anulada: "Anulada" } as const;
const STATUS_CLASS = { borrador: "status status-inactive", confirmada: "status status-online", anulada: "status status-revoked" } as const;

function useSettings() {
  return useQuery({ queryKey: ["delivery-settings"], queryFn: () => api<DeliverySettings>("GET", "/delivery-settings") });
}

export default function Deliveries() {
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
  const q = query({ q: params.get("q"), type: params.get("type"), status: params.get("status"), page: page > 1 ? page : null });
  const list = useQuery({ queryKey: ["deliveries", q], queryFn: () => api<Page<Delivery>>("GET", "/deliveries" + q), placeholderData: keepPreviousData });

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Doc. Entrega</h1>
          <p className="muted">Actas de entrega y devolución de material. Al confirmar un acta, sus activos cambian de persona y de estado.</p>
        </div>
        {can("operator") && (
          <div className="page-actions">
            <Link className="btn btn-primary" to="/actas/nueva?tipo=entrega">Nueva acta</Link>
            {can("admin") && <Link className="btn btn-quiet" to="/ajustes/inventario">Datos del PDF</Link>}
          </div>
        )}
      </div>
      <div className="toolbar" role="search">
        <input type="search" aria-label="Buscar" placeholder="Buscar por número o persona" value={search} onChange={(e) => setSearch(e.target.value)} />
        <select aria-label="Tipo" value={params.get("type") ?? ""} onChange={(e) => set("type", e.target.value)}>
          <option value="">Entregas y devoluciones</option><option value="entrega">Entregas</option><option value="devolucion">Devoluciones</option>
        </select>
        <select aria-label="Estado" value={params.get("status") ?? ""} onChange={(e) => set("status", e.target.value)}>
          <option value="">Todos los estados</option><option value="borrador">Borradores</option><option value="confirmada">Confirmadas</option><option value="anulada">Anuladas</option>
        </select>
      </div>
      {list.error ? <p className="form-error" role="alert">{list.error instanceof ApiError ? list.error.message : "No se ha podido cargar la lista."}</p>
        : !list.data ? <p className="muted">Cargando…</p>
          : list.data.total === 0 ? <EmptyState message="No hay actas que coincidan." />
            : (
              <>
                <div className="table-wrap">
                  <table>
                    <caption className="visually-hidden">Actas</caption>
                    <thead><tr><th scope="col">Acta</th><th scope="col">Tipo</th><th scope="col">Persona</th><th scope="col">Fecha</th><th scope="col" className="num">Material</th><th scope="col">Estado</th></tr></thead>
                    <tbody>
                      {list.data.items.map((d) => (
                        <tr key={d.id}>
                          <td><Link className="row-link" to={`/actas/${d.id}`}>{d.number || "Borrador"}</Link></td>
                          <td>{TYPE_LABEL[d.type]}</td>
                          <td><Link to={`/usuarios/${d.user.id}`}>{d.user.name}</Link></td>
                          <td className="nowrap">{formatDate(d.date)}</td>
                          <td className="num">{d.items.length}</td>
                          <td><span className={STATUS_CLASS[d.status]}>{STATUS_LABEL[d.status]}</span></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <Pagination page={list.data.page} perPage={list.data.per_page} total={list.data.total} onChange={(p) => set("page", p > 1 ? String(p) : "")} />
              </>
            )}
    </>
  );
}

const emptyLine = (): DeliveryItem => ({ asset_id: null, category: "", brand: "", model: "", serial: "", observations: "" });
const lineFromAsset = (a: Asset): DeliveryItem => ({ asset_id: a.id, category: a.category, brand: a.brand, model: a.model, serial: a.serial, observations: "" });
const today = () => new Date().toISOString().slice(0, 10);

/** Nueva acta (/actas/nueva) o un acta existente (/actas/:id). */
export function DeliveryEdit() {
  const { id } = useParams();
  const isNew = !id;
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { can } = useAuth();
  const settings = useSettings();
  const existing = useQuery({ queryKey: ["delivery", id], queryFn: () => api<Delivery>("GET", `/deliveries/${id}`), enabled: !isNew });
  const personParam = params.get("persona");
  const preset = useQuery({
    queryKey: ["directory-user", personParam],
    queryFn: () => api<DirectoryUser & { assets: Asset[] }>("GET", `/directory/users/${personParam}`),
    enabled: isNew && !!personParam,
  });

  const [type, setType] = useState<Delivery["type"]>(params.get("tipo") === "devolucion" ? "devolucion" : "entrega");
  const [person, setPerson] = useState<PersonRef | null>(null);
  const [dni, setDni] = useState("");
  const [date, setDate] = useState(today());
  const [responsible, setResponsible] = useState("");
  const [notes, setNotes] = useState("");
  const [lines, setLines] = useState<DeliveryItem[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [dialog, setDialog] = useState<"confirm" | "cancel" | "delete" | null>(null);
  const [reason, setReason] = useState("");

  // Carga inicial: del acta guardada o de la persona indicada en la URL.
  useEffect(() => {
    if (loaded) return;
    const d = existing.data;
    if (d) {
      setType(d.type);
      setPerson({ id: d.user.id, name: d.user.name, email: d.user.email });
      setDni(d.recipient_dni);
      setDate(d.date);
      setResponsible(d.responsible);
      setNotes(d.notes);
      setLines(d.items.map((it) => ({ ...it })));
      setLoaded(true);
    } else if (isNew && preset.data) {
      const u = preset.data;
      setPerson({ id: u.id, name: u.display_name, email: u.email });
      if (type === "devolucion") setLines(u.assets.map(lineFromAsset));
      setLoaded(true);
    } else if (isNew && !personParam) {
      setLoaded(true);
    }
  }, [existing.data, preset.data, isNew, loaded, personParam, type]);

  const d = existing.data;
  const editable = isNew || d?.status === "borrador";
  const canEdit = editable && can("operator");

  if (!isNew && existing.error) return <p className="form-error" role="alert">{existing.error instanceof ApiError ? existing.error.message : "No se ha podido cargar el acta."}</p>;
  if (!isNew && !d) return <p className="muted">Cargando…</p>;

  const pdfRecord = (): PdfRecord => ({
    number: d?.number ?? "", type, date, responsible, recipient_dni: dni, user: { name: person?.name ?? "" },
    items: lines.filter((l) => l.asset_id || l.category || l.model || l.brand),
  });
  async function pdf() {
    if (!settings.data) return setError("Aún se están cargando los datos de la empresa para el PDF.");
    // jsPDF pesa: se carga solo cuando hace falta.
    const { downloadDeliveryPdf } = await import("../lib/deliveryPdf");
    downloadDeliveryPdf(pdfRecord(), settings.data);
  }

  const body = () => ({
    type, user_id: person?.id ?? "", recipient_dni: dni.trim(), date, responsible: responsible.trim(), notes: notes.trim(),
    items: lines.filter((l) => l.asset_id || l.category.trim() || l.model.trim() || l.brand.trim())
      .map((l) => ({ asset_id: l.asset_id, category: l.category.trim(), brand: l.brand.trim(), model: l.model.trim(), serial: l.serial.trim(), observations: l.observations.trim() })),
  });

  async function save(): Promise<Delivery | null> {
    setError("");
    if (!person) {
      setError("Elige la persona que recibe o devuelve el material.");
      return null;
    }
    setBusy(true);
    try {
      const saved = isNew ? await api<Delivery>("POST", "/deliveries", body()) : await api<Delivery>("PATCH", `/deliveries/${id}`, body());
      void queryClient.invalidateQueries({ queryKey: ["deliveries"] });
      queryClient.setQueryData(["delivery", saved.id], saved);
      if (isNew) navigate(`/actas/${saved.id}`, { replace: true });
      return saved;
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido guardar el acta.");
      return null;
    } finally {
      setBusy(false);
    }
  }

  async function confirm() {
    const saved = await save();
    if (!saved) {
      setDialog(null);
      return;
    }
    const c = await api<Delivery>("POST", `/deliveries/${saved.id}/confirm`);
    queryClient.setQueryData(["delivery", c.id], c);
    void queryClient.invalidateQueries({ queryKey: ["deliveries"] });
    void queryClient.invalidateQueries({ queryKey: ["assets"] });
    setDialog(null);
  }

  async function cancel() {
    const c = await api<Delivery>("POST", `/deliveries/${id}/cancel`, { reason: reason.trim() });
    queryClient.setQueryData(["delivery", c.id], c);
    void queryClient.invalidateQueries({ queryKey: ["deliveries"] });
    setDialog(null);
  }

  async function remove() {
    await api("DELETE", `/deliveries/${id}`);
    void queryClient.invalidateQueries({ queryKey: ["deliveries"] });
    navigate("/actas", { replace: true });
  }

  const setLine = (i: number, k: keyof DeliveryItem, v: string) => setLines((ls) => ls.map((l, j) => (j === i ? { ...l, [k]: v } : l)));

  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/actas">Doc. Entrega</Link></p>
      <div className="page-head">
        <div>
          <h1>{isNew ? `Nueva acta de ${type === "entrega" ? "entrega" : "devolución"}` : `${TYPE_LABEL[d!.type]} ${d!.number || "(borrador)"}`}</h1>
          {d && <p><span className={STATUS_CLASS[d.status]}>{STATUS_LABEL[d.status]}</span>{d.cancel_reason && <span className="muted"> · {d.cancel_reason}</span>}</p>}
        </div>
        <div className="page-actions">
          {editable ? <button className="btn" onClick={() => void pdf()}>Vista previa PDF</button> : <button className="btn btn-primary" onClick={() => void pdf()}>Descargar PDF</button>}
          {d?.status === "confirmada" && can("admin") && <button className="btn btn-danger-outline" onClick={() => setDialog("cancel")}>Anular</button>}
          {d?.status === "borrador" && can("admin") && <button className="btn btn-quiet" onClick={() => setDialog("delete")}>Eliminar borrador</button>}
        </div>
      </div>

      <div className="panel">
        <div className="edit-grid">
          <label className="field"><span>Tipo</span>
            <select value={type} onChange={(e) => setType(e.target.value as Delivery["type"])} disabled={!canEdit}>
              <option value="entrega">Entrega</option><option value="devolucion">Devolución</option>
            </select>
          </label>
          {canEdit ? <PersonPicker label="Persona" value={person} onChange={setPerson} required />
            : <div className="field"><span>Persona</span><span>{person && <Link to={`/usuarios/${person.id}`}>{person.name}</Link>}</span></div>}
          <label className="field"><span>DNI / NIE del receptor</span><input value={dni} onChange={(e) => setDni(e.target.value)} maxLength={20} disabled={!canEdit} /></label>
          <label className="field"><span>Fecha</span><input type="date" value={date} onChange={(e) => setDate(e.target.value)} disabled={!canEdit} /></label>
          <label className="field"><span>Responsable (firma empresa) <span className="hint">vacío = tu correo</span></span>
            <input value={responsible} onChange={(e) => setResponsible(e.target.value)} maxLength={200} disabled={!canEdit} /></label>
        </div>
        <label className="field"><span>Notas internas <span className="hint">no aparecen en el PDF</span></span>
          <textarea rows={2} style={{ width: "100%" }} value={notes} onChange={(e) => setNotes(e.target.value)} disabled={!canEdit} /></label>
      </div>

      <section className="panel">
        <h2>Material</h2>
        {lines.length === 0 && <p className="muted">Añade los activos del inventario o líneas sueltas (un cargador, un ratón…).</p>}
        {lines.length > 0 && (
          <div className="table-wrap delivery-lines">
            <table>
              <thead><tr><th scope="col">Dispositivo</th><th scope="col">Marca</th><th scope="col">Modelo</th><th scope="col">Número de serie</th><th scope="col">Observaciones</th><th scope="col"><span className="visually-hidden">Quitar</span></th></tr></thead>
              <tbody>
                {lines.map((l, i) => (
                  <tr key={i}>
                    {(["category", "brand", "model", "serial"] as const).map((k) => (
                      <td key={k}>
                        {l.asset_id || !canEdit ? <input value={l[k]} readOnly aria-label={`${{ category: "Dispositivo", brand: "Marca", model: "Modelo", serial: "Serie" }[k]} de la línea ${i + 1}`} />
                          : <input value={l[k]} onChange={(e) => setLine(i, k, e.target.value)} maxLength={200}
                            aria-label={`${{ category: "Dispositivo", brand: "Marca", model: "Modelo", serial: "Serie" }[k]} de la línea ${i + 1}`} />}
                      </td>
                    ))}
                    <td><input value={l.observations} onChange={(e) => setLine(i, "observations", e.target.value)} readOnly={!canEdit} maxLength={200} aria-label={`Observaciones de la línea ${i + 1}`} /></td>
                    <td>{canEdit && <button className="btn btn-quiet btn-small" onClick={() => setLines((ls) => ls.filter((_, j) => j !== i))} aria-label={`Quitar la línea ${i + 1}`}>✕</button>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {canEdit && (
          <div className="edit-grid" style={{ marginTop: "1rem" }}>
            <AssetAdder type={type} onAdd={(a) => setLines((ls) => (ls.some((l) => l.asset_id === a.id) ? ls : [...ls, lineFromAsset(a)]))} />
            <div className="field"><span>&nbsp;</span><button type="button" className="btn" onClick={() => setLines((ls) => [...ls, emptyLine()])}>Añadir línea sin activo</button></div>
          </div>
        )}
      </section>

      {error && <p className="form-error" role="alert">{error}</p>}
      {canEdit && (
        <div className="dialog-actions" style={{ justifyContent: "flex-start", marginTop: "1rem" }}>
          <button className="btn" onClick={() => void save()} disabled={busy}>Guardar borrador</button>
          <button className="btn btn-primary" onClick={() => setDialog("confirm")} disabled={busy || !person || lines.length === 0}>Confirmar acta</button>
        </div>
      )}

      {dialog === "confirm" && (
        <ConfirmDialog title="Confirmar el acta"
          body={type === "entrega"
            ? `Se le dará número definitivo y los activos del inventario pasarán a ser de ${person?.name ?? "la persona"}, en uso. Después no se puede editar.`
            : "Se le dará número definitivo y los activos del inventario quedarán sin persona y en almacén. Después no se puede editar."}
          confirmLabel="Confirmar" onConfirm={confirm} onCancel={() => setDialog(null)} />
      )}
      {dialog === "delete" && <ConfirmDialog title="Eliminar este borrador" body="El borrador desaparece. No afecta a ningún activo." confirmLabel="Eliminar" danger onConfirm={remove} onCancel={() => setDialog(null)} />}
      {dialog === "cancel" && (
        <div className="dialog-backdrop">
          <div className="confirm" role="dialog" aria-modal="true" aria-labelledby="cancel-title">
            <h2 id="cancel-title">Anular el acta {d?.number}</h2>
            <p>El acta queda como anulada. Los cambios que hizo en los activos no se deshacen: corrígelos a mano o con otra acta.</p>
            <label className="field"><span>Motivo</span><input value={reason} onChange={(e) => setReason(e.target.value)} maxLength={200} /></label>
            <div className="dialog-actions">
              <button className="btn" onClick={() => setDialog(null)}>Cancelar</button>
              <button className="btn btn-danger" disabled={!reason.trim()} onClick={() => void cancel()}>Anular</button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

/** Buscador de activos para añadir al acta (en una entrega, primero los de almacén). */
function AssetAdder({ type, onAdd }: { type: Delivery["type"]; onAdd(a: Asset): void }) {
  const [text, setText] = useState("");
  const q = useDebounced(text.trim(), 250);
  const res = useQuery({
    queryKey: ["asset-search", type, q],
    queryFn: () => api<Page<Asset>>("GET", "/assets" + query({ q, per_page: 8, status: type === "entrega" && !q ? "en_almacen" : null })),
    enabled: q.length >= 2,
  });
  return (
    <div className="field person-picker">
      <label htmlFor="add-asset">Añadir activo del inventario</label>
      <input id="add-asset" value={text} onChange={(e) => setText(e.target.value)} placeholder="Nombre, número de serie o etiqueta…" autoComplete="off" />
      {q.length >= 2 && res.data && (
        <ul className="picker-list" role="listbox">
          {res.data.items.length === 0 && <li className="muted small">Ningún activo coincide.</li>}
          {res.data.items.map((a) => (
            <li key={a.id} role="option" aria-selected={false}>
              <button type="button" onClick={() => { onAdd(a); setText(""); }}>
                {a.name} <span className="muted small">{a.category}{a.serial ? ` · ${a.serial}` : ""}{a.owner ? ` · ${a.owner.name}` : " · en almacén"}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
