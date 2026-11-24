import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, query } from "../api/client";
import type { Asset, Page } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import ConfirmDialog from "../components/ConfirmDialog";
import Pagination from "../components/Pagination";
import { formatDateTime } from "../lib/format";

export default function Trash() {
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [purging, setPurging] = useState<Asset | null>(null);
  const [error, setError] = useState("");
  const q = useQuery({
    queryKey: ["assets", "trash", page],
    queryFn: () => api<Page<Asset>>("GET", "/assets" + query({ trash: 1, sort: "deleted_at", order: "desc", page: page > 1 ? page : null })),
  });

  async function restore(a: Asset) {
    setError("");
    try {
      await api("POST", `/assets/${a.id}/restore`);
      void queryClient.invalidateQueries({ queryKey: ["assets"] });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido restaurar.");
    }
  }
  async function purge(a: Asset) {
    await api("DELETE", `/assets/${a.id}/purge`);
    setPurging(null);
    void queryClient.invalidateQueries({ queryKey: ["assets"] });
  }

  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/inventario">Inventario</Link></p>
      <h1>Papelera</h1>
      <p className="muted">Activos eliminados del inventario. Al restaurarlos vuelven tal como estaban, salvo la persona. No se vacía sola.</p>
      {error && <p className="form-error" role="alert">{error}</p>}
      {!q.data ? <p className="muted">Cargando…</p> : q.data.total === 0 ? <p className="muted">La papelera está vacía.</p> : (
        <>
          <div className="table-wrap">
            <table>
              <thead><tr><th scope="col">Activo</th><th scope="col">Categoría</th><th scope="col">Serie</th><th scope="col">Eliminado</th><th scope="col">Motivo</th><th scope="col"><span className="visually-hidden">Acciones</span></th></tr></thead>
              <tbody>
                {q.data.items.map((a) => (
                  <tr key={a.id}>
                    <td><strong>{a.name}</strong></td>
                    <td>{a.category}</td>
                    <td className="mono">{a.serial || "—"}</td>
                    <td className="nowrap">{a.deleted_at ? formatDateTime(a.deleted_at) : ""}</td>
                    <td>{a.deleted_reason || <span className="muted">—</span>}</td>
                    <td className="num nowrap">
                      {can("operator") && <button className="btn btn-small" onClick={() => void restore(a)}>Restaurar</button>}{" "}
                      {can("admin") && <button className="btn btn-small btn-danger-outline" onClick={() => setPurging(a)}>Eliminar definitivamente</button>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination page={q.data.page} perPage={q.data.per_page} total={q.data.total} onChange={setPage} />
        </>
      )}
      {purging && (
        <ConfirmDialog title={`Eliminar definitivamente «${purging.name}»`}
          body="Se borran el activo, sus documentos y sus asignaciones de licencias. No se puede deshacer."
          confirmLabel="Eliminar definitivamente" danger requireText={purging.name}
          onConfirm={() => purge(purging)} onCancel={() => setPurging(null)} />
      )}
    </>
  );
}
