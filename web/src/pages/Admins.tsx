import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import type { Admin, Role } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import ConfirmDialog from "../components/ConfirmDialog";
import PasswordDialog from "../components/PasswordDialog";
import { formatDateTime, relativeTime, roleLabel } from "../lib/format";

const ROLES: Role[] = ["readonly", "operator", "admin"];
const ROLE_HELP: Record<Role, string> = {
  readonly: "Consulta el parque y exporta listados.",
  operator: "Además, organiza equipos en grupos y crea tokens de inscripción.",
  admin: "Además, revoca y elimina equipos, gestiona cuentas y consulta la auditoría.",
};

export default function Admins() {
  const { me } = useAuth();
  const queryClient = useQueryClient();
  const admins = useQuery({ queryKey: ["admins"], queryFn: () => api<Admin[]>("GET", "/admins") });
  const [disabling, setDisabling] = useState<Admin | null>(null);
  const [resetting, setResetting] = useState<Admin | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["admins"] });

  async function patch(a: Admin, body: Partial<Pick<Admin, "role" | "disabled">>) {
    setError("");
    try {
      await api("PATCH", `/admins/${a.id}`, body);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido guardar el cambio.");
    }
  }

  async function disable(a: Admin) {
    await api("PATCH", `/admins/${a.id}`, { disabled: true });
    setDisabling(null);
    await refresh();
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Administradores</h1>
          <p className="muted">Cuentas con acceso a esta consola. Todas usan segundo factor.</p>
        </div>
      </div>

      <CreateAdmin onCreated={refresh} />

      <section className="section">
        <h2>Cuentas</h2>
        {error && <p className="form-error" role="alert">{error}</p>}
        {notice && <p className="form-ok" role="status">{notice}</p>}
        {admins.error ? (
          <p className="form-error" role="alert">No se han podido cargar las cuentas.</p>
        ) : !admins.data ? (
          <p className="muted">Cargando…</p>
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th scope="col">Correo</th><th scope="col">Rol</th><th scope="col">Segundo factor</th>
                  <th scope="col">Último acceso</th><th scope="col">Estado</th>
                  <th scope="col"><span className="visually-hidden">Acciones</span></th>
                </tr>
              </thead>
              <tbody>
                {admins.data.map((a) => {
                  const self = a.id === me?.id;
                  return (
                    <tr key={a.id}>
                      <td><strong>{a.email}</strong></td>
                      <td>
                        {self ? (
                          roleLabel(a.role)
                        ) : (
                          <select aria-label={`Rol de ${a.email}`} value={a.role} onChange={(e) => void patch(a, { role: e.target.value as Role })} style={{ width: "auto" }}>
                            {ROLES.map((r) => <option key={r} value={r}>{roleLabel(r)}</option>)}
                          </select>
                        )}
                      </td>
                      <td>{a.has_totp ? "Configurado" : <span className="state-partial">Pendiente de configurar</span>}</td>
                      <td className="nowrap" title={a.last_login_at ? formatDateTime(a.last_login_at) : undefined}>{relativeTime(a.last_login_at)}</td>
                      <td>{a.disabled ? <span className="status status-inactive">Desactivada</span> : <span className="status status-online">Activa</span>}</td>
                      <td className="num nowrap">
                        {self ? (
                          <span className="muted">Tu cuenta</span>
                        ) : (
                          <>
                            <button className="btn btn-small" aria-label={`Restablecer la contraseña de ${a.email}`} onClick={() => { setNotice(""); setResetting(a); }}>
                              Restablecer contraseña
                            </button>{" "}
                            {a.disabled ? (
                              <button className="btn btn-small" onClick={() => void patch(a, { disabled: false })}>Reactivar</button>
                            ) : (
                              <button className="btn btn-small" onClick={() => setDisabling(a)}>Desactivar</button>
                            )}
                          </>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {disabling && (
        <ConfirmDialog
          title={`Desactivar la cuenta de ${disabling.email}`}
          body="No podrá iniciar sesión y se cerrarán sus sesiones abiertas. Puedes reactivarla más adelante."
          confirmLabel="Desactivar"
          danger
          onConfirm={() => disable(disabling)}
          onCancel={() => setDisabling(null)}
        />
      )}

      {resetting && (
        <PasswordDialog
          target={resetting}
          onDone={(msg) => { setResetting(null); setNotice(msg); }}
          onCancel={() => setResetting(null)}
        />
      )}
    </>
  );
}

function CreateAdmin({ onCreated }: { onCreated(): void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Role>("readonly");
  const [error, setError] = useState("");
  const [done, setDone] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setDone("");
    if ([...password].length < 12) {
      setError("La contraseña debe tener al menos 12 caracteres.");
      return;
    }
    setBusy(true);
    try {
      await api("POST", "/admins", { email: email.trim(), password, role });
      setDone(`Cuenta creada para ${email.trim()}. Comparte la contraseña inicial de forma segura; en su primer acceso configurará el segundo factor.`);
      setEmail("");
      setPassword("");
      setRole("readonly");
      onCreated();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se ha podido crear la cuenta.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="panel" onSubmit={submit} noValidate>
      <h2>Nueva cuenta</h2>
      <div className="inline-form">
        <label className="field">
          <span>Correo</span>
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="off" />
        </label>
        <label className="field">
          <span>Contraseña inicial</span>
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
        </label>
        <label className="field">
          <span>Rol</span>
          <select value={role} onChange={(e) => setRole(e.target.value as Role)}>
            {ROLES.map((r) => <option key={r} value={r}>{roleLabel(r)}</option>)}
          </select>
        </label>
      </div>
      <p className="muted small" style={{ marginBottom: "1rem" }}>{roleLabel(role)}: {ROLE_HELP[role]}</p>
      {error && <p className="form-error" role="alert">{error}</p>}
      {done && <p className="form-ok" role="status" style={{ marginBottom: "1rem" }}>{done}</p>}
      <button className="btn btn-primary" disabled={busy || !email.trim() || !password}>Crear cuenta</button>
    </form>
  );
}
