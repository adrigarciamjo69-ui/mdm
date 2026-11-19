import type { ReactNode } from "react";
import { Link, Navigate, useLocation } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import type { Role } from "../api/types";

export function RequireAuth({ children }: { children: ReactNode }) {
  const { me, loading, loggedOut } = useAuth();
  const location = useLocation();
  if (loading) return <p className="loading-screen">Cargando…</p>;
  if (!me) {
    const next = location.pathname + location.search;
    // Tras cerrar sesión no se recuerda la página: quien entre después puede ser otra persona.
    if (loggedOut) return <Navigate to="/login" replace />;
    return <Navigate to={next === "/" ? "/login" : `/login?next=${encodeURIComponent(next)}`} replace />;
  }
  return <>{children}</>;
}

export function RequireRole({ min, children }: { min: Role; children: ReactNode }) {
  const { can } = useAuth();
  if (!can(min)) {
    return (
      <div className="notice">
        <p>No tienes permiso para ver esta página.</p>
        <Link to="/">Ir al resumen</Link>
      </div>
    );
  }
  return <>{children}</>;
}

export function NotFound() {
  return (
    <div className="notice">
      <p>Esta página no existe.</p>
      <Link to="/">Ir al resumen</Link>
    </div>
  );
}
