import type { CatalogDef, CatalogField, PolicySetting } from "../api/types";

/** Valida los parámetros contra el esquema; devuelve los errores por campo. */
export function validateParams(def: CatalogDef, params: Record<string, unknown>): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const f of def.fields) {
    const v = params[f.name];
    const empty = v === undefined || v === null || v === "" || (Array.isArray(v) && v.length === 0);
    if (empty) {
      if (f.required) errors[f.name] = "Obligatorio.";
      continue;
    }
    if (f.type === "int") {
      const n = Number(v);
      if (!Number.isInteger(n)) errors[f.name] = "Debe ser un número entero.";
      else if (f.min !== undefined && f.max !== undefined && (n < f.min || n > f.max)) errors[f.name] = `Debe estar entre ${f.min} y ${f.max}.`;
    }
    if (f.type === "string_list" && Array.isArray(v) && f.max !== undefined && v.length > f.max) {
      errors[f.name] = `Como mucho ${f.max} elementos.`;
    }
  }
  return errors;
}

/** Valores iniciales de un ajuste nuevo: los predeterminados del catálogo. */
export function defaultParams(def: CatalogDef): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of def.fields) if (f.default !== undefined) out[f.name] = f.default;
  return out;
}

function FieldInput({ f, value, onChange, error, disabled }: { f: CatalogField; value: unknown; onChange(v: unknown): void; error?: string; disabled?: boolean }) {
  const id = `f-${f.name}`;
  const common = { id, "aria-invalid": error ? true : undefined, disabled };
  let input;
  switch (f.type) {
    case "bool":
      return (
        <label className="check-field">
          <input type="checkbox" checked={value === true} onChange={(e) => onChange(e.target.checked)} disabled={disabled} />
          <span>{f.label}</span>
        </label>
      );
    case "int":
      input = <input {...common} type="number" min={f.min} max={f.max} value={value === undefined ? "" : String(value)}
        onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))} />;
      break;
    case "enum":
      input = (
        <select {...common} value={String(value ?? "")} onChange={(e) => onChange(e.target.value)}>
          <option value="">Elige…</option>
          {f.options?.map((o) => <option key={o} value={o}>{f.option_labels?.[o] ?? o}</option>)}
        </select>
      );
      break;
    case "text":
      input = <textarea {...common} className="code" rows={8} value={String(value ?? "")} onChange={(e) => onChange(e.target.value)} spellCheck={false} />;
      break;
    case "time":
      input = <input {...common} type="time" value={String(value ?? "")} onChange={(e) => onChange(e.target.value || undefined)} />;
      break;
    case "package":
      input = <input {...common} className="mono" placeholder="com.empresa.app" value={String(value ?? "")} onChange={(e) => onChange(e.target.value)} />;
      break;
    case "secret":
      // La API nunca devuelve el valor: «********» significa «sin cambios».
      input = <input {...common} type="password" autoComplete="new-password" value={String(value ?? "")} onChange={(e) => onChange(e.target.value)}
        onFocus={(e) => { if (e.target.value === "********") onChange(""); }} />;
      break;
    case "string_list":
      input = <textarea {...common} rows={4} value={Array.isArray(value) ? value.join("\n") : ""}
        onChange={(e) => onChange(e.target.value.split("\n").map((x) => x.trim()).filter(Boolean))} />;
      break;
    default:
      input = <input {...common} value={String(value ?? "")} onChange={(e) => onChange(e.target.value)} />;
  }
  return (
    <div className="field">
      <label htmlFor={id}>{f.label}{f.type === "string_list" && <span className="hint"> uno por línea</span>}</label>
      {input}
      {f.description && <span className="hint small">{f.description}</span>}
      {error && <span className="field-error">{error}</span>}
    </div>
  );
}

interface Props {
  def: CatalogDef;
  setting: PolicySetting;
  errors: Record<string, string>;
  readOnly?: boolean;
  onChange(s: PolicySetting): void;
  onRemove(): void;
}

/** Un ajuste de la política: modo, parámetros generados del esquema y quitar. */
export default function SettingForm({ def, setting, errors, readOnly, onChange, onRemove }: Props) {
  return (
    <fieldset className="setting" aria-label={def.title}>
      <legend>{def.title}</legend>
      <p className="muted small">{def.description}</p>
      <div className="setting-mode">
        <label>
          <span className="visually-hidden">Modo de {def.title}</span>
          <select value={setting.mode} disabled={readOnly} onChange={(e) => onChange({ ...setting, mode: e.target.value as PolicySetting["mode"] })}>
            <option value="audit" disabled={def.enforce_only}>Auditar</option>
            <option value="enforce" disabled={def.audit_only}>Aplicar</option>
          </select>
        </label>
        {def.audit_only && <span className="muted small">Este ajuste solo se puede auditar.</span>}
        {def.enforce_only && <span className="muted small">En Android, Google lo aplica siempre.</span>}
        {!readOnly && <button type="button" className="btn btn-quiet" onClick={onRemove}>Quitar</button>}
      </div>
      {def.fields.map((f) => (
        <FieldInput key={f.name} f={f} value={setting.params[f.name]} error={errors[f.name]} disabled={readOnly}
          onChange={(v) => onChange({ ...setting, params: { ...setting.params, [f.name]: v } })} />
      ))}
    </fieldset>
  );
}
