import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { getCsrfToken } from "../api/client";

interface Row { line: number; name: string; errors: string[] }
interface Result { rows: Row[]; valid: number; invalid: number; created: number }

async function post(csv: string, dryRun: boolean): Promise<{ ok: boolean; data: Result & { error?: { message: string } } }> {
  const csrf = getCsrfToken();
  const res = await fetch(`/api/v1/assets/import${dryRun ? "?dry_run=1" : ""}`, {
    method: "POST", body: csv, credentials: "same-origin",
    headers: { "Content-Type": "text/csv", Accept: "application/json", ...(csrf ? { "X-CSRF-Token": csrf } : {}) },
  });
  const data = await res.json().catch(() => ({ rows: [], valid: 0, invalid: 0, created: 0, error: { message: "Respuesta no válida del servidor." } }));
  return { ok: res.ok, data };
}

function readText(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result ?? ""));
    r.onerror = () => reject(r.error);
    r.readAsText(file, "utf-8");
  });
}

export default function AssetImport() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [csv, setCsv] = useState("");
  const [result, setResult] = useState<Result | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function load(file: File) {
    setError("");
    setResult(null);
    if (file.size > 5 * 1024 * 1024) return setError("El fichero supera los 5 MB.");
    const text = await readText(file);
    setCsv(text);
    setBusy(true);
    try {
      const { ok, data } = await post(text, true);
      if (!ok) setError(data.error?.message ?? "No se ha podido revisar el fichero.");
      else setResult(data);
    } catch {
      setError("No hay conexión con el servidor.");
    } finally {
      setBusy(false);
    }
  }

  async function doImport() {
    setBusy(true);
    setError("");
    try {
      const { ok, data } = await post(csv, false);
      if (!ok) {
        setError(data.error?.message ?? "No se ha podido importar.");
        if (data.rows) setResult({ ...data, valid: 0, invalid: data.rows.filter((r) => r.errors.length).length });
        return;
      }
      void queryClient.invalidateQueries({ queryKey: ["assets"] });
      navigate("/inventario?origin=manual", { replace: true });
    } finally {
      setBusy(false);
    }
  }

  const withErrors = result?.rows.filter((r) => r.errors.length > 0) ?? [];
  return (
    <>
      <p className="small" style={{ marginBottom: "0.75rem" }}><Link to="/inventario">Inventario</Link></p>
      <h1>Importar activos desde CSV</h1>
      <div className="panel">
        <p>Descarga la <a href="/api/v1/assets/import/template.csv" download>plantilla</a>, rellénala en Excel y guárdala como CSV (UTF-8). Separadores admitidos: punto y coma o coma.</p>
        <ul className="small muted">
          <li><strong>categoria</strong> tiene que existir (Portátil, Monitor, Impresora…). <strong>estado</strong>: en_uso, en_almacen, en_reparacion o baja (vacío = en_almacen).</li>
          <li><strong>persona</strong> es el correo de alguien de Usuarios. Fechas como 2026-01-15. Precio como 150,50.</li>
          <li>Antes de guardar se revisa todo el fichero. Si una fila tiene errores, no se guarda ninguna.</li>
        </ul>
        <label className="field"><span>Fichero CSV</span>
          <input type="file" accept=".csv,text/csv" onChange={(e) => e.target.files?.[0] && void load(e.target.files[0])} disabled={busy} />
        </label>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      {result && (
        <div className="panel">
          <p><strong>{result.valid}</strong> filas correctas · <strong>{result.invalid}</strong> con errores</p>
          {withErrors.length > 0 && (
            <div className="table-wrap">
              <table>
                <thead><tr><th scope="col">Línea</th><th scope="col">Nombre</th><th scope="col">Errores</th></tr></thead>
                <tbody>
                  {withErrors.map((r) => (
                    <tr key={r.line}><td className="num">{r.line}</td><td>{r.name || <span className="muted">—</span>}</td>
                      <td><ul className="plain-list">{r.errors.map((e) => <li key={e}>{e}</li>)}</ul></td></tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <button className="btn btn-primary" disabled={busy || result.invalid > 0 || result.valid === 0} onClick={() => void doImport()}>
            Importar {result.valid} {result.valid === 1 ? "activo" : "activos"}
          </button>
          {result.invalid > 0 && <p className="muted small">Corrige las filas con errores en el fichero y vuelve a cargarlo.</p>}
        </div>
      )}
    </>
  );
}
