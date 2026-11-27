import { useEffect, useId, useRef, useState } from "react";

/**
 * Filtro de varios valores: un botón que abre una lista con casillas. Sin
 * nada marcado equivale a «todos».
 */
export default function MultiSelect({ label, all, options, value, onChange, text: fixedText }: {
  label: string;
  /** Texto fijo del botón (si no, resume lo elegido). */
  text?: string;
  /** Texto cuando no hay nada elegido, p. ej. «Todas las categorías». */
  all: string;
  options: [string, string][];
  value: string[];
  onChange(v: string[]): void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const id = useId();
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent | KeyboardEvent) => {
      if (e instanceof KeyboardEvent ? e.key === "Escape" : !ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", close);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("keydown", close);
    };
  }, [open]);

  const names = options.filter(([v]) => value.includes(v)).map(([, l]) => l);
  const text = fixedText ?? (value.length === 0 ? all : names.length === 1 ? names[0] : `${label}: ${value.length}`);
  const toggle = (v: string) => onChange(value.includes(v) ? value.filter((x) => x !== v) : [...value, v]);

  return (
    <div className="multi-select" ref={ref}>
      <button type="button" className={"multi-select-button" + (value.length && !fixedText ? " active" : "")} aria-haspopup="true"
        aria-expanded={open} aria-controls={id} aria-label={`${label}: ${value.length ? names.join(", ") : all}`}
        title={names.join(", ") || undefined} onClick={() => setOpen((o) => !o)}>
        {text}
      </button>
      {open && (
        <div className="multi-select-menu" id={id} role="group" aria-label={label}>
          {options.length === 0 && <p className="muted small">Sin opciones</p>}
          {options.map(([v, l]) => (
            <label key={v} className="multi-select-option">
              <input type="checkbox" checked={value.includes(v)} onChange={() => toggle(v)} /> {l}
            </label>
          ))}
          {value.length > 0 && (
            <button type="button" className="btn btn-quiet btn-small" onClick={() => onChange([])}>Quitar selección</button>
          )}
        </div>
      )}
    </div>
  );
}
