import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { api, ApiError } from "../api/client";

interface Props {
  /** Cuenta de otra persona a la que un administrador fija la contraseña. Sin ella, la propia. */
  target?: { id: string; email: string };
  onDone(message: string): void;
  onCancel(): void;
}

/** Diálogo para cambiar la contraseña propia o restablecer la de otra cuenta. */
export default function PasswordDialog({ target, onDone, onCancel }: Props) {
  const titleId = useId();
  const own = !target;
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [repeat, setRepeat] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const panel = useRef<HTMLFormElement>(null);
  const first = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    first.current?.focus();
    return () => previous?.focus?.();
  }, []);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape" && !busy) {
        e.preventDefault();
        onCancel();
      }
      if (e.key === "Tab" && panel.current) {
        const items = panel.current.querySelectorAll<HTMLElement>("button:not(:disabled), input");
        if (items.length === 0) return;
        const firstEl = items[0], lastEl = items[items.length - 1];
        if (e.shiftKey && document.activeElement === firstEl) { e.preventDefault(); lastEl.focus(); }
        else if (!e.shiftKey && document.activeElement === lastEl) { e.preventDefault(); firstEl.focus(); }
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [busy, onCancel]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    if ([...next].length < 12) {
      setError("La contraseña debe tener al menos 12 caracteres.");
      return;
    }
    if (next !== repeat) {
      setError("Las contraseñas no coinciden.");
      return;
    }
    setBusy(true);
    try {
      if (own) {
        await api("POST", "/auth/password", { current_password: current, new_password: next });
        onDone("Contraseña cambiada. Se han cerrado tus otras sesiones abiertas.");
      } else {
        await api("POST", `/admins/${target.id}/password`, { password: next });
        onDone(`Contraseña de ${target.email} cambiada y sus sesiones cerradas. Hazle llegar la nueva de forma segura.`);
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido cambiar la contraseña.");
      setBusy(false);
    }
  }

  return (
    <div className="dialog-backdrop">
      <form ref={panel} className="confirm" role="dialog" aria-modal="true" aria-labelledby={titleId} onSubmit={submit} noValidate>
        <h2 id={titleId}>{own ? "Cambiar mi contraseña" : `Restablecer la contraseña de ${target.email}`}</h2>
        {own && (
          <label className="field">
            <span>Contraseña actual</span>
            <input ref={first} type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" />
          </label>
        )}
        <label className="field">
          <span>Contraseña nueva</span>
          <input ref={own ? undefined : first} type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" />
        </label>
        <label className="field">
          <span>Repite la contraseña nueva</span>
          <input type="password" value={repeat} onChange={(e) => setRepeat(e.target.value)} autoComplete="new-password" />
        </label>
        <p className="muted small">Mínimo 12 caracteres.{own ? "" : " Se cerrarán sus sesiones abiertas."}</p>
        {error && <p className="form-error" role="alert">{error}</p>}
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={onCancel} disabled={busy}>Cancelar</button>
          <button type="submit" className="btn btn-primary" disabled={busy || !next || !repeat || (own && !current)}>
            Cambiar contraseña
          </button>
        </div>
      </form>
    </div>
  );
}
