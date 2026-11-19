import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { Group } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import ConfirmDialog from "../components/ConfirmDialog";
import { CLASSES, describeWindow, type MaintWindow } from "../lib/patches";
import type { PatchPolicyRow } from "./Patches";

const DAYS: [number, string][] = [[1, "L"], [2, "M"], [3, "X"], [4, "J"], [5, "V"], [6, "S"], [0, "D"]];
const DAY_NAME = ["domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"];
interface Assign { group_id: string | null; exclude: boolean }

export default function PatchPolicyEdit() {
  const { id = "" } = useParams();
  const isNew = id === "nueva";
  const readOnly = !useAuth().can("admin");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });
  const existing = useQuery({ queryKey: ["patch-policy", id], queryFn: () => api<PatchPolicyRow>("GET", `/patch-policies/${id}`), enabled: !isNew });

  const [name, setName] = useState("");
  const [platform, setPlatform] = useState<"windows" | "linux">("windows");
  const [auto, setAuto] = useState<string[]>(["security", "critical", "definition"]);
  const [defer, setDefer] = useState(0);
  const [win, setWin] = useState<MaintWindow>({ days: [1, 2, 3, 4, 5], start: "22:00", end: "05:00" });
  const [reboot, setReboot] = useState<PatchPolicyRow["reboot"]>("in_window");
  const [notify, setNotify] = useState(15);
  const [scope, setScope] = useState<"security" | "all">("security");
  const [excluded, setExcluded] = useState("");
  const [assign, setAssign] = useState<Assign[]>([]);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [deleting, setDeleting] = useState(false);

  useEffect(() => {
    const p = existing.data;
    if (!p) return;
    setName(p.name); setPlatform(p.platform); setAuto(p.auto_approve); setDefer(p.defer_days); setWin(p.window); setReboot(p.reboot);
    setNotify(p.notify_minutes); setScope(p.linux_scope); setExcluded(p.excluded_kbs.join(", "));
    setAssign(p.assignments.map((a) => ({ group_id: a.group_id, exclude: a.exclude })));
  }, [existing.data]);

  const groupName = (gid: string | null) => (gid === null ? "Todos los equipos" : groups.data?.find((g) => g.id === gid)?.name ?? "Grupo eliminado");
  const assignable = (groups.data ?? []).filter((g) => !assign.some((a) => a.group_id === g.id));
  const toggleDay = (d: number) => setWin({ ...win, days: win.days.includes(d) ? win.days.filter((x) => x !== d) : [...win.days, d] });

  async function save(e: FormEvent) {
    e.preventDefault();
    setError(""); setSaved(false);
    const body = { name: name.trim(), platform, auto_approve: auto, defer_days: defer, window: win, reboot, notify_minutes: notify, linux_scope: scope,
      excluded_kbs: excluded.split(/[\s,]+/).filter(Boolean) };
    try {
      const p = isNew ? await api<PatchPolicyRow>("POST", "/patch-policies", body) : await api<PatchPolicyRow>("PATCH", `/patch-policies/${id}`, body);
      await api("PUT", `/patch-policies/${p.id}/assignments`, { assignments: assign });
      void queryClient.invalidateQueries({ queryKey: ["patch-policies"] });
      setSaved(true);
      if (isNew) navigate(`/parches/politicas/${p.id}`, { replace: true });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido guardar la política de parches.");
    }
  }

  return (
    <form onSubmit={save} noValidate>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/parches">Parches</Link></p>
      <div className="page-head">
        <h1>{isNew ? "Nueva política de parches" : name}</h1>
        {!readOnly && !isNew && <div className="page-actions"><button type="button" className="btn" onClick={() => setDeleting(true)}>Eliminar</button></div>}
      </div>
      <fieldset className="panel" disabled={readOnly} style={{ border: 0 }}>
        <div className="edit-grid">
          <label className="field"><span>Nombre</span><input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} /></label>
          <label className="field">
            <span>Plataforma</span>
            <select value={platform} disabled={!isNew} onChange={(e) => setPlatform(e.target.value as "windows" | "linux")}>
              <option value="windows">Windows</option><option value="linux">Linux</option>
            </select>
          </label>
          <label className="field">
            <span>Días de espera desde la publicación</span>
            <input type="number" min={0} max={60} value={defer} onChange={(e) => setDefer(Number(e.target.value))} />
          </label>
        </div>
        {platform === "windows" ? (
          <fieldset className="params">
            <legend>Se aprueban solas</legend>
            <div className="check-row">
              {CLASSES.map(([v, l]) => (
                <label key={v} className="check-field">
                  <input type="checkbox" checked={auto.includes(v)} onChange={(e) => setAuto(e.target.checked ? [...auto, v] : auto.filter((x) => x !== v))} />{l}
                </label>
              ))}
            </div>
            <label className="field" style={{ maxWidth: "30rem" }}>
              <span>KB excluidas <span className="hint">nunca se instalan, separadas por comas</span></span>
              <input value={excluded} onChange={(e) => setExcluded(e.target.value)} placeholder="KB5034441" />
            </label>
          </fieldset>
        ) : (
          <label className="field" style={{ maxWidth: "24rem" }}>
            <span>Qué se instala</span>
            <select value={scope} onChange={(e) => setScope(e.target.value as "security" | "all")}>
              <option value="security">Solo actualizaciones de seguridad</option>
              <option value="all">Todas las actualizaciones</option>
            </select>
          </label>
        )}
        <fieldset className="params">
          <legend>Ventana de mantenimiento <span className="hint">· en la hora local de cada equipo</span></legend>
          <div className="day-picker" role="group" aria-label="Días">
            {DAYS.map(([d, l]) => (
              <button key={d} type="button" className={win.days.includes(d) ? "day on" : "day"} aria-pressed={win.days.includes(d)}
                aria-label={DAY_NAME[d]} onClick={() => toggleDay(d)}>{l}</button>
            ))}
          </div>
          <div className="time-row">
            <label className="field"><span>Desde</span><input type="time" value={win.start} onChange={(e) => setWin({ ...win, start: e.target.value })} /></label>
            <label className="field"><span>Hasta</span><input type="time" value={win.end} onChange={(e) => setWin({ ...win, end: e.target.value })} /></label>
          </div>
          <p className="small">{describeWindow(win)}</p>
        </fieldset>
        <div className="edit-grid">
          <label className="field">
            <span>Reinicio</span>
            <select value={reboot} onChange={(e) => setReboot(e.target.value as PatchPolicyRow["reboot"])}>
              <option value="in_window">Reiniciar dentro de la ventana</option>
              <option value="notify">Avisar al usuario y reiniciar</option>
              <option value="never">No reiniciar nunca</option>
            </select>
          </label>
          {reboot !== "never" && (
            <label className="field"><span>Aviso previo (minutos)</span><input type="number" min={5} max={120} value={notify} onChange={(e) => setNotify(Number(e.target.value))} /></label>
          )}
        </div>
      </fieldset>
      <section className="section">
        <h2>Asignación</h2>
        <ul className="assign-list">
          {assign.map((a) => (
            <li key={`${a.group_id}-${a.exclude}`}>
              <span className={a.exclude ? "tag tag-exclude" : "tag"}>{a.exclude ? "Excepto " : ""}{groupName(a.group_id)}</span>
              {!readOnly && <button type="button" className="btn btn-quiet" onClick={() => setAssign(assign.filter((x) => x !== a))}>Quitar</button>}
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
        <div style={{ display: "flex", gap: "1rem", alignItems: "center", marginTop: "1rem" }}>
          <button className="btn btn-primary">Guardar política de parches</button>
          {saved && <span className="form-ok" role="status">Guardada.</span>}
        </div>
      )}
      {deleting && (
        <ConfirmDialog title={`Eliminar «${name}»`} body="Los equipos dejarán de instalar actualizaciones con esta política." confirmLabel="Eliminar" danger
          onConfirm={async () => { await api("DELETE", `/patch-policies/${id}`); navigate("/parches", { replace: true }); }} onCancel={() => setDeleting(false)} />
      )}
    </form>
  );
}
