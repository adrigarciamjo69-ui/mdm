import type { ActivityPoint } from "../api/types";
import { formatNumber } from "../lib/format";

const DAYS = 90;
const PER_DAY_MAX = 24 * 60; // un check-in por minuto

/** Check-ins por día de los últimos 90 días, agregados a partir de los puntos por hora. */
export default function ActivityChart({ points, now = new Date() }: { points: ActivityPoint[]; now?: Date }) {
  const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  const perDay = new Array<number>(DAYS).fill(0);
  let total = 0;
  for (const p of points) {
    const t = new Date(p.hour).getTime();
    const day = DAYS - 1 - Math.floor((today - Date.UTC(new Date(t).getUTCFullYear(), new Date(t).getUTCMonth(), new Date(t).getUTCDate())) / 86_400_000);
    if (day >= 0 && day < DAYS) perDay[day] += p.checkins;
    total += p.checkins;
  }
  const max = Math.max(PER_DAY_MAX / 4, ...perDay);
  const activeDays = perDay.filter((n) => n > 0).length;
  return (
    <figure style={{ margin: 0 }}>
      <figcaption className="muted" style={{ marginBottom: "0.75rem" }}>
        {formatNumber(total)} {total === 1 ? "conexión" : "conexiones"} en los últimos 90 días, con actividad en {activeDays}{" "}
        {activeDays === 1 ? "día" : "días"}.
      </figcaption>
      <div className="activity" aria-hidden="true">
        {perDay.map((n, i) =>
          n === 0 ? <i key={i} className="none" /> : <i key={i} style={{ height: `${Math.max(4, (n / max) * 100)}%` }} />,
        )}
      </div>
      <div className="activity-axis" aria-hidden="true">
        <span>hace 90 días</span>
        <span>hoy</span>
      </div>
    </figure>
  );
}
