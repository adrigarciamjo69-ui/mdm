// Textos y utilidades de los parches.
export interface MaintWindow {
  days: number[];
  start: string;
  end: string;
}

const DAY = ["domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"];
const DAY_PLURAL = ["domingos", "lunes", "martes", "miércoles", "jueves", "viernes", "sábados"];

export const CLASSES: [string, string][] = [
  ["security", "Seguridad"], ["critical", "Críticas"], ["definition", "Definiciones (antivirus)"], ["update", "Actualizaciones"],
  ["feature_pack", "Paquetes de características"], ["driver", "Controladores"], ["other", "Otras"],
];
export const CLASS_LABEL: Record<string, string> = Object.fromEntries(CLASSES);

function describeDays(days: number[]): string {
  const d = [...days].sort((a, b) => a - b);
  if (d.length === 7) return "Todos los días";
  if (d.join() === "1,2,3,4,5") return "De lunes a viernes";
  if (d.join() === "0,6") return "Los fines de semana";
  if (d.length === 1) return `Los ${DAY_PLURAL[d[0]]}`;
  const names = d.map((x) => DAY[x]);
  return "Los " + names.slice(0, -1).join(", ") + " y " + names[names.length - 1];
}

/** Describe la ventana en palabras: «De lunes a viernes, de 22:00 a 05:00 del día siguiente». */
export function describeWindow(w: MaintWindow): string {
  if (w.days.length === 0) return "Sin días: nunca";
  const days = describeDays(w.days);
  if (w.start === w.end) return `${days}, todo el día`;
  return `${days}, de ${w.start} a ${w.end}${w.end < w.start ? " del día siguiente" : ""}`;
}

/** Subtítulo de una actualización: su KB si no está ya en el título; en Linux, el paquete. */
export function updateSubtitle(u: { id: string; kb: string; title: string }, platform = "windows"): string {
  if (u.kb && !u.title.includes(u.kb)) return u.kb;
  if (!u.kb && platform === "linux") return u.id;
  return "";
}
