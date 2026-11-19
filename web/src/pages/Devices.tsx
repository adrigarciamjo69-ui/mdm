import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api, ApiError, query } from "../api/client";
import type { Device, Group, Page } from "../api/types";
import DataTable, { type Column } from "../components/DataTable";
import Pagination from "../components/Pagination";
import StatusBadge from "../components/StatusBadge";
import ComplianceBadge from "../components/ComplianceBadge";
import EmptyState from "../components/EmptyState";
import NoEnrollments from "../components/NoEnrollments";
import { useDebounced } from "../lib/hooks";
import ActionBar from "../components/ActionBar";
import { formatDateTime, relativeTime } from "../lib/format";

const FILTERS = ["q", "platform", "status", "group_id", "os_version", "tag", "issue", "compliance"] as const;

export const ISSUE_LABELS: Record<string, string> = {
  low_disk: "Poco espacio en disco",
  unencrypted: "Sin cifrar",
  stale: "Sin conexión reciente",
};

// Orden por defecto de cada columna al pulsarla por primera vez.
const DEFAULT_ORDER: Record<string, "asc" | "desc"> = { hostname: "asc", last_seen_at: "desc", os_version: "asc", enrolled_at: "desc" };

const columns: Column<Device>[] = [
  { key: "hostname", label: "Nombre", sort: "hostname", render: (d) => <Link className="row-link" to={`/dispositivos/${d.id}`}>{d.hostname}</Link> },
  { key: "os", label: "Sistema", sort: "os_version", render: (d) => <>{d.os_name} <span className="muted">{d.os_version}</span></> },
  { key: "status", label: "Estado", render: (d) => <StatusBadge status={d.status} /> },
  { key: "compliance", label: "Cumplimiento", render: (d) => <ComplianceBadge state={d.compliance} /> },
  { key: "group", label: "Grupo", render: (d) => d.group_name || <span className="muted">—</span> },
  { key: "user", label: "Usuario", render: (d) => d.assigned_user || <span className="muted">—</span> },
  {
    key: "last_seen", label: "Última conexión", sort: "last_seen_at", className: "nowrap",
    render: (d) => <span title={d.last_seen_at ? formatDateTime(d.last_seen_at) : undefined}>{relativeTime(d.last_seen_at)}</span>,
  },
];

export default function Devices() {
  const [params, setParams] = useSearchParams();
  const [selected, setSelected] = useState<Map<string, Device>>(new Map());
  const toggle = (d: Device) => setSelected((prev) => {
    const next = new Map(prev);
    if (next.has(d.id)) next.delete(d.id);
    else next.set(d.id, d);
    return next;
  });
  const selectedPlatforms = new Set([...selected.values()].map((d) => d.platform));
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

  // Si la URL cambia desde fuera (un enlace, «Quitar filtros»), la caja la sigue.
  const urlQ = params.get("q") ?? "";
  useEffect(() => {
    if (urlQ !== debounced) setSearch(urlQ);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlQ]);

  // La búsqueda pasa a la URL cuando se deja de escribir.
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
  const sort = params.get("sort") ?? "hostname";
  const order = (params.get("order") as "asc" | "desc" | null) ?? DEFAULT_ORDER[sort] ?? "asc";
  const page = Math.max(1, Number(params.get("page")) || 1);
  const hasFilters = Object.keys(filters).length > 0;

  const listQuery = query({ ...filters, sort: params.get("sort"), order: params.get("order"), page: page > 1 ? page : null });
  const devices = useQuery({
    queryKey: ["devices", listQuery],
    queryFn: () => api<Page<Device>>("GET", "/devices" + listQuery),
    placeholderData: keepPreviousData,
    refetchInterval: 30_000,
  });
  const data = devices.data;
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });

  function onSort(field: string) {
    const nextOrder = field === sort ? (order === "asc" ? "desc" : "asc") : DEFAULT_ORDER[field] ?? "asc";
    const isDefault = field === "hostname" && nextOrder === "asc";
    update({ sort: isDefault ? null : field, order: isDefault ? null : nextOrder });
  }

  function clearFilters() {
    setSearch("");
    setParams(new URLSearchParams(), { replace: true });
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Dispositivos</h1>
        </div>
        <div className="page-actions">
          <a className="btn" href={`/api/v1/devices.csv${query({ ...filters, sort: params.get("sort"), order: params.get("order") }) || "?"}`} download>
            Exportar CSV
          </a>
        </div>
      </div>

      <div className="toolbar" role="search">
        <input
          type="search"
          aria-label="Buscar"
          placeholder="Buscar por nombre, número de serie o usuario"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <label>
          <span className="visually-hidden">Plataforma</span>
          <select aria-label="Plataforma" value={filters.platform ?? ""} onChange={(e) => update({ platform: e.target.value })}>
            <option value="">Todas las plataformas</option>
            <option value="windows">Windows</option>
            <option value="linux">Linux</option>
            <option value="android">Android</option>
          </select>
        </label>
        <label>
          <span className="visually-hidden">Estado</span>
          <select aria-label="Estado" value={filters.status ?? ""} onChange={(e) => update({ status: e.target.value })}>
            <option value="">Todos los estados</option>
            <option value="online">En línea</option>
            <option value="offline">Desconectado</option>
            <option value="inactive">Inactivo</option>
            <option value="revoked">Revocado</option>
          </select>
        </label>
        <label>
          <span className="visually-hidden">Cumplimiento</span>
          <select aria-label="Cumplimiento" value={filters.compliance ?? ""} onChange={(e) => update({ compliance: e.target.value })}>
            <option value="">Cualquier cumplimiento</option>
            <option value="compliant">Cumple</option>
            <option value="noncompliant">No cumple</option>
            <option value="unknown">Desconocido</option>
          </select>
        </label>
        <label>
          <span className="visually-hidden">Grupo</span>
          <select aria-label="Grupo" value={filters.group_id ?? ""} onChange={(e) => update({ group_id: e.target.value })}>
            <option value="">Todos los grupos</option>
            {groups.data?.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
          </select>
        </label>
        {(filters.issue || filters.tag || filters.os_version) && (
          <span className="tag">
            {filters.issue ? ISSUE_LABELS[filters.issue] ?? filters.issue : filters.tag ? `Etiqueta: ${filters.tag}` : `Versión ${filters.os_version}`}
            {" "}
            <button className="btn-quiet btn" style={{ minHeight: 0, padding: 0 }} onClick={() => update({ issue: null, tag: null, os_version: null })}
              aria-label="Quitar este filtro">✕</button>
          </span>
        )}
        {hasFilters && !!data?.total && <button className="btn btn-quiet" onClick={clearFilters}>Quitar filtros</button>}
      </div>

      {devices.error ? (
        <p className="form-error" role="alert">{devices.error instanceof ApiError ? devices.error.message : "No se ha podido cargar el listado."}</p>
      ) : !data ? (
        <p className="muted">Cargando dispositivos…</p>
      ) : data.total === 0 && !hasFilters ? (
        <NoEnrollments />
      ) : data.total === 0 ? (
        <EmptyState message="Ningún dispositivo coincide con los filtros.">
          <button className="btn" onClick={clearFilters}>Quitar filtros</button>
        </EmptyState>
      ) : (
        <>
          {selected.size > 0 && (
            <ActionBar target={{
              deviceIds: [...selected.keys()],
              label: selected.size === 1 ? [...selected.values()][0].hostname : `${selected.size} equipos seleccionados`,
              platform: selectedPlatforms.size === 1 ? [...selectedPlatforms][0] : undefined,
            }}
              label="">
              <span className="small"><strong>{selected.size === 1 ? "1 seleccionado" : `${selected.size} seleccionados`}</strong></span>
              <button className="btn btn-quiet" onClick={() => setSelected(new Map())}>Quitar selección</button>
            </ActionBar>
          )}
          <DataTable columns={[{
            key: "select", label: "Seleccionar", hideLabel: true, className: "col-select",
            render: (d: Device) => (
              <input type="checkbox" aria-label={`Seleccionar ${d.hostname}`} checked={selected.has(d.id)} onChange={() => toggle(d)} disabled={d.status === "revoked"} />
            ),
          }, ...columns]} rows={data.items} rowKey={(d) => d.id} sort={sort} order={order} onSort={onSort}
            loading={devices.isPlaceholderData} caption="Dispositivos" />
          <Pagination page={data.page} perPage={data.per_page} total={data.total} onChange={(p) => update({ page: p > 1 ? String(p) : null }, true)} />
        </>
      )}
    </>
  );
}
