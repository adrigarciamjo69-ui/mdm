import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import RuleEditor, { ruleIsComplete, type Rule } from "../components/RuleEditor";
import type { Group } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import ConfirmDialog from "../components/ConfirmDialog";
import EmptyState from "../components/EmptyState";
import { formatNumber } from "../lib/format";

const devicesLabel = (n: number) => `${formatNumber(n)} ${n === 1 ? "dispositivo" : "dispositivos"}`;

function deleteBody(n: number) {
  if (n === 0) return "No tiene dispositivos.";
  if (n === 1) return "Su único dispositivo quedará sin grupo; no se borra.";
  return `Sus ${formatNumber(n)} dispositivos quedarán sin grupo; no se borra ninguno.`;
}

export default function Groups() {
  const { can } = useAuth();
  const canEdit = can("operator");
  const queryClient = useQueryClient();
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });
  const staticGroups = (groups.data ?? []).filter((g) => g.kind !== "dynamic");
  const [editing, setEditing] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<Group | null>(null);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["groups"] });

  async function remove(g: Group) {
    await api("DELETE", `/groups/${g.id}`);
    setDeleting(null);
    await refresh();
    void queryClient.invalidateQueries({ queryKey: ["devices"] });
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Grupos</h1>
          <p className="muted">Los grupos manuales organizan los equipos por departamento, sede o función: cada equipo está como mucho en uno. Los dinámicos se calculan con una regla y un equipo puede estar en varios.</p>
        </div>
      </div>

      {canEdit && <CreateGroup staticGroups={staticGroups} onCreated={refresh} />}

      {groups.error ? (
        <p className="form-error" role="alert">{groups.error instanceof ApiError ? groups.error.message : "No se han podido cargar los grupos."}</p>
      ) : !groups.data ? (
        <p className="muted">Cargando…</p>
      ) : groups.data.length === 0 ? (
        <EmptyState message={canEdit ? "Aún no hay grupos. Crea el primero con el formulario de arriba." : "Aún no hay grupos."} />
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th scope="col">Nombre</th>
                <th scope="col">Descripción</th>
                <th scope="col">Dispositivos</th>
                {canEdit && <th scope="col"><span className="visually-hidden">Acciones</span></th>}
              </tr>
            </thead>
            <tbody>
              {groups.data.map((g) =>
                editing === g.id ? (
                  <EditRow key={g.id} g={g} staticGroups={staticGroups} onDone={() => { setEditing(null); void refresh(); }} onCancel={() => setEditing(null)} />
                ) : (
                  <tr key={g.id}>
                    <td><strong>{g.name}</strong>{g.kind === "dynamic" && <> <span className="tag">Dinámico</span></>}</td>
                    <td>{g.description || <span className="muted">—</span>}</td>
                    <td className="nowrap">
                      {g.device_count > 0 ? <Link to={`/dispositivos?group_id=${g.id}`}>{devicesLabel(g.device_count)}</Link> : <span className="muted">{devicesLabel(0)}</span>}
                    </td>
                    {canEdit && (
                      <td className="num nowrap">
                        <button className="btn btn-small" onClick={() => setEditing(g.id)}>Editar</button>{" "}
                        <button className="btn btn-small" onClick={() => setDeleting(g)}>Eliminar</button>
                      </td>
                    )}
                  </tr>
                ),
              )}
            </tbody>
          </table>
        </div>
      )}

      {deleting && (
        <ConfirmDialog
          title={`Eliminar el grupo «${deleting.name}»`}
          body={deleteBody(deleting.device_count)}
          confirmLabel="Eliminar"
          danger
          onConfirm={() => remove(deleting)}
          onCancel={() => setDeleting(null)}
        />
      )}
    </>
  );
}

const DEFAULT_RULE: Rule = { match: "all", conditions: [{ field: "platform", op: "eq", value: "windows" }] };

function CreateGroup({ onCreated, staticGroups }: { onCreated(): void; staticGroups: Group[] }) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [kind, setKind] = useState<"static" | "dynamic">("static");
  const [rule, setRule] = useState<Rule>(DEFAULT_RULE);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      await api("POST", "/groups", kind === "dynamic"
        ? { name: name.trim(), description: description.trim(), kind, rule }
        : { name: name.trim(), description: description.trim() });
      setName("");
      setDescription("");
      setKind("static");
      setRule(DEFAULT_RULE);
      onCreated();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido crear el grupo.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="panel" onSubmit={submit} style={{ marginBottom: "1.5rem" }}>
      <h2>Nuevo grupo</h2>
      <div className="inline-form">
        <label className="field">
          <span>Nombre</span>
          <input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} />
        </label>
        <label className="field">
          <span>Descripción <span className="hint">opcional</span></span>
          <input value={description} onChange={(e) => setDescription(e.target.value)} maxLength={500} aria-label="Descripción" />
        </label>
        <label className="field">
          <span>Tipo</span>
          <select value={kind} onChange={(e) => setKind(e.target.value as "static" | "dynamic")}>
            <option value="static">Manual</option>
            <option value="dynamic">Dinámico (por reglas)</option>
          </select>
        </label>
        <button className="btn btn-primary" disabled={busy || !name.trim() || (kind === "dynamic" && !ruleIsComplete(rule))}>Crear grupo</button>
      </div>
      {kind === "dynamic" && <RuleEditor rule={rule} onChange={setRule} groups={staticGroups} />}
      {error && <p className="form-error" role="alert" style={{ marginBottom: 0 }}>{error}</p>}
    </form>
  );
}

function EditRow({ g, onDone, onCancel, staticGroups }: { g: Group; onDone(): void; onCancel(): void; staticGroups: Group[] }) {
  const [rule, setRule] = useState<Rule>((g.rule as Rule) ?? DEFAULT_RULE);
  const dynamic = g.kind === "dynamic";
  const [name, setName] = useState(g.name);
  const [description, setDescription] = useState(g.description);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function save(e: FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      await api("PATCH", `/groups/${g.id}`, dynamic
        ? { name: name.trim(), description: description.trim(), rule }
        : { name: name.trim(), description: description.trim() });
      onDone();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se han podido guardar los cambios.");
      setBusy(false);
    }
  }

  return (
    <tr>
      <td colSpan={4}>
        <form onSubmit={save} className="inline-form">
          <label className="field">
            <span>Nombre</span>
            <input value={name} onChange={(e) => setName(e.target.value)} aria-label="Nombre del grupo" maxLength={100} autoFocus />
          </label>
          <label className="field">
            <span>Descripción</span>
            <input value={description} onChange={(e) => setDescription(e.target.value)} aria-label="Descripción del grupo" maxLength={500} />
          </label>
          <div style={{ display: "flex", gap: "0.5rem" }}>
            <button className="btn btn-primary" disabled={busy || !name.trim() || (dynamic && !ruleIsComplete(rule))}>Guardar</button>
            <button type="button" className="btn" onClick={onCancel}>Cancelar</button>
          </div>
        </form>
        {dynamic && <RuleEditor rule={rule} onChange={setRule} groups={staticGroups} />}
        {error && <p className="form-error" role="alert" style={{ marginBottom: 0 }}>{error}</p>}
      </td>
    </tr>
  );
}
