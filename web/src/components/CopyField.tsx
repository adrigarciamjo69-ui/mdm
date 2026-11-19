import { useEffect, useRef, useState } from "react";

/** Valor de solo lectura con botón para copiarlo al portapapeles. */
export default function CopyField({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false);
  const code = useRef<HTMLElement>(null);

  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(t);
  }, [copied]);

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
    } catch {
      // Sin acceso al portapapeles (por ejemplo, fuera de HTTPS): se selecciona el texto para copiarlo a mano.
      const range = document.createRange();
      if (code.current) {
        range.selectNodeContents(code.current);
        window.getSelection()?.removeAllRanges();
        window.getSelection()?.addRange(range);
      }
    }
  }

  return (
    <div className="copy-field">
      <span>{label}</span>
      <div className="copy-row">
        <code ref={code}>{value}</code>
        <button type="button" className="btn btn-small" onClick={() => void copy()} aria-live="polite">
          {copied ? "Copiado" : "Copiar"}
        </button>
      </div>
    </div>
  );
}
