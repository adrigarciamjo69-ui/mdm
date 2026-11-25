import type { CommandRow } from "./commands";

export interface TempAdminGrant {
  /** Vacío: el usuario que tenía la sesión iniciada. */
  user: string;
  reason: string;
  expiresAt: Date;
}

const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

/**
 * Permisos de administrador temporal vigentes según la actividad del equipo:
 * las concesiones correctas que no han caducado ni se han retirado después.
 * El agente los quita él solo al caducar.
 */
export function activeTempAdmins(rows: CommandRow[], now: Date): TempAdminGrant[] {
  const done = rows.filter((r) => r.status === "succeeded" && r.finished_at).sort((a, b) => a.finished_at!.localeCompare(b.finished_at!));
  const grants: (TempAdminGrant & { at: string })[] = [];
  for (const r of done) {
    const user = String(r.params?.user ?? "");
    if (r.type === "temp_admin") {
      const minutes = Number(r.params?.minutes ?? 0);
      const expiresAt = new Date(new Date(r.finished_at!).getTime() + minutes * 60_000);
      const prev = grants.findIndex((g) => same(g.user, user));
      if (prev >= 0) grants.splice(prev, 1);
      grants.push({ user, reason: String(r.params?.reason ?? ""), expiresAt, at: r.finished_at! });
    } else if (r.type === "temp_admin_revoke") {
      for (let i = grants.length - 1; i >= 0; i--) if (user === "" || same(grants[i].user, user)) grants.splice(i, 1);
    }
  }
  return grants.filter((g) => g.expiresAt > now).map(({ user, reason, expiresAt }) => ({ user, reason, expiresAt }));
}
