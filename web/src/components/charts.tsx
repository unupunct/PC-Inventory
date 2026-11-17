// Dependency-free sparkline / line chart on an SVG canvas.
// Values are real samples from the API; nothing is smoothed or invented.

export interface Point {
  x: number; // timestamp ms
  y: number;
}

export function Spark({
  series,
  height = 64,
  color = "#4c8dff",
  fill = true,
  min,
  max,
  areaOpacity = 0.18,
}: {
  series: Point[];
  height?: number;
  color?: string;
  fill?: boolean;
  min?: number;
  max?: number;
  areaOpacity?: number;
}) {
  const width = 1000; // viewBox width; scales to the container
  const pad = 4;
  if (!series.length) {
    return (
      <div className="flex h-[64px] items-center justify-center text-[12px] text-muted" style={{ height }}>
        no samples yet
      </div>
    );
  }
  const xs = series.map((p) => p.x);
  const ys = series.map((p) => p.y);
  const x0 = Math.min(...xs);
  const x1 = Math.max(...xs);
  const y0 = min ?? Math.min(...ys);
  const y1 = max ?? Math.max(...ys);
  const spanX = x1 - x0 || 1;
  const spanY = y1 - y0 || 1;
  const sx = (x: number) => pad + ((x - x0) / spanX) * (width - pad * 2);
  const sy = (y: number) => height - pad - ((y - y0) / spanY) * (height - pad * 2);
  const line = series.map((p, i) => `${i === 0 ? "M" : "L"}${sx(p.x).toFixed(1)},${sy(p.y).toFixed(1)}`).join(" ");
  const area = `${line} L${sx(x1).toFixed(1)},${height - pad} L${sx(x0).toFixed(1)},${height - pad} Z`;
  const gid = `g-${color.replace("#", "")}-${height}`;

  return (
    <svg className="block w-full" height={height} viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
      <defs>
        <linearGradient id={gid} x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity={areaOpacity} />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      {fill && <path d={area} fill={`url(#${gid})`} />}
      <path d={line} fill="none" stroke={color} strokeWidth={1.6} vectorEffect="non-scaling-stroke" />
    </svg>
  );
}

// MultiSeries draws several series on one canvas with a shared y-axis.
export function MultiLine({
  series,
  height = 180,
  min = 0,
  max,
}: {
  series: { name: string; color: string; points: Point[] }[];
  height?: number;
  min?: number;
  max?: number;
}) {
  const width = 1000;
  const pad = 6;
  const all = series.flatMap((s) => s.points);
  if (!all.length) {
    return (
      <div className="flex items-center justify-center text-[12px] text-muted" style={{ height }}>
        no samples yet
      </div>
    );
  }
  const x0 = Math.min(...all.map((p) => p.x));
  const x1 = Math.max(...all.map((p) => p.x));
  const y0 = min;
  const y1 = max ?? Math.max(...all.map((p) => p.y), 1);
  const spanX = x1 - x0 || 1;
  const spanY = y1 - y0 || 1;
  const sx = (x: number) => pad + ((x - x0) / spanX) * (width - pad * 2);
  const sy = (y: number) => height - pad - ((y - y0) / spanY) * (height - pad * 2);

  return (
    <div>
      <svg className="block w-full" height={height} viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
        {[0.25, 0.5, 0.75].map((f) => (
          <line
            key={f}
            x1={pad}
            x2={width - pad}
            y1={sy(y0 + spanY * f)}
            y2={sy(y0 + spanY * f)}
            stroke="#233149"
            strokeWidth="1"
            vectorEffect="non-scaling-stroke"
          />
        ))}
        {series.map((s) => (
          <path
            key={s.name}
            d={s.points.map((p, i) => `${i === 0 ? "M" : "L"}${sx(p.x).toFixed(1)},${sy(p.y).toFixed(1)}`).join(" ")}
            fill="none"
            stroke={s.color}
            strokeWidth={1.6}
            vectorEffect="non-scaling-stroke"
          />
        ))}
      </svg>
      <div className="mt-1.5 flex flex-wrap gap-3 px-1 text-[11.5px] text-muted">
        {series.map((s) => (
          <span key={s.name} className="inline-flex items-center gap-1.5">
            <span className="inline-block h-2 w-2 rounded-sm" style={{ background: s.color }} />
            {s.name}
          </span>
        ))}
      </div>
    </div>
  );
}
