import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { CatalogDef, Group, Platform, Policy, PolicySetting } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import ConfirmDialog from "../components/ConfirmDialog";
import SettingForm, { defaultParams, validateParams } from "../components/SettingForm";
import StatusBadge from "../components/StatusBadge";
import type { Status } from "../api/types";

interface Assign {
  group_id: string | null;
  exclude: boolean;
}

/** Quita los parámetros vacíos para que el servidor aplique sus valores por defecto. */
function cleanParams(p: Record<string, unknown>) {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(p)) if (v !== undefined && v !== "") out[k] = v;
  return out;
}

export default function PolicyEdit() {
  const { id } = useParams();
  const isNew = !id || id === "nueva";
  const { can } = useAuth();
  const readOnly = !can("admin");
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const existing = useQuery({ queryKey: ["policy", id], queryFn: () => api<Policy>("GET", `/policies/${id}`), enabled: !isNew });
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [platform, setPlatform] = useState<Platform>("windows");
  const [settings, setSettings] = useState<PolicySetting[]>([]);
  const [assign, setAssign] = useState<Assign[]>([]);
  const [errors, setErrors] = useState<Record<string, Record<string, string>>>({});
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState(false);
  const [deleting, setDeleting] = useState(false);

  useEffect(() => {
    const p = existing.data;
    if (!p) return;
    setName(p.name);
    setDescription(p.description);
    setPlatform(p.platform);
    setSettings(p.settings);
    setAssign(p.assignments.map((a) => ({ group_id: a.group_id, exclude: a.exclude })));
  }, [existing.data]);

  const catalog = useQuery({ queryKey: ["policy-catalog", platform], queryFn: () => api<CatalogDef[]>("GET", `/policy-catalog?platform=${platform}`) });
  const defs = useMemo(() => new Map((catalog.data ?? []).map((d) => [d.key, d])), [catalog.data]);
  const available = (catalog.data ?? []).filter((d) => !settings.some((s) => s.key === d.key));
  const devices = useQuery({
    queryKey: ["policy-devices", id],
    queryFn: () => api<{ id: string; hostname: string; status: Status }[]>("GET", `/policies/${id}/devices`),
    enabled: !isNew,
  });

  function addSetting(key: string) {
    const def = defs.get(key);
    if (!def) return;
    setSettings([...settings, { key, mode: def.audit_only ? "audit" : "enforce", params: defaultParams(def) }]);
  }

  const groupName = (gid: string | null) => (gid === null ? "Todos los equipos" : groups.data?.find((g) => g.id === gid)?.name ?? "Grupo eliminado");
  const assignable = (groups.data ?? []).filter((g) => !assign.some((a) => a.group_id === g.id));

  async function save(e: FormEvent) {
    e.preventDefault();
    setError("");
    setSaved(false);
    const errs: Record<string, Record<string, string>> = {};
    for (const s of settings) {
      const def = defs.get(s.key);
      if (def) {
        const fe = validateParams(def, s.params);
        if (Object.keys(fe).length) errs[s.key] = fe;
      }
    }
    setErrors(errs);
    if (!name.trim()) {
      setError("El nombre es obligatorio.");
      return;
    }
    if (Object.keys(errs).length) return;
    setBusy(true);
    try {
      const body = { name: name.trim(), description, settings: settings.map((s) => ({ ...s, params: cleanParams(s.params) })) };
      const p = isNew
        ? await api<Policy>("POST", "/policies", { ...body, platform })
        : await api<Policy>("PATCH", `/policies/${id}`, body);
      await api("PUT", `/policies/${p.id}/assignments`, { assignments: assign });
      void queryClient.invalidateQueries({ queryKey: ["policies"] });
      void queryClient.invalidateQueries({ queryKey: ["policy", p.id] });
      setSaved(true);
      if (isNew) navigate(`/politicas/${p.id}`, { replace: true });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido guardar la política.");
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    await api("DELETE", `/policies/${id}`);
    void queryClient.invalidateQueries({ queryKey: ["policies"] });
    navigate("/politicas", { replace: true });
  }

  if (!isNew && existing.error) {
    return (
      <div className="notice">
        <p>Esta política ya no existe.</p>
        <Link to="/politicas">Volver a políticas</Link>
      </div>
    );
  }

  return (
    <form onSubmit={save} noValidate>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/politicas">Políticas</Link></p>
      <div className="page-head">
        <h1>{isNew ? "Nueva política" : name || "Política"}</h1>
        {!readOnly && !isNew && <div className="page-actions"><button type="button" className="btn" onClick={() => setDeleting(true)}>Eliminar</button></div>}
      </div>

      <div className="panel">
        <div className="edit-grid">
          <label className="field">
            <span>Nombre</span>
            <input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} disabled={readOnly} />
          </label>
          <label className="field">
            <span>Plataforma</span>
            <select value={platform} disabled={!isNew || readOnly} onChange={(e) => { setPlatform(e.target.value as Platform); setSettings([]); }}>
              <option value="windows">Windows</option>
              <option value="linux">Linux</option>
              <option value="android">Android</option>
            </select>
          </label>
        </div>
        <label className="field">
          <span>Descripción <span className="hint">opcional</span></span>
          <input value={description} onChange={(e) => setDescription(e.target.value)} disabled={readOnly} aria-label="Descripción" />
        </label>
      </div>

      <section className="section">
        <h2>Ajustes</h2>
        {settings.length === 0 && <p className="muted" style={{ marginBottom: "0.75rem" }}>La política aún no tiene ajustes.</p>}
        {settings.map((s, i) => {
          const def = defs.get(s.key);
          if (!def) return null;
          return (
            <SettingForm key={s.key} def={def} setting={s} errors={errors[s.key] ?? {}} readOnly={readOnly}
              onChange={(n) => setSettings(settings.map((x, j) => (j === i ? n : x)))}
              onRemove={() => setSettings(settings.filter((_, j) => j !== i))} />
          );
        })}
        {!readOnly && (
          <label className="field" style={{ maxWidth: "26rem" }}>
            <span>Añadir ajuste</span>
            <select value="" onChange={(e) => addSetting(e.target.value)}>
              <option value="">Elige un ajuste…</option>
              {available.map((d) => <option key={d.key} value={d.key}>{d.title}</option>)}
            </select>
          </label>
        )}
      </section>

      <section className="section">
        <h2>Asignación</h2>
        <p className="muted small" style={{ marginBottom: "0.75rem" }}>La exclusión siempre gana: un equipo incluido por un grupo y excluido por otro no recibe la política.</p>
        <ul className="assign-list">
          {assign.map((a) => (
            <li key={`${a.group_id}-${a.exclude}`}>
              <span className={a.exclude ? "tag tag-exclude" : "tag"}>{a.exclude ? "Excepto " : ""}{groupName(a.group_id)}</span>
              {!readOnly && (
                <button type="button" className="btn btn-quiet" aria-label={`Quitar ${groupName(a.group_id)}`}
                  onClick={() => setAssign(assign.filter((x) => x !== a))}>Quitar</button>
              )}
            </li>
          ))}
          {assign.length === 0 && <li className="muted">Sin asignar: no se aplica a ningún equipo.</li>}
        </ul>
        {!readOnly && (
          <div className="edit-grid">
            <label className="field">
              <span>Incluir grupo</span>
              <select value="" onChange={(e) => e.target.value && setAssign([...assign, { group_id: e.target.value === "*" ? null : e.target.value, exclude: false }])}>
                <option value="">Elige…</option>
                {!assign.some((a) => a.group_id === null) && <option value="*">Todos los equipos</option>}
                {assignable.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
              </select>
            </label>
            <label className="field">
              <span>Excluir grupo</span>
              <select value="" onChange={(e) => e.target.value && setAssign([...assign, { group_id: e.target.value, exclude: true }])}>
                <option value="">Elige…</option>
                {assignable.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
              </select>
            </label>
          </div>
        )}
      </section>

      {error && <p className="form-error" role="alert">{error}</p>}
      {!readOnly && (
        <div style={{ display: "flex", gap: "1rem", alignItems: "center", marginTop: "1.5rem" }}>
          <button className="btn btn-primary" disabled={busy}>Guardar política</button>
          {saved && <span className="form-ok" role="status">Política guardada.</span>}
        </div>
      )}

      {!isNew && devices.data && (
        <section className="section">
          <h2>Equipos a los que se aplica ({devices.data.length})</h2>
          {devices.data.length === 0 ? (
            <p className="muted">Ninguno por ahora.</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th scope="col">Equipo</th><th scope="col">Estado</th></tr></thead>
                <tbody>
                  {devices.data.map((d) => (
                    <tr key={d.id}>
                      <td><Link className="row-link" to={`/dispositivos/${d.id}`}>{d.hostname}</Link></td>
                      <td><StatusBadge status={d.status} /></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}

      {deleting && (
        <ConfirmDialog title={`Eliminar la política «${name}»`} body="Los equipos dejarán de recibir sus ajustes. Lo que ya se aplicó en ellos no se deshace."
          confirmLabel="Eliminar" danger onConfirm={remove} onCancel={() => setDeleting(false)} />
      )}
    </form>
  );
}
