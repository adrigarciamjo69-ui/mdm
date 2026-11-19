/** Gráfica de líneas en SVG, sin bibliotecas: una o varias series por día. */
export interface Series {
  label: string;
  color: string;
  points: { day: string; value: number }[];
}

export default function TrendChart({ title, series, unit = "", height = 140 }: { title: string; series: Series[]; unit?: string; height?: number }) {
  const days = Array.from(new Set(series.flatMap((s) => s.points.map((p) => p.day)))).sort();
  const width = 560;
  const pad = { l: 36, r: 8, t: 8, b: 20 };
  if (days.length === 0) {
    return <p className="muted small">{title}: aún no hay datos (se toma una instantánea cada día).</p>;
  }
  const max = Math.max(1, ...series.flatMap((s) => s.points.map((p) => p.value)));
  const x = (d: string) => pad.l + (days.length === 1 ? (width - pad.l - pad.r) / 2 : (days.indexOf(d) * (width - pad.l - pad.r)) / (days.length - 1));
  const y = (v: number) => pad.t + (height - pad.t - pad.b) * (1 - v / max);
  const last = series.map((s) => `${s.label}: ${s.points.at(-1)?.value ?? "—"}${unit}`).join(", ");
  const fmt = (d: string) => `${d.slice(8, 10)}/${d.slice(5, 7)}`;
  return (
    <figure className="trend">
      <figcaption className="small"><strong>{title}</strong></figcaption>
      <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`${title}. Último día: ${last}.`}>
        {[0, 0.5, 1].map((f) => (
          <g key={f}>
            <line x1={pad.l} x2={width - pad.r} y1={y(max * f)} y2={y(max * f)} className="trend-grid" />
            <text x={pad.l - 4} y={y(max * f) + 4} textAnchor="end" className="trend-label">{Math.round(max * f)}</text>
          </g>
        ))}
        <text x={pad.l} y={height - 4} className="trend-label">{fmt(days[0])}</text>
        <text x={width - pad.r} y={height - 4} textAnchor="end" className="trend-label">{fmt(days[days.length - 1])}</text>
        {series.map((s) => (
          <polyline key={s.label} fill="none" stroke={s.color} strokeWidth={2} strokeLinejoin="round"
            points={s.points.map((p) => `${x(p.day)},${y(p.value)}`).join(" ")} />
        ))}
      </svg>
      {series.length > 1 && (
        <ul className="trend-legend small">
          {series.map((s) => <li key={s.label}><span style={{ background: s.color }} />{s.label}</li>)}
        </ul>
      )}
    </figure>
  );
}
