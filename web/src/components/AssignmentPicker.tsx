import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Group } from "../api/types";

export interface Assign { group_id: string | null; exclude: boolean }

/** Alcance: grupos incluidos y excluidos (o todos los equipos). */
export default function AssignmentPicker({ value, onChange, readOnly }: { value: Assign[]; onChange(v: Assign[]): void; readOnly?: boolean }) {
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });
  const name = (gid: string | null) => (gid === null ? "Todos los equipos" : groups.data?.find((g) => g.id === gid)?.name ?? "Grupo eliminado");
  const free = (groups.data ?? []).filter((g) => !value.some((a) => a.group_id === g.id));
  return (
    <>
      <ul className="assign-list">
        {value.map((a) => (
          <li key={`${a.group_id}-${a.exclude}`}>
            <span className={a.exclude ? "tag tag-exclude" : "tag"}>{a.exclude ? "Excepto " : ""}{name(a.group_id)}</span>
            {!readOnly && <button type="button" className="btn btn-quiet" onClick={() => onChange(value.filter((x) => x !== a))}>Quitar</button>}
          </li>
        ))}
        {value.length === 0 && <li className="muted">Sin alcance: no se aplica a ningún equipo.</li>}
      </ul>
      {!readOnly && (
        <div className="edit-grid">
          <label className="field">
            <span>Incluir grupo</span>
            <select value="" onChange={(e) => e.target.value && onChange([...value, { group_id: e.target.value === "*" ? null : e.target.value, exclude: false }])}>
              <option value="">Elige…</option>
              {!value.some((a) => a.group_id === null) && <option value="*">Todos los equipos</option>}
              {free.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
            </select>
          </label>
          <label className="field">
            <span>Excluir grupo</span>
            <select value="" onChange={(e) => e.target.value && onChange([...value, { group_id: e.target.value, exclude: true }])}>
              <option value="">Elige…</option>
              {free.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
            </select>
          </label>
        </div>
      )}
    </>
  );
}
