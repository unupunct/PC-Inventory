import type { ReactNode } from "react";

// Presentational building blocks shared by every page.

export function Card({ title, actions, children, className = "" }: {
  title?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`card ${className}`}>
      {(title || actions) && (
        <header className="flex items-center gap-3 border-b border-line px-4 py-3">
          <h2 className="text-[12px] uppercase tracking-[0.09em] text-muted font-semibold">{title}</h2>
          <div className="grow" />
          {actions}
        </header>
      )}
      {children}
    </section>
  );
}

export function Stat({
  label,
  value,
  sub,
  tone = "default",
}: {
  label: string;
  value: ReactNode;
  sub?: ReactNode;
  tone?: "default" | "ok" | "warn" | "bad";
}) {
  const toneClass =
    tone === "ok" ? "text-ok" : tone === "warn" ? "text-warn" : tone === "bad" ? "text-bad" : "text-ink";
  return (
    <div className="card px-4 py-3.5">
      <div className="text-[11px] uppercase tracking-[0.09em] text-muted font-semibold">{label}</div>
      <div className={`mt-1.5 text-[26px] leading-none font-semibold ${toneClass}`}>{value}</div>
      {sub && <div className="mt-1.5 text-[12.5px] text-muted">{sub}</div>}
    </div>
  );
}

export function Tag({ tone = "muted", children }: {
  tone?: "ok" | "warn" | "bad" | "muted" | "accent";
  children: ReactNode;
}) {
  const map = {
    ok: "bg-ok/15 text-ok",
    warn: "bg-warn/15 text-warn",
    bad: "bg-bad/15 text-bad",
    accent: "bg-accent/15 text-accent",
    muted: "bg-white/5 text-muted",
  } as const;
  return <span className={`tag ${map[tone]}`}>{children}</span>;
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="px-4 py-6 text-[13px] text-muted">{children}</div>;
}

export function ErrorNote({ children, onRetry }: { children: ReactNode; onRetry?: () => void }) {
  return (
    <div className="mx-4 my-3 flex items-center gap-3 rounded-lg border border-bad/40 bg-bad/10 px-3 py-2 text-[13px] text-bad">
      <span className="grow">{children}</span>
      {onRetry && (
        <button className="btn btn-danger" onClick={onRetry}>
          Retry
        </button>
      )}
    </div>
  );
}

export function KeyValue({ rows }: { rows: [string, ReactNode][] }) {
  const visible = rows.filter(([, v]) => v !== undefined && v !== null && v !== "" && v !== "—");
  if (!visible.length) return <Empty>No data.</Empty>;
  return (
    <dl className="divide-y divide-line/60">
      {visible.map(([k, v]) => (
        <div key={k} className="grid grid-cols-[minmax(120px,34%)_1fr] gap-3 px-4 py-2">
          <dt className="text-[12.5px] text-muted">{k}</dt>
          <dd className="text-[13px] break-words">{v}</dd>
        </div>
      ))}
    </dl>
  );
}

export function Table({ head, children }: { head: ReactNode[]; children: ReactNode }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse">
        <thead>
          <tr>
            {head.map((h, i) => (
              <th key={i} className="th">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  );
}

export function Bar({ value, max, tone = "accent" }: { value: number; max: number; tone?: "accent" | "ok" | "warn" | "bad" }) {
  const p = max > 0 ? Math.min(100, Math.max(0, (value / max) * 100)) : 0;
  const color = { accent: "bg-accent", ok: "bg-ok", warn: "bg-warn", bad: "bg-bad" }[tone];
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-white/5">
      <div className={`h-full rounded-full ${color}`} style={{ width: `${p}%` }} />
    </div>
  );
}

// DistributionBars renders a simple labelled bar list (used for OS, RAM, disks).
export function DistributionBars({ data, total, label }: { data: { name: string; value: number }[]; total?: number; label?: string }) {
  const sum = total ?? data.reduce((t, d) => t + d.value, 0);
  if (!data.length) return <Empty>No data yet.</Empty>;
  const max = Math.max(...data.map((d) => d.value), 1);
  return (
    <div className="space-y-2 px-4 py-3">
      {data.map((d) => (
        <div key={d.name}>
          <div className="mb-1 flex items-baseline justify-between gap-2 text-[12.5px]">
            <span className="truncate">{d.name || "unknown"}</span>
            <span className="shrink-0 text-muted">
              {d.value}
              {sum > 0 && <span className="ml-1.5 text-muted/70">{Math.round((d.value / sum) * 100)}%</span>}
            </span>
          </div>
          <Bar value={d.value} max={max} />
        </div>
      ))}
      {label && <div className="pt-1 text-[11.5px] text-muted">{label}</div>}
    </div>
  );
}

export function Modal({ title, onClose, children, footer }: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="card w-full max-w-lg" onClick={(e) => e.stopPropagation()}>
        <header className="flex items-center border-b border-line px-4 py-3">
          <h3 className="font-semibold">{title}</h3>
          <div className="grow" />
          <button className="btn" onClick={onClose}>
            Close
          </button>
        </header>
        <div className="px-4 py-4">{children}</div>
        {footer && <footer className="flex justify-end gap-2 border-t border-line px-4 py-3">{footer}</footer>}
      </div>
    </div>
  );
}

export function Spinner({ label }: { label?: string }) {
  return (
    <div className="flex items-center gap-2 px-4 py-6 text-[13px] text-muted">
      <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-line border-t-accent" />
      {label ?? "Loading…"}
    </div>
  );
}
