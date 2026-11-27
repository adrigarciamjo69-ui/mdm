import { useEffect, useMemo, useRef, useState } from "react";
import { Link, Navigate, useNavigate, useSearchParams } from "react-router-dom";
import { keepPreviousData, useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, query } from "../api/client";
import type { Asset, AssetStatus, Category, Group, Page, PersonRef } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import DataTable, { type Column } from "../components/DataTable";
import StatusBadge from "../components/StatusBadge";
import EmptyState from "../components/EmptyState";
import ActionBar from "../components/ActionBar";
import MergeDialog from "../components/MergeDialog";
import PersonPicker from "../components/PersonPicker";
import { useDebounced } from "../lib/hooks";
import { ASSET_STATUS_CLASS, ASSET_STATUS_LABEL, ISSUE_LABELS } from "../lib/inventory";

const PER_PAGE = 200;

const FILTERS = ["q", "category_id", "status", "origin", "owner_id", "department", "platform", "device_status", "group_id", "compliance", "issue", "os_version", "tag"] as const;

/** Los enlaces antiguos a /dispositivos?… llevan al inventario de equipos con agente con los mismos filtros. */
export function DevicesRedirect() {
  const [params] = useSearchParams();
  const next = new URLSearchParams({ origin: "agent" });
  params.forEach((v, k) => next.set(k === "status" ? "device_status" : k, v));
  return <Navigate to={`/inventario?${next.toString()}`} replace />;
}

export const assetLink = (a: Asset) => (a.device_id ? `/dispositivos/${a.device_id}` : `/inventario/${a.id}`);

const columns: Column<Asset>[] = [
  { key: "name", label: "Nombre", sort: "name", render: (a) => <Link className="row-link" to={assetLink(a)}>{a.name}</Link> },
  { key: "category", label: "Categoría", sort: "category", render: (a) => a.category },
  { key: "model", label: "Marca y modelo", render: (a) => [a.brand, a.model].filter(Boolean).join(" ") || <span className="muted">—</span> },
  { key: "serial", label: "Número de serie", render: (a) => (a.serial ? <span className="mono">{a.serial}</span> : <span className="muted">—</span>) },
  { key: "owner", label: "Persona", sort: "owner", render: (a) => (a.owner ? <Link to={`/usuarios/${a.owner.id}`}>{a.owner.name}</Link> : <span className="muted">—</span>) },
  { key: "department", label: "Departamento", sort: "department", render: (a) => a.department || <span className="muted">—</span> },
  { key: "status", label: "Estado", sort: "status", render: (a) => <span className={ASSET_STATUS_CLASS[a.status]}>{ASSET_STATUS_LABEL[a.status]}</span> },
  {
    key: "origin", label: "Origen",
    render: (a) => (a.device ? <span title={`Agente ${a.device.agent_version}`}><StatusBadge status={a.device.status} /></span> : <span className="muted">Manual</span>),
  },
];

export default function Inventory() {
  const { can } = useAuth();
  const [params, setParams] = useSearchParams();
  const [selected, setSelected] = useState<Map<string, Asset>>(new Map());
  const [bulk, setBulk] = useState(false);
  const [merge, setMerge] = useState(false);
  const navigate = useNavigate();
  const [search, setSearch] = useState(params.get("q") ?? "");
  const debounced = useDebounced(search.trim(), 300);

  const update = (changes: Record<string, string | null>, keepPage = false) => {
    setParams((prev) => {
      const next = new URLSearchParams(prev);
      for (const [k, v] of Object.entries(changes)) {
        if (v) next.set(k, v);
        else next.delete(k);
      }
      if (!keepPage) next.delete("page");
      return next;
    }, { replace: true });
  };
  const urlQ = params.get("q") ?? "";
  useEffect(() => {
    if (urlQ !== debounced) setSearch(urlQ);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlQ]);
  useEffect(() => {
    if (debounced !== (params.get("q") ?? "")) update({ q: debounced || null });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debounced]);

  const filters = useMemo(() => {
    const f: Record<string, string> = {};
    for (const k of FILTERS) {
      const v = params.get(k);
      if (v) f[k] = v;
    }
    return f;
  }, [params]);
  const sort = params.get("sort") ?? "name";
  const order = (params.get("order") as "asc" | "desc" | null) ?? "asc";
  const hasFilters = Object.keys(filters).length > 0;
  // Sin páginas: se cargan bloques de PER_PAGE según se baja por la lista.
  const listQuery = query({ ...filters, sort: params.get("sort"), order: params.get("order") });
  const assets = useInfiniteQuery({
    queryKey: ["assets", listQuery, "all"],
    queryFn: ({ pageParam }) => api<Page<Asset>>("GET", "/assets" + query({ ...filters, sort: params.get("sort"), order: params.get("order"), page: pageParam, per_page: PER_PAGE })),
    initialPageParam: 1,
    getNextPageParam: (last) => (last.page * last.per_page < last.total ? last.page + 1 : undefined),
    placeholderData: keepPreviousData,
    refetchInterval: 30_000,
  });
  const sentinel = useRef<HTMLDivElement>(null);
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = assets;
  useEffect(() => {
    const el = sentinel.current;
    if (!el || !hasNextPage || typeof IntersectionObserver === "undefined") return;
    const io = new IntersectionObserver((e) => {
      if (e[0]?.isIntersecting && !isFetchingNextPage) void fetchNextPage();
    }, { rootMargin: "600px" });
    io.observe(el);
    return () => io.disconnect();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);
  const cats = useQuery({ queryKey: ["asset-categories"], queryFn: () => api<Category[]>("GET", "/asset-categories") });
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });
  const departments = useQuery({ queryKey: ["asset-departments"], queryFn: () => api<string[]>("GET", "/assets/departments") });
  // Un departamento que llega por enlace y ya no tiene activos sigue apareciendo elegido.
  const deptOptions = [...new Set([...(departments.data ?? []), ...(filters.department ? [filters.department] : [])])].sort((a, b) => a.localeCompare(b, "es"));
  const pages = assets.data?.pages;
  const data = pages ? { items: pages.flatMap((p) => p.items), total: pages[0]?.total ?? 0 } : undefined;

  const toggle = (a: Asset) => setSelected((prev) => {
    const next = new Map(prev);
    if (next.has(a.id)) next.delete(a.id);
    else next.set(a.id, a);
    return next;
  });
  const agentSel = [...selected.values()].filter((a) => a.device && a.device.status !== "revoked");
  const platforms = new Set(agentSel.map((a) => a.device!.platform));

  function onSort(field: string) {
    const nextOrder = field === sort ? (order === "asc" ? "desc" : "asc") : "asc";
    const isDefault = field === "name" && nextOrder === "asc";
    update({ sort: isDefault ? null : field, order: isDefault ? null : nextOrder });
  }
  function clearFilters() {
    setSearch("");
    setParams(new URLSearchParams(), { replace: true });
  }
  const select = (name: string, label: string, opts: [string, string][], all: string) => (
    <label>
      <span className="visually-hidden">{label}</span>
      <select aria-label={label} value={filters[name] ?? ""} onChange={(e) => update({ [name]: e.target.value })}>
        <option value="">{all}</option>
        {opts.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
      </select>
    </label>
  );
  const agentOnly = filters.origin === "agent";

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Inventario</h1>
          <p className="muted">Equipos con agente y activos dados de alta a mano, en una sola lista.</p>
        </div>
        <div className="page-actions">
          {can("operator") && <Link className="btn btn-primary" to="/inventario/nuevo">Nuevo activo</Link>}
          {can("operator") && <Link className="btn" to="/inventario/importar">Importar</Link>}
          <a className="btn" href={`/api/v1/assets.csv${query({ ...filters, sort: params.get("sort"), order: params.get("order") }) || "?"}`} download>Exportar CSV</a>
          <Link className="btn btn-quiet" to="/inventario/papelera">Papelera</Link>
        </div>
      </div>

      <div className="toolbar" role="search">
        <input type="search" aria-label="Buscar" placeholder="Buscar por nombre, serie, marca, persona…" value={search} onChange={(e) => setSearch(e.target.value)} />
        {select("category_id", "Categoría", (cats.data ?? []).map((c) => [c.id, c.name]), "Todas las categorías")}
        {select("status", "Estado", Object.entries(ASSET_STATUS_LABEL), "Todos los estados")}
        {select("department", "Departamento", deptOptions.map((d) => [d, d]), "Todos los departamentos")}
        {select("origin", "Origen", [["agent", "Con agente"], ["manual", "Manuales"]], "Con agente y manuales")}
        {agentOnly && select("platform", "Plataforma", [["windows", "Windows"], ["linux", "Linux"], ["android", "Android"]], "Todas las plataformas")}
        {agentOnly && select("device_status", "Conexión", [["online", "En línea"], ["offline", "Desconectado"], ["inactive", "Inactivo"], ["revoked", "Revocado"]], "Cualquier conexión")}
        {agentOnly && select("compliance", "Cumplimiento", [["compliant", "Cumple"], ["noncompliant", "No cumple"], ["unknown", "Desconocido"]], "Cualquier cumplimiento")}
        {agentOnly && select("group_id", "Grupo", (groups.data ?? []).map((g) => [g.id, g.name]), "Todos los grupos")}
        {(filters.issue || filters.owner_id || filters.os_version || filters.tag) && (
          <span className="tag">
            {filters.issue ? ISSUE_LABELS[filters.issue] ?? filters.issue : filters.owner_id ? "De una persona"
              : filters.tag ? `Etiqueta: ${filters.tag}` : `Versión ${filters.os_version}`}{" "}
            <button className="btn-quiet btn" style={{ minHeight: 0, padding: 0 }} onClick={() => update({ issue: null, owner_id: null, os_version: null, tag: null })}
              aria-label="Quitar este filtro">✕</button>
          </span>
        )}
        {hasFilters && <button className="btn btn-quiet" onClick={clearFilters}>Quitar filtros</button>}
      </div>

      {assets.error ? (
        <p className="form-error" role="alert">{assets.error instanceof ApiError ? assets.error.message : "No se ha podido cargar el inventario."}</p>
      ) : !data ? (
        <p className="muted">Cargando inventario…</p>
      ) : data.total === 0 ? (
        <EmptyState message={hasFilters ? "Ningún activo coincide con los filtros." : "El inventario está vacío. Inscribe equipos con un token o da de alta activos a mano."}>
          {hasFilters ? <button className="btn" onClick={clearFilters}>Quitar filtros</button>
            : can("operator") && <Link className="btn btn-primary" to="/inventario/nuevo">Nuevo activo</Link>}
        </EmptyState>
      ) : (
        <>
          {selected.size > 0 && (
            <div className="action-bar" role="toolbar" aria-label="Selección">
              <span className="small"><strong>{selected.size === 1 ? "1 seleccionado" : `${selected.size} seleccionados`}</strong></span>
              {can("operator") && <button className="btn btn-small" onClick={() => setBulk(true)}>Cambiar estado, categoría o persona</button>}
              {can("admin") && selected.size === 2 && <button className="btn btn-small" onClick={() => setMerge(true)}>Fusionar</button>}
              <button className="btn btn-quiet" onClick={() => setSelected(new Map())}>Quitar selección</button>
            </div>
          )}
          {agentSel.length > 0 && (
            <ActionBar target={{
              deviceIds: agentSel.map((a) => a.device_id!),
              label: agentSel.length === 1 ? agentSel[0].name : `${agentSel.length} equipos con agente`,
              platform: platforms.size === 1 ? [...platforms][0] : undefined,
            }} label="">
              <span className="small">Órdenes para {agentSel.length === 1 ? "el equipo con agente" : `los ${agentSel.length} equipos con agente`}:</span>
            </ActionBar>
          )}
          <DataTable columns={[{
            key: "select", label: "Seleccionar", hideLabel: true, className: "col-select",
            render: (a: Asset) => <input type="checkbox" aria-label={`Seleccionar ${a.name}`} checked={selected.has(a.id)} onChange={() => toggle(a)} />,
          }, ...columns]} rows={data.items} rowKey={(a) => a.id} sort={sort} order={order} onSort={onSort}
            loading={assets.isPlaceholderData} caption="Inventario" />
          <div ref={sentinel} className="list-footer small muted" style={{ marginTop: "0.75rem", display: "flex", gap: "1rem", alignItems: "center" }}>
            <span>{data.items.length === data.total ? `${data.total} ${data.total === 1 ? "activo" : "activos"}` : `Mostrando ${data.items.length} de ${data.total}`}</span>
            {hasNextPage && (
              <button className="btn btn-small" disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
                {isFetchingNextPage ? "Cargando…" : "Cargar más"}
              </button>
            )}
          </div>
        </>
      )}
      {merge && selected.size === 2 && (
        <MergeDialog a={[...selected.values()][0]} b={[...selected.values()][1]} onClose={() => setMerge(false)}
          onDone={(id) => { setMerge(false); setSelected(new Map()); navigate(`/inventario/${id}`); }} />
      )}
      {bulk && <BulkDialog ids={[...selected.keys()]} categories={cats.data ?? []} onClose={(done) => { setBulk(false); if (done) setSelected(new Map()); }} />}
    </>
  );
}

function BulkDialog({ ids, categories, onClose }: { ids: string[]; categories: Category[]; onClose(done: boolean): void }) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<AssetStatus | "">("");
  const [category, setCategory] = useState("");
  const [owner, setOwner] = useState<PersonRef | null>(null);
  const [ownerMode, setOwnerMode] = useState<"keep" | "set" | "clear">("keep");
  const [location, setLocation] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function apply() {
    const body: Record<string, unknown> = { ids };
    if (status) body.status = status;
    if (category) body.category_id = category;
    if (ownerMode === "clear") body.owner_id = "";
    if (ownerMode === "set" && owner) body.owner_id = owner.id;
    if (location.trim()) body.location = location.trim();
    if (Object.keys(body).length === 1) return setError("Elige al menos un cambio.");
    setBusy(true);
    try {
      await api("POST", "/assets/bulk", body);
      void queryClient.invalidateQueries({ queryKey: ["assets"] });
      onClose(true);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se han podido aplicar los cambios.");
      setBusy(false);
    }
  }

  return (
    <div className="dialog-backdrop">
      <div className="confirm" role="dialog" aria-modal="true" aria-labelledby="bulk-title">
        <h2 id="bulk-title">Cambiar {ids.length === 1 ? "1 activo" : `${ids.length} activos`}</h2>
        <p className="muted small">Solo se cambia lo que elijas; lo demás queda como está.</p>
        <label className="field"><span>Estado</span>
          <select value={status} onChange={(e) => setStatus(e.target.value as AssetStatus)}>
            <option value="">No cambiar</option>
            {(Object.keys(ASSET_STATUS_LABEL) as AssetStatus[]).map((s) => <option key={s} value={s}>{ASSET_STATUS_LABEL[s]}</option>)}
          </select>
        </label>
        <label className="field"><span>Categoría</span>
          <select value={category} onChange={(e) => setCategory(e.target.value)}>
            <option value="">No cambiar</option>
            {categories.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
        </label>
        <label className="field"><span>Persona</span>
          <select value={ownerMode} onChange={(e) => setOwnerMode(e.target.value as "keep" | "set" | "clear")}>
            <option value="keep">No cambiar</option>
            <option value="set">Asignar a…</option>
            <option value="clear">Quitar la persona</option>
          </select>
        </label>
        {ownerMode === "set" && <PersonPicker label="Asignar a" value={owner} onChange={setOwner} required />}
        <label className="field"><span>Ubicación</span><input value={location} onChange={(e) => setLocation(e.target.value)} placeholder="No cambiar" /></label>
        {error && <p className="form-error" role="alert">{error}</p>}
        <div className="dialog-actions">
          <button className="btn" onClick={() => onClose(false)} disabled={busy}>Cancelar</button>
          <button className="btn btn-primary" onClick={() => void apply()} disabled={busy}>Aplicar</button>
        </div>
      </div>
    </div>
  );
}
