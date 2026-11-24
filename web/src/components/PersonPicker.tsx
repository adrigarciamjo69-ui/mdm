import { useEffect, useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, query } from "../api/client";
import type { DirectoryUser, Page, PersonRef } from "../api/types";
import { useDebounced } from "../lib/hooks";

/** Buscador de personas de Entra ID. value = null significa «sin persona». */
export default function PersonPicker({ label, value, onChange, required }: {
  label: string;
  value: PersonRef | null;
  onChange(p: PersonRef | null): void;
  required?: boolean;
}) {
  const id = useId();
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const q = useDebounced(text.trim(), 250);
  const results = useQuery({
    queryKey: ["directory-search", q],
    queryFn: () => api<Page<DirectoryUser>>("GET", "/directory/users" + query({ q, per_page: 8 })),
    enabled: open && q.length >= 2,
  });
  useEffect(() => {
    if (value) setText("");
  }, [value]);

  if (value) {
    return (
      <div className="field">
        <span>{label}</span>
        <div className="person-chip">
          <span><strong>{value.name}</strong> <span className="muted small">{value.email}</span></span>
          <button type="button" className="btn btn-quiet btn-small" onClick={() => onChange(null)} aria-label={`Quitar ${value.name}`}>Cambiar</button>
        </div>
      </div>
    );
  }
  return (
    <div className="field person-picker">
      <label htmlFor={id}>{label}{required ? "" : <span className="hint"> opcional</span>}</label>
      <input id={id} value={text} placeholder="Escribe nombre o correo…" autoComplete="off" role="combobox" aria-expanded={open && !!results.data}
        aria-controls={id + "-list"} onFocus={() => setOpen(true)} onChange={(e) => { setText(e.target.value); setOpen(true); }} />
      {open && q.length >= 2 && results.data && (
        <ul className="picker-list" id={id + "-list"} role="listbox">
          {results.data.items.length === 0 && <li className="muted small">Nadie coincide. ¿Está sincronizado Entra ID?</li>}
          {results.data.items.map((u) => (
            <li key={u.id} role="option" aria-selected={false}>
              <button type="button" onClick={() => { onChange({ id: u.id, name: u.display_name, email: u.email }); setOpen(false); }}>
                <strong>{u.display_name}</strong> <span className="muted small">{u.email}{u.department ? ` · ${u.department}` : ""}</span>
                {u.status !== "active" && <span className="tag">baja</span>}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
