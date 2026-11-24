import { useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { roleLabel } from "../lib/format";
import PasswordDialog from "./PasswordDialog";
import type { Role } from "../api/types";

const links: { to: string; label: string; min: Role; end?: boolean }[] = [
  { to: "/", label: "Resumen", min: "readonly", end: true },
  { to: "/alertas", label: "Alertas", min: "readonly" },
  { to: "/inventario", label: "Inventario", min: "readonly" },
  { to: "/usuarios", label: "Usuarios", min: "readonly" },
  { to: "/licencias", label: "Licencias", min: "readonly" },
  { to: "/actas", label: "Doc. Entrega", min: "readonly" },
  { to: "/grupos", label: "Grupos", min: "readonly" },
  { to: "/politicas", label: "Políticas", min: "readonly" },
  { to: "/software", label: "Software", min: "readonly" },
  { to: "/despliegues", label: "Despliegues", min: "readonly" },
  { to: "/parches", label: "Parches", min: "readonly" },
  { to: "/scripts", label: "Scripts", min: "readonly" },
  { to: "/actividad", label: "Actividad", min: "readonly" },
  { to: "/automatizaciones", label: "Automatizaciones", min: "readonly" },
  { to: "/informes", label: "Informes", min: "readonly" },
  { to: "/inscripcion", label: "Inscripción", min: "operator" },
  { to: "/administradores", label: "Administradores", min: "admin" },
  { to: "/auditoria", label: "Auditoría", min: "admin" },
  { to: "/integraciones/avisos", label: "Avisos", min: "admin" },
  { to: "/integraciones/android", label: "Android", min: "admin" },
  { to: "/ajustes", label: "Ajustes", min: "admin" },
];

/** Alertas sin resolver, para el contador del menú. */
function useOpenAlerts(enabled: boolean): number {
  const q = useQuery({ queryKey: ["alert-counts"], queryFn: () => api<Record<string, number>>("GET", "/alerts/counts"), enabled, refetchInterval: 60_000 });
  return Object.values(q.data ?? {}).reduce((a, b) => a + b, 0);
}

export default function Layout() {
  const { me, can, logout } = useAuth();
  const openAlerts = useOpenAlerts(!!me);
  const [changing, setChanging] = useState(false);
  const [notice, setNotice] = useState("");
  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">MDM</div>
        <nav aria-label="Principal">
          <ul>
            {links.filter((l) => can(l.min)).map((l) => (
              <li key={l.to}>
                <NavLink to={l.to} end={l.end}>
                  {l.label}
                  {l.to === "/alertas" && openAlerts > 0 && <> <span className="nav-badge">{openAlerts}</span><span className="visually-hidden"> sin resolver</span></>}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>
        {me && (
          <div className="whoami">
            <span className="whoami-email">{me.email}</span>
            <span className="whoami-role">{roleLabel(me.role)}</span>
            <button className="btn btn-quiet" onClick={() => setChanging(true)}>Cambiar mi contraseña</button>
            <button className="btn btn-quiet" onClick={() => void logout()}>Cerrar sesión</button>
          </div>
        )}
      </aside>
      <main className="content" id="contenido">
        {notice && (
          <p className="form-ok" role="status">
            {notice} <button className="btn btn-quiet btn-small" onClick={() => setNotice("")}>Cerrar</button>
          </p>
        )}
        <Outlet />
      </main>
      {changing && (
        <PasswordDialog onDone={(msg) => { setChanging(false); setNotice(msg); }} onCancel={() => setChanging(false)} />
      )}
    </div>
  );
}
