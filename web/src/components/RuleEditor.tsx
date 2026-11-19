import { useEffect, useState } from "react";
import { api, ApiError } from "../api/client";
import { useDebounced } from "../lib/hooks";

export interface Condition {
  field: string;
  op: string;
  value: string | number;
}

export interface Rule {
  match: "all" | "any";
  conditions: Condition[];
}

type Kind = "enum" | "text" | "version" | "tag" | "software" | "days" | "group";

const OPS: Record<Kind, [string, string][]> = {
  enum: [["eq", "es"], ["ne", "no es"]],
  group: [["eq", "es"], ["ne", "no es"]],
  text: [["eq", "es"], ["ne", "no es"], ["contains", "contiene"], ["starts_with", "empieza por"]],
  version: [["eq", "es"], ["lt", "es menor que"], ["lte", "es menor o igual que"], ["gt", "es mayor que"], ["gte", "es mayor o igual que"]],
  tag: [["has", "tiene"], ["not_has", "no tiene"]],
  software: [["installed", "está instalado"], ["not_installed", "no está instalado"]],
  days: [["lt", "hace menos de (días)"], ["gt", "hace más de (días)"]],
};

const FIELDS: { key: string; label: string; kind: Kind; options?: [string, string][] }[] = [
  { key: "platform", label: "Plataforma", kind: "enum", options: [["windows", "Windows"], ["linux", "Linux"]] },
  { key: "group", label: "Grupo principal", kind: "group" },
  { key: "hostname", label: "Nombre del equipo", kind: "text" },
  { key: "os_name", label: "Sistema", kind: "text" },
  { key: "os_version", label: "Versión del sistema", kind: "version" },
  { key: "agent_version", label: "Versión del agente", kind: "version" },
  { key: "manufacturer", label: "Fabricante", kind: "text" },
  { key: "model", label: "Modelo", kind: "text" },
  { key: "assigned_user", label: "Usuario asignado", kind: "text" },
  { key: "domain", label: "Dominio", kind: "text" },
  { key: "tag", label: "Etiqueta", kind: "tag" },
  { key: "software", label: "Programa", kind: "software" },
  { key: "compliance", label: "Cumplimiento", kind: "enum", options: [["compliant", "Cumple"], ["noncompliant", "No cumple"], ["unknown", "Desconocido"]] },
  { key: "status", label: "Estado", kind: "enum", options: [["online", "En línea"], ["offline", "Desconectado"], ["inactive", "Inactivo"], ["revoked", "Revocado"]] },
  { key: "last_seen_days", label: "Última conexión", kind: "days" },
];

const fieldDef = (key: string) => FIELDS.find((f) => f.key === key) ?? FIELDS[0];

function defaultCondition(field: string, groups: { id: string }[]): Condition {
  const f = fieldDef(field);
  const op = OPS[f.kind][0][0];
  const value = f.options ? f.options[0][0] : f.kind === "group" ? groups[0]?.id ?? "" : f.kind === "days" ? 30 : "";
  return { field, op, value };
}

export function ruleIsComplete(r: Rule): boolean {
  return r.conditions.length > 0 && r.conditions.every((c) => c.value !== "" && c.value !== undefined);
}

interface Props {
  rule: Rule;
  onChange(r: Rule): void;
  /** Grupos estáticos, para el campo «Grupo principal». */
  groups: { id: string; name: string }[];
}

export default function RuleEditor({ rule, onChange, groups }: Props) {
  const [preview, setPreview] = useState<{ count: number; sample: { id: string; hostname: string }[] } | null>(null);
  const [error, setError] = useState("");
  const debounced = useDebounced(rule, 400);

  useEffect(() => {
    if (!ruleIsComplete(debounced)) {
      setPreview(null);
      return;
    }
    let cancelled = false;
    api<{ count: number; sample: { id: string; hostname: string }[] }>("POST", "/groups/preview", { rule: debounced })
      .then((p) => { if (!cancelled) { setPreview(p); setError(""); } })
      .catch((e) => { if (!cancelled) setError(e instanceof ApiError ? e.message : "No se ha podido calcular la vista previa."); });
    return () => { cancelled = true; };
  }, [debounced]);

  const set = (i: number, c: Condition) => onChange({ ...rule, conditions: rule.conditions.map((x, j) => (j === i ? c : x)) });

  return (
    <div className="rule-editor">
      <label className="field" style={{ maxWidth: "22rem" }}>
        <span>Los equipos deben cumplir</span>
        <select value={rule.match} onChange={(e) => onChange({ ...rule, match: e.target.value as Rule["match"] })}>
          <option value="all">todas las condiciones</option>
          <option value="any">cualquiera de las condiciones</option>
        </select>
      </label>
      {rule.conditions.map((c, i) => {
        const f = fieldDef(c.field);
        return (
          <fieldset key={i} className="rule-row" aria-label={`Condición ${i + 1}`}>
            <label>
              <span className="visually-hidden">Campo</span>
              <select aria-label="Campo" value={c.field} onChange={(e) => set(i, defaultCondition(e.target.value, groups))}>
                {FIELDS.map((x) => <option key={x.key} value={x.key}>{x.label}</option>)}
              </select>
            </label>
            <label>
              <span className="visually-hidden">Operador</span>
              <select aria-label="Operador" value={c.op} onChange={(e) => set(i, { ...c, op: e.target.value })}>
                {OPS[f.kind].map(([v, l]) => <option key={v} value={v}>{l}</option>)}
              </select>
            </label>
            <label className="rule-value">
              <span className="visually-hidden">Valor</span>
              {f.options ? (
                <select aria-label="Valor" value={String(c.value)} onChange={(e) => set(i, { ...c, value: e.target.value })}>
                  {f.options.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
                </select>
              ) : f.kind === "group" ? (
                <select aria-label="Valor" value={String(c.value)} onChange={(e) => set(i, { ...c, value: e.target.value })}>
                  {groups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
                </select>
              ) : f.kind === "days" ? (
                <input aria-label="Valor" type="number" min={0} value={c.value} onChange={(e) => set(i, { ...c, value: Number(e.target.value) })} />
              ) : (
                <input aria-label="Valor" value={String(c.value)} placeholder={f.kind === "version" ? "p. ej. 10.0.22631" : ""}
                  onChange={(e) => set(i, { ...c, value: e.target.value })} />
              )}
            </label>
            {rule.conditions.length > 1 && (
              <button type="button" className="btn btn-quiet" onClick={() => onChange({ ...rule, conditions: rule.conditions.filter((_, j) => j !== i) })}>Quitar</button>
            )}
          </fieldset>
        );
      })}
      {rule.conditions.length < 20 && (
        <button type="button" className="btn btn-small" onClick={() => onChange({ ...rule, conditions: [...rule.conditions, defaultCondition("hostname", groups)] })}>
          Añadir condición
        </button>
      )}
      <div className="rule-preview" aria-live="polite">
        {error ? (
          <p className="form-error">{error}</p>
        ) : preview ? (
          <>
            <p className="rule-count">{`${preview.count} ${preview.count === 1 ? "equipo cumple" : "equipos cumplen"} la regla ahora mismo.`}</p>
            {preview.sample.length > 0 && (
              <p className="muted small">{preview.sample.map((d) => d.hostname).join(", ")}{preview.count > preview.sample.length ? "…" : ""}</p>
            )}
          </>
        ) : (
          <p className="muted small">Completa las condiciones para ver cuántos equipos cumplen la regla.</p>
        )}
      </div>
    </div>
  );
}
