import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import ConfirmDialog from "../components/ConfirmDialog";
import { formatDateTime } from "../lib/format";
import type { ScriptRow } from "./Scripts";

type Param = ScriptRow["parameters"][number];
interface Version { version: number; content?: string; created_by_email: string; created_at: string }

const INTERPRETERS: Record<string, string[]> = { windows: ["powershell"], linux: ["bash", "sh", "python3"] };

export default function ScriptEdit() {
  const { id = "" } = useParams();
  const isNew = id === "nuevo";
  const readOnly = !useAuth().can("admin");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const script = useQuery({ queryKey: ["script", id], queryFn: () => api<ScriptRow>("GET", `/scripts/${id}`), enabled: !isNew });
  const versions = useQuery({ queryKey: ["script-versions", id], queryFn: () => api<Version[]>("GET", `/scripts/${id}/versions`), enabled: !isNew });
  const latest = script.data?.latest_version;
  const current = useQuery({
    queryKey: ["script-version", id, latest],
    queryFn: () => api<Version>("GET", `/scripts/${id}/versions/${latest}`),
    enabled: !isNew && !!latest,
  });

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [platform, setPlatform] = useState("linux");
  const [interpreter, setInterpreter] = useState("bash");
  const [timeout, setTimeoutS] = useState(300);
  const [params, setParams] = useState<Param[]>([]);
  const [content, setContent] = useState("");
  const [original, setOriginal] = useState("");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  const [busy, setBusy] = useState(false);
  const [archiving, setArchiving] = useState(false);
  const [compare, setCompare] = useState<[number, number] | null>(null);

  useEffect(() => {
    const s = script.data;
    if (!s) return;
    setName(s.name); setDescription(s.description); setPlatform(s.platform); setInterpreter(s.interpreter);
    setTimeoutS(s.timeout_s); setParams(s.parameters);
  }, [script.data]);
  useEffect(() => {
    if (current.data?.content !== undefined) { setContent(current.data.content); setOriginal(current.data.content); }
  }, [current.data]);

  async function save(e: FormEvent) {
    e.preventDefault();
    setError(""); setSaved("");
    setBusy(true);
    try {
      const meta = { name, description, timeout_s: timeout, parameters: params.filter((p) => p.name.trim()) };
      if (isNew) {
        const s = await api<ScriptRow>("POST", "/scripts", { ...meta, platform, interpreter, content });
        void queryClient.invalidateQueries({ queryKey: ["scripts"] });
        navigate(`/scripts/${s.id}`, { replace: true });
        return;
      }
      await api("PATCH", `/scripts/${id}`, meta);
      if (content !== original) {
        const v = await api<{ version: number }>("POST", `/scripts/${id}/versions`, { content });
        setSaved(`Guardado como versión ${v.version}.`);
        setOriginal(content);
      } else {
        setSaved("Guardado.");
      }
      void queryClient.invalidateQueries({ queryKey: ["script", id] });
      void queryClient.invalidateQueries({ queryKey: ["script-versions", id] });
      void queryClient.invalidateQueries({ queryKey: ["scripts"] });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido guardar el script.");
    } finally {
      setBusy(false);
    }
  }

  async function archive() {
    await api("POST", `/scripts/${id}/archive`);
    void queryClient.invalidateQueries({ queryKey: ["scripts"] });
    navigate("/scripts", { replace: true });
  }

  if (!isNew && script.error) return <div className="notice"><p>Este script no existe.</p><Link to="/scripts">Volver a scripts</Link></div>;

  return (
    <form onSubmit={save} noValidate>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/scripts">Scripts</Link></p>
      <div className="page-head">
        <h1>{isNew ? "Nuevo script" : name}</h1>
        {!readOnly && !isNew && !script.data?.archived && <div className="page-actions"><button type="button" className="btn" onClick={() => setArchiving(true)}>Archivar</button></div>}
      </div>
      <div className="panel">
        <div className="edit-grid">
          <label className="field"><span>Nombre</span><input value={name} onChange={(e) => setName(e.target.value)} disabled={readOnly} maxLength={100} /></label>
          <label className="field"><span>Descripción <span className="hint">opcional</span></span><input value={description} onChange={(e) => setDescription(e.target.value)} disabled={readOnly} /></label>
          <label className="field">
            <span>Plataforma</span>
            <select value={platform} disabled={!isNew || readOnly} onChange={(e) => { setPlatform(e.target.value); setInterpreter(INTERPRETERS[e.target.value][0]); }}>
              <option value="windows">Windows</option><option value="linux">Linux</option>
            </select>
          </label>
          <label className="field">
            <span>Intérprete</span>
            <select value={interpreter} disabled={!isNew || readOnly} onChange={(e) => setInterpreter(e.target.value)}>
              {INTERPRETERS[platform].map((i) => <option key={i} value={i}>{i}</option>)}
            </select>
          </label>
          <label className="field"><span>Tiempo máximo (segundos)</span><input type="number" min={1} max={3600} value={timeout} onChange={(e) => setTimeoutS(Number(e.target.value))} disabled={readOnly} /></label>
        </div>
        <fieldset className="params" disabled={readOnly}>
          <legend>Parámetros</legend>
          <p className="muted small">Llegan al script como variables de entorno <span className="mono">MDM_PARAM_NOMBRE</span>, nunca pegados al texto.</p>
          {params.map((p, i) => (
            <div key={i} className="param-row">
              <input aria-label="Nombre del parámetro" value={p.name} placeholder="RUTA" onChange={(e) => setParams(params.map((x, j) => (j === i ? { ...x, name: e.target.value.toUpperCase() } : x)))} />
              <input aria-label="Descripción del parámetro" value={p.description} placeholder="Descripción" onChange={(e) => setParams(params.map((x, j) => (j === i ? { ...x, description: e.target.value } : x)))} />
              <label className="check-field" style={{ margin: 0 }}><input type="checkbox" checked={p.required} onChange={(e) => setParams(params.map((x, j) => (j === i ? { ...x, required: e.target.checked } : x)))} />Obligatorio</label>
              <button type="button" className="btn btn-quiet" onClick={() => setParams(params.filter((_, j) => j !== i))}>Quitar</button>
            </div>
          ))}
          {!readOnly && params.length < 20 && (
            <button type="button" className="btn btn-small" onClick={() => setParams([...params, { name: "", description: "", required: false, default: "" }])}>Añadir parámetro</button>
          )}
        </fieldset>
        <label className="field">
          <span>Contenido</span>
          <textarea className="code" rows={18} value={content} onChange={(e) => setContent(e.target.value)} readOnly={readOnly} spellCheck={false} />
        </label>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      {!readOnly && (
        <div style={{ display: "flex", gap: "1rem", alignItems: "center", marginTop: "1rem" }}>
          <button className="btn btn-primary" disabled={busy}>Guardar</button>
          {saved && <span className="form-ok" role="status">{saved}</span>}
        </div>
      )}

      {!isNew && versions.data && (
        <section className="section">
          <h2>Historial</h2>
          <ul className="version-list">
            {versions.data.map((v) => (
              <li key={v.version}>
                <span>Versión {v.version}</span>
                <span className="muted small">{formatDateTime(v.created_at)} · {v.created_by_email}</span>
                {v.version > 1 && <button type="button" className="btn btn-quiet" onClick={() => setCompare([v.version - 1, v.version])}>Comparar con la anterior</button>}
              </li>
            ))}
          </ul>
          {compare && <Compare id={id} a={compare[0]} b={compare[1]} onClose={() => setCompare(null)} />}
        </section>
      )}
      {archiving && (
        <ConfirmDialog title={`Archivar «${name}»`} body="No se podrá ejecutar más. Sus versiones y las ejecuciones anteriores se conservan."
          confirmLabel="Archivar" onConfirm={archive} onCancel={() => setArchiving(false)} />
      )}
    </form>
  );
}

function Compare({ id, a, b, onClose }: { id: string; a: number; b: number; onClose(): void }) {
  const va = useQuery({ queryKey: ["script-version", id, a], queryFn: () => api<Version>("GET", `/scripts/${id}/versions/${a}`) });
  const vb = useQuery({ queryKey: ["script-version", id, b], queryFn: () => api<Version>("GET", `/scripts/${id}/versions/${b}`) });
  return (
    <div className="panel" style={{ marginTop: "1rem" }}>
      <div className="compare">
        <div><h3>Versión {a}</h3><pre>{va.data?.content ?? "…"}</pre></div>
        <div><h3>Versión {b}</h3><pre>{vb.data?.content ?? "…"}</pre></div>
      </div>
      <button type="button" className="btn btn-small" onClick={onClose}>Cerrar</button>
    </div>
  );
}
