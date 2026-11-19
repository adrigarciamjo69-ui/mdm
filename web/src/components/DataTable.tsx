import type { ReactNode } from "react";

export interface Column<T> {
  key: string;
  label: string;
  render(row: T): ReactNode;
  /** Si se indica, la columna se puede ordenar por este campo. */
  sort?: string;
  className?: string;
  /** La cabecera solo se anuncia a los lectores de pantalla. */
  hideLabel?: boolean;
}

interface Props<T> {
  columns: Column<T>[];
  rows: T[];
  rowKey(row: T): string;
  sort?: string;
  order?: "asc" | "desc";
  onSort?(field: string): void;
  loading?: boolean;
  caption?: string;
}

export default function DataTable<T>({ columns, rows, rowKey, sort, order, onSort, loading, caption }: Props<T>) {
  return (
    <div className="table-wrap">
      <table className={loading ? "loading" : undefined} aria-busy={loading || undefined}>
        {caption && <caption className="visually-hidden">{caption}</caption>}
        <thead>
          <tr>
            {columns.map((c) => {
              const active = !!c.sort && c.sort === sort;
              const ariaSort = active ? (order === "desc" ? "descending" : "ascending") : undefined;
              return (
                <th key={c.key} scope="col" className={c.className} aria-sort={ariaSort}>
                  {c.sort && onSort ? (
                    <button type="button" className="sort-btn" data-active={active} onClick={() => onSort(c.sort!)}>
                      {c.hideLabel ? <span className="visually-hidden">{c.label}</span> : c.label}
                      <span aria-hidden="true">{active ? (order === "desc" ? "▾" : "▴") : ""}</span>
                    </button>
                  ) : (
                    c.label
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={rowKey(r)}>
              {columns.map((c) => (
                <td key={c.key} className={c.className}>{c.render(r)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
