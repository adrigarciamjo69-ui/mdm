import AndroidEnrollment from "../components/AndroidEnrollment";
import { useSearchParams } from "react-router-dom";
import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { CreatedToken, EnrollmentToken, Group, TokenState } from "../api/types";
import ConfirmDialog from "../components/ConfirmDialog";
import CopyField from "../components/CopyField";
import EmptyState from "../components/EmptyState";
import { formatDate, formatNumber, relativeFuture } from "../lib/format";

const STATE: Record<TokenState, { label: string; className: string }> = {
  active: { label: "Activo", className: "status status-online" },
  exhausted: { label: "Agotado", className: "status status-inactive" },
  expired: { label: "Caducado", className: "status status-inactive" },
  revoked: { label: "Revocado", className: "status status-revoked" },
};

export default function Enrollment() {
  const [params, setParams] = useSearchParams();
  const android = params.get("plataforma") === "android";
  return (
    <>
      <div className="tabs" role="tablist" aria-label="Plataforma" style={{ marginBottom: "1rem" }}>
        <button role="tab" aria-selected={!android} className={!android ? "tab active" : "tab"} onClick={() => setParams({})}>Windows y Linux</button>
        <button role="tab" aria-selected={android} className={android ? "tab active" : "tab"} onClick={() => setParams({ plataforma: "android" })}>Android</button>
      </div>
      {android ? (
        <>
          <div className="page-head"><div><h1>Inscripción de móviles</h1>
            <p className="muted">Móviles Android de empresa: se inscriben escaneando un código QR al configurarlos por primera vez.</p></div></div>
          <AndroidEnrollment />
        </>
      ) : <AgentEnrollment />}
    </>
  );
}

function AgentEnrollment() {
  const queryClient = useQueryClient();
  const tokens = useQuery({ queryKey: ["tokens"], queryFn: () => api<EnrollmentToken[]>("GET", "/enrollment-tokens") });
  const [created, setCreated] = useState<CreatedToken | null>(null);
  const [revoking, setRevoking] = useState<EnrollmentToken | null>(null);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["tokens"] });

  async function revoke(t: EnrollmentToken) {
    await api("POST", `/enrollment-tokens/${t.id}/revoke`);
    setRevoking(null);
    await refresh();
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Inscripción</h1>
          <p className="muted">
            Crea un token y ejecuta el comando en cada equipo como administrador. El equipo aparecerá en la consola en uno o dos minutos.
          </p>
        </div>
      </div>

      {created ? (
        <section className="reveal" aria-labelledby="reveal-title">
          <h2 id="reveal-title">Token creado: {created.name}</h2>
          <p>Copia el token o uno de los comandos ahora: por seguridad, no se volverá a mostrar.</p>
          <CopyField label="Instalar en Linux" value={created.install.linux} />
          <CopyField label="Instalar en Windows (PowerShell como administrador)" value={created.install.windows} />
          <CopyField label="Token" value={created.token} />
          <button className="btn btn-primary" onClick={() => setCreated(null)}>Ya lo he copiado</button>
        </section>
      ) : (
        <CreateToken onCreated={(t) => { setCreated(t); void refresh(); }} />
      )}

      <section className="section">
        <h2>Tokens</h2>
        {tokens.error ? (
          <p className="form-error" role="alert">{tokens.error instanceof ApiError ? tokens.error.message : "No se han podido cargar los tokens."}</p>
        ) : !tokens.data ? (
          <p className="muted">Cargando…</p>
        ) : tokens.data.length === 0 ? (
          <EmptyState message="Aún no has creado ningún token." />
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th scope="col">Nombre</th><th scope="col">Grupo de destino</th><th scope="col">Estado</th>
                  <th scope="col" className="num">Equipos inscritos</th><th scope="col">Caducidad</th><th scope="col">Creado</th>
                  <th scope="col"><span className="visually-hidden">Acciones</span></th>
                </tr>
              </thead>
              <tbody>
                {tokens.data.map((t) => (
                  <tr key={t.id}>
                    <td><strong>{t.name}</strong></td>
                    <td>{t.group_name || <span className="muted">—</span>}</td>
                    <td><span className={STATE[t.state].className}>{STATE[t.state].label}</span></td>
                    <td className="num nowrap">{t.max_uses ? `${formatNumber(t.uses)} de ${formatNumber(t.max_uses)}` : formatNumber(t.uses)}</td>
                    <td className="nowrap">
                      {!t.expires_at ? <span className="muted">Sin caducidad</span> : t.state === "active" ? relativeFuture(t.expires_at) : formatDate(t.expires_at)}
                    </td>
                    <td className="nowrap">{formatDate(t.created_at)}</td>
                    <td className="num">
                      {t.state === "active" && <button className="btn btn-small" onClick={() => setRevoking(t)}>Revocar</button>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {revoking && (
        <ConfirmDialog
          title={`Revocar el token «${revoking.name}»`}
          body="Ya no se podrá usar para inscribir equipos. Los equipos ya inscritos no se ven afectados."
          confirmLabel="Revocar"
          danger
          onConfirm={() => revoke(revoking)}
          onCancel={() => setRevoking(null)}
        />
      )}
    </>
  );
}

function CreateToken({ onCreated }: { onCreated(t: CreatedToken): void }) {
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("GET", "/groups") });
  const [name, setName] = useState("");
  const [group, setGroup] = useState("");
  const [days, setDays] = useState("7");
  const [maxUses, setMaxUses] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    const body: Record<string, unknown> = { name: name.trim() };
    if (group) body.group_id = group;
    if (days) body.expires_at = new Date(Date.now() + Number(days) * 86_400_000).toISOString();
    if (maxUses) {
      const n = Number(maxUses);
      if (!Number.isInteger(n) || n < 1) {
        setError("El máximo de equipos debe ser un número entero mayor que cero.");
        return;
      }
      body.max_uses = n;
    }
    setBusy(true);
    try {
      onCreated(await api<CreatedToken>("POST", "/enrollment-tokens", body));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido crear el token.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="panel" onSubmit={submit}>
      <h2>Nuevo token</h2>
      <div className="inline-form">
        <label className="field">
          <span>Nombre</span>
          <input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} placeholder="Por ejemplo, Portátiles de Ventas" />
        </label>
        <label className="field">
          <span>Grupo de destino</span>
          <select value={group} onChange={(e) => setGroup(e.target.value)}>
            <option value="">Sin grupo</option>
            {groups.data?.filter((g) => g.kind !== "dynamic").map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
          </select>
        </label>
        <label className="field">
          <span>Caducidad</span>
          <select value={days} onChange={(e) => setDays(e.target.value)}>
            <option value="1">1 día</option>
            <option value="7">7 días</option>
            <option value="30">30 días</option>
            <option value="">Sin caducidad</option>
          </select>
        </label>
        <label className="field">
          <span>Máximo de equipos <span className="hint">vacío, sin límite</span></span>
          <input type="number" min={1} inputMode="numeric" value={maxUses} onChange={(e) => setMaxUses(e.target.value)} />
        </label>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      <button className="btn btn-primary" disabled={busy || !name.trim()}>Crear token</button>
    </form>
  );
}
