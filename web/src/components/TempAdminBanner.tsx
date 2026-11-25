import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { CommandRow } from "../lib/commands";
import { activeTempAdmins } from "../lib/tempAdmin";
import RunDialog from "./RunDialog";

const time = (d: Date) => d.toLocaleString("es-ES", { hour: "2-digit", minute: "2-digit", day: "2-digit", month: "2-digit" });

/** Aviso de los permisos de administrador temporal vigentes en un equipo. */
export default function TempAdminBanner({ deviceId, hostname }: { deviceId: string; hostname: string }) {
  const { can } = useAuth();
  const navigate = useNavigate();
  const [revoke, setRevoke] = useState<string | null>(null);
  const cmds = useQuery({ queryKey: ["device-commands", deviceId], queryFn: () => api<CommandRow[]>("GET", `/devices/${deviceId}/commands`), refetchInterval: 15_000 });
  const grants = activeTempAdmins(cmds.data ?? [], new Date());
  if (grants.length === 0) return null;
  return (
    <>
      {grants.map((g) => (
        <p key={g.user} className="banner">
          {g.user || "El usuario con la sesión iniciada"} es administrador temporal hasta {time(g.expiresAt)}{g.reason && <> · {g.reason}</>}.
          {can("admin") && <> <button className="btn btn-small" onClick={() => setRevoke(g.user)}>Quitar ya</button></>}
        </p>
      ))}
      {revoke !== null && (
        <RunDialog type="temp_admin_revoke" presetUser={revoke} target={{ deviceIds: [deviceId], label: hostname }}
          onClose={() => setRevoke(null)} onDone={(id) => navigate(`/actividad/${id}`)} />
      )}
    </>
  );
}
