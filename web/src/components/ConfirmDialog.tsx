import { useEffect, useId, useRef, useState } from "react";
import { ApiError } from "../api/client";

interface Props {
  title: string;
  body: string;
  confirmLabel: string;
  danger?: boolean;
  /** Texto que hay que escribir para habilitar la confirmación. */
  requireText?: string;
  onConfirm(): void | Promise<void>;
  onCancel(): void;
}

export default function ConfirmDialog({ title, body, confirmLabel, danger, requireText, onConfirm, onCancel }: Props) {
  const titleId = useId();
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const panel = useRef<HTMLDivElement>(null);
  const first = useRef<HTMLInputElement | HTMLButtonElement | null>(null);

  // Al abrir se lleva el foco al diálogo; al cerrar, vuelve a donde estaba.
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
        // El foco no sale del diálogo mientras está abierto.
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

  const ready = !requireText || typed === requireText;

  async function confirm() {
    setError("");
    setBusy(true);
    try {
      await onConfirm();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "No se ha podido completar la acción.");
      setBusy(false);
    }
  }

  return (
    <div className="dialog-backdrop">
      <div ref={panel} className="confirm" role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <h2 id={titleId}>{title}</h2>
        <p>{body}</p>
        {requireText && (
          <label className="field">
            <span>Escribe <strong>{requireText}</strong> para confirmar</span>
            <input
              ref={(el) => { first.current = el; }}
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
              spellCheck={false}
            />
          </label>
        )}
        {error && <p className="form-error" role="alert">{error}</p>}
        <div className="dialog-actions">
          <button
            ref={(el) => { if (!requireText) first.current = el; }}
            className="btn"
            onClick={onCancel}
            disabled={busy}
          >
            Cancelar
          </button>
          <button className={danger ? "btn btn-danger" : "btn btn-primary"} onClick={() => void confirm()} disabled={!ready || busy}>
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
