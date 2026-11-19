import type { Detection, Platform } from "../api/types";

const INTERPRETERS: Record<string, string[]> = { windows: ["powershell"], linux: ["sh", "bash", "python3"] };

/** Editor de la regla que decide si una aplicación está instalada. */
export default function DetectionEditor({ value, onChange, platform }: { value: Detection; onChange(d: Detection): void; platform: Platform }) {
  const set = (patch: Partial<Detection>) => onChange({ ...value, ...patch });
  const windows = platform === "windows";
  return (
    <fieldset className="params">
      <legend>Cómo saber si está instalada</legend>
      <label className="field" style={{ maxWidth: "24rem" }}>
        <span>Detección</span>
        <select value={value.type} onChange={(e) => onChange({ type: e.target.value as Detection["type"], interpreter: INTERPRETERS[platform]?.[0] })}>
          <option value="inventory">Aparece en el software del inventario</option>
          {windows && <option value="file">Existe un fichero</option>}
          {windows && <option value="registry">Existe una clave del registro</option>}
          <option value="script">Script propio</option>
        </select>
      </label>
      <div className="edit-grid">
        {value.type === "inventory" && (
          <label className="field">
            <span>El nombre del programa contiene</span>
            <input value={value.name_contains ?? ""} onChange={(e) => set({ name_contains: e.target.value })} />
          </label>
        )}
        {value.type === "file" && (
          <label className="field">
            <span>Ruta del fichero</span>
            <input value={value.path ?? ""} placeholder="%ProgramFiles%\Programa\programa.exe" onChange={(e) => set({ path: e.target.value })} />
          </label>
        )}
        {value.type === "registry" && (
          <>
            <label className="field">
              <span>Clave del registro</span>
              <input value={value.registry_key ?? ""} placeholder="HKLM\SOFTWARE\Empresa\Programa" onChange={(e) => set({ registry_key: e.target.value })} />
            </label>
            <label className="field">
              <span>Valor <span className="hint">opcional</span></span>
              <input value={value.registry_value ?? ""} onChange={(e) => set({ registry_value: e.target.value })} />
            </label>
          </>
        )}
        {value.type !== "script" && (
          <label className="field">
            <span>Versión mínima <span className="hint">opcional</span></span>
            <input value={value.min_version ?? ""} onChange={(e) => set({ min_version: e.target.value })} />
          </label>
        )}
      </div>
      {value.type === "script" && (
        <>
          <label className="field" style={{ maxWidth: "16rem" }}>
            <span>Intérprete</span>
            <select value={value.interpreter ?? INTERPRETERS[platform][0]} onChange={(e) => set({ interpreter: e.target.value })}>
              {INTERPRETERS[platform].map((i) => <option key={i}>{i}</option>)}
            </select>
          </label>
          <div className="field">
            <label htmlFor="det-script">Script de detección</label>
            <textarea id="det-script" className="code" rows={6} value={value.script ?? ""} onChange={(e) => set({ script: e.target.value })} spellCheck={false}
              aria-describedby="det-script-hint" />
            <span id="det-script-hint" className="hint">Termina con código 0 si la aplicación está instalada.</span>
          </div>
        </>
      )}
    </fieldset>
  );
}
