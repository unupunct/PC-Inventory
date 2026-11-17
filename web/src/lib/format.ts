// Formatting helpers. Times arrive as UTC ISO strings and are shown in the
// browser's local timezone.

export function bytes(v?: number | null, digits = 1): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  if (v === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let i = 0;
  let n = v;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return (n >= 10 || i === 0 ? n.toFixed(0) : n.toFixed(digits)) + " " + units[i];
}

export function rate(v?: number | null): string {
  if (v === null || v === undefined) return "—";
  return bytes(v) + "/s";
}

export function pct(v?: number | null, digits = 1): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  return Number(v).toFixed(digits) + "%";
}

export function num(v?: number | null): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  return Number(v).toLocaleString();
}

export function when(iso?: string | null): string {
  if (!iso) return "never";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1970) return "never";
  const s = (Date.now() - d.getTime()) / 1000;
  if (s < 0) return "just now";
  if (s < 60) return Math.max(0, Math.round(s)) + "s ago";
  if (s < 3600) return Math.round(s / 60) + "m ago";
  if (s < 86400) return Math.round(s / 3600) + "h ago";
  if (s < 86400 * 7) return Math.round(s / 86400) + "d ago";
  return d.toLocaleDateString();
}

export function dt(iso?: string | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1970) return "—";
  return d.toLocaleString();
}

export function dateOnly(iso?: string | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1970) return "—";
  return d.toLocaleDateString();
}

export function uptime(seconds?: number | null): string {
  if (!seconds || seconds < 0) return "—";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

export function platformLabel(p?: string): string {
  switch (p) {
    case "windows":
      return "Windows";
    case "darwin":
      return "macOS";
    case "linux":
      return "Linux";
    default:
      return p || "unknown";
  }
}

export function healthTone(freePct: number): "ok" | "warn" | "bad" {
  if (freePct < 5) return "bad";
  if (freePct < 15) return "warn";
  return "ok";
}

export function usageTone(p: number): "ok" | "warn" | "bad" {
  if (p >= 90) return "bad";
  if (p >= 75) return "warn";
  return "ok";
}
