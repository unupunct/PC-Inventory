import { useState } from "react";
import { download } from "../lib/api";
import { Card, Empty } from "../components/ui";
import { Download } from "lucide-react";

const kinds = [
  { key: "inventory", label: "Inventory", hint: "One row per computer with hardware, OS and licence summary." },
  { key: "network", label: "Network", hint: "Adapters, IP addresses, MAC addresses and gateways." },
  { key: "storage", label: "Storage", hint: "Physical disks, volumes and free space." },
  { key: "distribution", label: "Distribution", hint: "OS, CPU, RAM and media breakdown of the fleet." },
  { key: "changes", label: "Changes", hint: "Every hardware change detected, with its category and severity." },
  { key: "performance", label: "Performance", hint: "CPU, memory, disk and network samples in the chosen range." },
] as const;

export default function Reports() {
  const [range, setRange] = useState("24h");
  const [busy, setBusy] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  async function run(kind: string, format: "csv" | "pdf") {
    setBusy(`${kind}-${format}`);
    setErr(null);
    try {
      const q = new URLSearchParams({ format });
      if (kind === "performance") q.set("range", range);
      await download(`/api/reports/${kind}?${q.toString()}`, `${kind}-${new Date().toISOString().slice(0, 10)}.${format}`);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-[20px] font-semibold tracking-tight">Reports</h1>
        <p className="text-[13px] text-muted">
          Generated on demand from the stored inventory and samples. Nothing leaves the server.
        </p>
      </div>

      {err && <div className="rounded-lg border border-bad/40 bg-bad/10 px-3 py-2 text-[13px] text-bad">{err}</div>}

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {kinds.map((k) => (
          <Card key={k.key} title={k.label}>
            <div className="px-4 py-3">
              <p className="mb-3 text-[12.5px] text-muted">{k.hint}</p>
              {k.key === "performance" && (
                <div className="mb-3 flex flex-wrap gap-1.5">
                  {["1h", "6h", "24h", "7d", "30d"].map((r) => (
                    <button key={r} className={`btn ${range === r ? "btn-primary" : ""}`} onClick={() => setRange(r)}>
                      {r}
                    </button>
                  ))}
                </div>
              )}
              <div className="flex gap-1.5">
                <button className="btn" disabled={busy !== null} onClick={() => void run(k.key, "csv")}>
                  <Download size={13} /> {busy === `${k.key}-csv` ? "Building…" : "CSV"}
                </button>
                <button className="btn" disabled={busy !== null} onClick={() => void run(k.key, "pdf")}>
                  <Download size={13} /> {busy === `${k.key}-pdf` ? "Building…" : "PDF"}
                </button>
              </div>
            </div>
          </Card>
        ))}
      </div>

      <Card title="Scheduled reports">
        <Empty>
          Scheduled email delivery of these reports is configured on the Settings page (SMTP server and recipients).
          When SMTP is not configured, nothing is emailed — reports are only downloaded here.
        </Empty>
      </Card>
    </div>
  );
}
