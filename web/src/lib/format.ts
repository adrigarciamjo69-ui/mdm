import type { Role, Status } from "../api/types";

const UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];

/** Tamaño en unidades binarias con coma decimal: «1,5 GB». */
export function formatBytes(n: number): string {
  if (!n || n < 0) return "0 B";
  let i = 0;
  let v = n;
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024;
    i++;
  }
  const text = Number.isInteger(v) || v >= 10 ? String(Math.round(v)) : v.toFixed(1).replace(".", ",");
  return `${text} ${UNITS[i]}`;
}

/** «hace 40 s», «hace 3 min», «hace 2 h», «hace 5 días» o «nunca». */
export function relativeTime(iso: string | null | undefined, now: Date = new Date()): string {
  if (!iso) return "nunca";
  const s = Math.max(0, Math.floor((now.getTime() - new Date(iso).getTime()) / 1000));
  if (s < 60) return `hace ${s} s`;
  if (s < 3600) return `hace ${Math.floor(s / 60)} min`;
  if (s < 86400) return `hace ${Math.floor(s / 3600)} h`;
  const d = Math.floor(s / 86400);
  return `hace ${d} ${d === 1 ? "día" : "días"}`;
}

const dateTime = new Intl.DateTimeFormat("es-ES", {
  day: "2-digit", month: "2-digit", year: "numeric", hour: "2-digit", minute: "2-digit",
});
const dateOnly = new Intl.DateTimeFormat("es-ES", { day: "numeric", month: "short", year: "numeric" });

export function formatDateTime(iso: string): string {
  return dateTime.format(new Date(iso));
}

export function formatDate(iso: string): string {
  return dateOnly.format(new Date(iso));
}

const integer = new Intl.NumberFormat("es-ES");
export function formatNumber(n: number): string {
  return integer.format(n);
}

const STATUS: Record<Status, string> = {
  online: "En línea", offline: "Desconectado", inactive: "Inactivo", revoked: "Revocado",
};
export function statusLabel(s: Status): string {
  return STATUS[s] ?? s;
}

const ROLES: Record<Role, string> = { admin: "Administrador", operator: "Operador", readonly: "Solo lectura" };
export function roleLabel(r: Role): string {
  return ROLES[r] ?? r;
}

export function platformLabel(p: string): string {
  return p === "windows" ? "Windows" : p === "linux" ? "Linux" : p === "android" ? "Android" : p;
}

/** «dentro de 5 días», «dentro de 3 h», «dentro de 20 min»; para fechas pasadas, relativeTime. */
export function relativeFuture(iso: string, now: Date = new Date()): string {
  const s = Math.floor((new Date(iso).getTime() - now.getTime()) / 1000);
  if (s <= 0) return relativeTime(iso, now);
  if (s < 3600) return `dentro de ${Math.max(1, Math.floor(s / 60))} min`;
  if (s < 86400) return `dentro de ${Math.round(s / 3600)} h`;
  // Se redondea: un token creado «a 5 días» debe decir 5, no 4, unos segundos después.
  const d = Math.round(s / 86400);
  return `dentro de ${d} ${d === 1 ? "día" : "días"}`;
}
