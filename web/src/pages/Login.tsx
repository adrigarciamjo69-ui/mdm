import { useEffect, useRef, useState, type FormEvent } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router-dom";
import QRCode from "qrcode";
import { api, ApiError } from "../api/client";
import type { Me } from "../api/types";
import { useAuth } from "../auth/AuthContext";

type Step = "credentials" | "totp" | "totp_setup";

/** Solo se vuelve a rutas internas, para que un enlace no pueda llevar fuera. */
export function safeNext(next: string | null): string {
  return next && next.startsWith("/") && !next.startsWith("//") && !next.startsWith("/login") ? next : "/";
}

function groupSecret(secret: string) {
  return secret.replace(/(.{4})/g, "$1 ").trim();
}

export default function Login() {
  const { me, signedIn } = useAuth();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const next = safeNext(params.get("next"));

  const [step, setStep] = useState<Step>("credentials");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [setup, setSetup] = useState<{ secret: string; qr: string } | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const codeRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (step !== "credentials") codeRef.current?.focus();
  }, [step, setup]);

  if (me) return <Navigate to={next} replace />;

  const fail = (e: unknown) => setError(e instanceof ApiError ? e.message : "Ha ocurrido un error inesperado.");

  async function submitCredentials(ev: FormEvent) {
    ev.preventDefault();
    setError("");
    setBusy(true);
    try {
      const res = await api<{ next: Step }>("POST", "/auth/login", { email, password });
      if (res.next === "totp_setup") {
        const s = await api<{ secret: string; uri: string }>("POST", "/auth/totp/setup");
        const svg = await QRCode.toString(s.uri, { type: "svg", margin: 1, errorCorrectionLevel: "M" });
        setSetup({ secret: s.secret, qr: "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg) });
      }
      setPassword("");
      setStep(res.next);
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }

  async function submitCode(ev: FormEvent) {
    ev.preventDefault();
    setError("");
    setBusy(true);
    try {
      const m = await api<Me>("POST", "/auth/totp/verify", { code: code.replace(/\s/g, "") });
      signedIn(m);
      navigate(next, { replace: true });
    } catch (e) {
      fail(e);
      setCode("");
      codeRef.current?.focus();
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login">
      <div className="login-panel">
        <p className="login-brand">MDM</p>
        {step === "credentials" ? (
          <form onSubmit={submitCredentials} noValidate>
            <h1>Iniciar sesión</h1>
            <label className="field">
              <span>Correo</span>
              <input type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus />
            </label>
            <label className="field">
              <span>Contraseña</span>
              <input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
            </label>
            {error && <p className="form-error" role="alert">{error}</p>}
            <button className="btn btn-primary btn-block" disabled={busy || !email || !password}>
              Iniciar sesión
            </button>
          </form>
        ) : (
          <form onSubmit={submitCode} noValidate>
            {step === "totp_setup" && setup ? (
              <>
                <h1>Configura el segundo factor</h1>
                <p className="muted">
                  Escanea el código con una aplicación de autenticación (Google Authenticator, Microsoft Authenticator, 1Password…)
                  y escribe el código de 6 cifras que te muestre.
                </p>
                <img className="qr" src={setup.qr} alt="Código QR para la aplicación de autenticación" width={184} height={184} />
                <p className="muted small">Si no puedes escanearlo, escribe esta clave en la aplicación:</p>
                <p className="secret">{groupSecret(setup.secret)}</p>
              </>
            ) : (
              <>
                <h1>Verificación</h1>
                <p className="muted">Escribe el código de 6 cifras de tu aplicación de autenticación.</p>
              </>
            )}
            <label className="field">
              <span>Código de verificación</span>
              <input
                ref={codeRef}
                inputMode="numeric"
                autoComplete="one-time-code"
                pattern="[0-9 ]*"
                maxLength={7}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                className="code-input"
              />
            </label>
            {error && <p className="form-error" role="alert">{error}</p>}
            <button className="btn btn-primary btn-block" disabled={busy || code.replace(/\s/g, "").length < 6}>
              Verificar
            </button>
          </form>
        )}
      </div>
    </main>
  );
}
