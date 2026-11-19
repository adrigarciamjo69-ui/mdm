import { formatNumber } from "../lib/format";

interface Props {
  page: number;
  perPage: number;
  total: number;
  onChange(page: number): void;
}

export default function Pagination({ page, perPage, total, onChange }: Props) {
  if (total === 0) return null;
  const first = (page - 1) * perPage + 1;
  const last = Math.min(page * perPage, total);
  const pages = Math.max(1, Math.ceil(total / perPage));
  return (
    <div className="pagination">
      <span>{first > total ? `0 de ${formatNumber(total)}` : `${formatNumber(first)}–${formatNumber(last)} de ${formatNumber(total)}`}</span>
      {pages > 1 && (
        <div>
          <button className="btn btn-small" disabled={page <= 1} onClick={() => onChange(page - 1)}>Anterior</button>
          <button className="btn btn-small" disabled={page >= pages} onClick={() => onChange(page + 1)}>Siguiente</button>
        </div>
      )}
    </div>
  );
}
