import { useRef, useState } from "react";
import { getCsrfToken } from "../api/client";

export interface Uploaded {
  sha256: string;
  size: number;
  original_name: string;
}

/** Sube un instalador con barra de progreso (XMLHttpRequest informa del envío; fetch no). */
export default function Upload({ onUploaded }: { onUploaded(f: Uploaded): void }) {
  const [progress, setProgress] = useState<number | null>(null);
  const [message, setMessage] = useState("");
  const xhr = useRef<XMLHttpRequest | null>(null);

  function start(file: File) {
    setMessage("");
    setProgress(0);
    const req = new XMLHttpRequest();
    xhr.current = req;
    req.open("POST", "/api/v1/files");
    const csrf = getCsrfToken();
    if (csrf) req.setRequestHeader("X-CSRF-Token", csrf);
    req.upload.onprogress = (e) => {
      if (e.lengthComputable) setProgress(Math.floor((e.loaded / e.total) * 100));
    };
    req.onload = () => {
      xhr.current = null;
      setProgress(null);
      if (req.status === 201) {
        onUploaded(JSON.parse(req.responseText) as Uploaded);
        setMessage("Fichero subido.");
        return;
      }
      try {
        setMessage((JSON.parse(req.responseText) as { error: { message: string } }).error.message);
      } catch {
        setMessage("No se ha podido subir el fichero.");
      }
    };
    req.onerror = () => {
      xhr.current = null;
      setProgress(null);
      setMessage("La subida se interrumpió. Comprueba la conexión y vuelve a intentarlo.");
    };
    req.onabort = () => {
      xhr.current = null;
      setProgress(null);
      setMessage("Subida cancelada.");
    };
    const form = new FormData();
    form.append("file", file);
    req.send(form);
  }

  return (
    <div className="upload">
      <label className="field">
        <span>Instalador</span>
        <input type="file" accept=".msi,.exe,.deb,.rpm" disabled={progress !== null}
          onChange={(e) => { const f = e.target.files?.[0]; if (f) start(f); }} />
      </label>
      {progress !== null && (
        <div className="upload-progress">
          <div className="progress" aria-hidden="true"><span className="progress-ok" style={{ width: `${progress}%` }} /></div>
          <span className="small" role="status">{progress} %</span>
          <button type="button" className="btn btn-small" onClick={() => xhr.current?.abort()}>Cancelar subida</button>
        </div>
      )}
      {message && <p className="small" role="status">{message}</p>}
    </div>
  );
}
