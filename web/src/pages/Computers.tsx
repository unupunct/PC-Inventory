import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useApp } from "../lib/store";
import { Empty, Spinner, Table, Tag } from "../components/ui";
import { bytes, platformLabel, when } from "../lib/format";
import type { Agent, InventoryRow } from "../lib/types";
import { api } from "../lib/api";
import { useEffect } from "react";
import { Search } from "lucide-react";

type Filter = "all" | "online" | "offline" | "windows" | "linux" | "darwin" | "pending";

const filters: { key: Filter; label: string }[] = [
  { key: "all", label: "All" },
  { key: "online", label: "Online" },
  { key: "offline", label: "Offline" },
  { key: "windows", label: "Windows" },
  { key: "linux", label: "Linux" },
  { key: "darwin", label: "macOS" },
  { key: "pending", label: "Pending" },
];

function statusTag(a: Agent) {
  if (a.status !== "approved") return <Tag tone="warn">{a.status}</Tag>;
  return a.online ? <Tag tone="ok">online</Tag> : <Tag tone="bad">offline</Tag>;
}

export default function Computers() {
  const { agents, loading } = useApp();
  const [filter, setFilter] = useState<Filter>("all");
  const [q, setQ] = useState("");
  const [inv, setInv] = useState<Record<string, InventoryRow>>({});

  // Inventory rows carry CPU, RAM and storage summaries for the table.
  useEffect(() => {
    let cancelled = false;
    api<{ computers: InventoryRow[] }>("/api/inventory")
      .then((r) => {
        if (cancelled) return;
        const map: Record<string, InventoryRow> = {};
        for (const row of r.computers || []) map[row.agentId] = row;
        setInv(map);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [agents]);

  const rows = useMemo(() => {
    const term = q.trim().toLowerCase();
    return agents
      .filter((a) => {
        if (filter === "online") return a.status === "approved" && a.online;
        if (filter === "offline") return a.status === "approved" && !a.online;
        if (filter === "pending") return a.status === "pending";
        if (filter === "windows" || filter === "linux" || filter === "darwin") return a.platform === filter;
        return true;
      })
      .filter((a) => {
        if (!term) return true;
        return [a.hostname, a.displayName, a.osName, a.primaryIp, a.location, a.notes]
          .filter(Boolean)
          .some((v) => String(v).toLowerCase().includes(term));
      })
      .sort((a, b) => Number(b.online) - Number(a.online) || a.hostname.localeCompare(b.hostname));
  }, [agents, filter, q]);

  if (loading && !agents.length) return <Spinner label="Loading computers…" />;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[20px] font-semibold tracking-tight">Computers</h1>
          <p className="text-[13px] text-muted">
            {agents.length} registered · {agents.filter((a) => a.online).length} online
          </p>
        </div>
        <div className="relative">
          <Search size={14} className="pointer-events-none absolute left-2.5 top-2.5 text-muted" />
          <input
            className="input w-[260px] pl-8"
            placeholder="Search hostname, OS, IP…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
      </div>

      <div className="flex flex-wrap gap-1.5">
        {filters.map((f) => (
          <button
            key={f.key}
            className={`btn ${filter === f.key ? "btn-primary" : ""}`}
            onClick={() => setFilter(f.key)}
          >
            {f.label}
          </button>
        ))}
      </div>

      <section className="card overflow-hidden">
        {rows.length ? (
          <Table head={["Computer", "Status", "OS", "CPU", "RAM", "Storage", "Last heartbeat", ""]}>
            {rows.map((a) => (
              <tr key={a.id} className="hover:bg-white/[0.02]">
                <td className="td">
                  <Link className="link font-medium" to={`/computers/${a.id}`}>
                    {a.displayName || a.hostname}
                  </Link>
                  <div className="text-[11.5px] text-muted">
                    {[a.primaryIp, platformLabel(a.platform), a.location].filter(Boolean).join(" · ")}
                  </div>
                </td>
                <td className="td">{statusTag(a)}</td>
                <td className="td text-[12.5px]">
                  {a.osName || "—"} {a.osVersion || ""}
                </td>
                <td className="td text-[12.5px] text-muted">{inv[a.id]?.cpuBrand || "—"}</td>
                <td className="td text-[12.5px]">{bytes(inv[a.id]?.ramBytes)}</td>
                <td className="td text-[12.5px] text-muted">
                  {inv[a.id]?.storageTotalBytes ? bytes(inv[a.id].storageTotalBytes) : "—"}
                </td>
                <td className="td whitespace-nowrap text-[12.5px] text-muted">{when(a.lastHeartbeatAt)}</td>
                <td className="td text-right">
                  <Link className="btn" to={`/computers/${a.id}`}>
                    Details
                  </Link>
                </td>
              </tr>
            ))}
          </Table>
        ) : (
          <Empty>
            {agents.length
              ? "No computer matches this filter."
              : "No computers yet. Install the agent on a machine and point it at this server."}
          </Empty>
        )}
      </section>
    </div>
  );
}
