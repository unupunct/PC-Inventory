import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, download } from "../lib/api";
import { useApp } from "../lib/store";
import { Card, Empty, ErrorNote, KeyValue, Modal, Spinner, Stat, Table, Tag } from "../components/ui";
import { MultiLine, Spark, type Point } from "../components/charts";
import { bytes, dateOnly, dt, num, pct, platformLabel, rate, uptime, usageTone, when } from "../lib/format";
import type { Agent, HardwareChange, Inventory, PerfResponse, Snapshot, SoftwareDiff } from "../lib/types";
import {
  ArrowLeft,
  Download,
  Power,
  RefreshCw,
  RotateCcw,
  ShieldOff,
  Trash2,
  UserRound,
} from "lucide-react";

type Tab = "hardware" | "performance" | "changes" | "software" | "history";

const tabs: { key: Tab; label: string }[] = [
  { key: "hardware", label: "Hardware" },
  { key: "performance", label: "Performance" },
  { key: "changes", label: "Changes" },
  { key: "software", label: "Software" },
  { key: "history", label: "Snapshots" },
];

export default function ComputerDetail() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const { refresh } = useApp();
  const [agent, setAgent] = useState<Agent | null>(null);
  const [inv, setInv] = useState<Inventory | null>(null);
  const [invMeta, setInvMeta] = useState<{ collectedAt?: string; receivedAt?: string; hash?: string }>({});
  const [perf, setPerf] = useState<PerfResponse | null>(null);
  const [changes, setChanges] = useState<HardwareChange[]>([]);
  const [snapshots, setSnapshots] = useState<Snapshot[]>([]);
  const [range, setRange] = useState("1h");
  const [tab, setTab] = useState<Tab>("hardware");
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [rename, setRename] = useState(false);
  const [form, setForm] = useState({ displayName: "", location: "", notes: "" });
  const [swQuery, setSwQuery] = useState("");
  const [swDiff, setSwDiff] = useState<SoftwareDiff | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await api<{ agent: Agent }>(`/api/agents/${id}`);
      setAgent(res.agent);
      setForm({
        displayName: res.agent.displayName || "",
        location: res.agent.location || "",
        notes: res.agent.notes || "",
      });
      setErr(null);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
    api<{ inventory: Inventory; collectedAt?: string; receivedAt?: string; hash?: string }>(`/api/agents/${id}/inventory`)
      .then((r) => {
        setInv(r.inventory);
        setInvMeta({ collectedAt: r.collectedAt, receivedAt: r.receivedAt, hash: r.hash });
      })
      .catch(() => setInv(null));
    api<PerfResponse>(`/api/agents/${id}/performance`, { query: { range } })
      .then(setPerf)
      .catch(() => setPerf(null));
    api<{ changes: HardwareChange[] }>("/api/changes", { query: { agentId: id, limit: 200 } })
      .then((r) => setChanges(r.changes || []))
      .catch(() => setChanges([]));
    api<SoftwareDiff>(`/api/agents/${id}/software-diff`)
      .then(setSwDiff)
      .catch(() => setSwDiff(null));
    api<{ snapshots: Snapshot[] }>(`/api/agents/${id}/snapshots`)
      .then((r) => setSnapshots(r.snapshots || []))
      .catch(() => setSnapshots([]));
  }, [id, range]);

  useEffect(() => {
    setLoading(true);
    void load();
  }, [load]);

  async function action(name: string, fn: () => Promise<unknown>, confirmText?: string) {
    if (confirmText && !window.confirm(confirmText)) return;
    setBusy(name);
    try {
      await fn();
      await load();
      await refresh();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  const samples = perf?.samples ?? [];
  const cpuSeries = useMemo<Point[]>(
    () => samples.map((s) => ({ x: new Date(s.at).getTime(), y: s.cpuPercent })),
    [samples]
  );
  const memSeries = useMemo<Point[]>(
    () => samples.map((s) => ({ x: new Date(s.at).getTime(), y: s.memPercent })),
    [samples]
  );
  const diskSeries = useMemo(
    () => samples.map((s) => ({ x: new Date(s.at).getTime(), y: (s.diskReadBps ?? 0) + (s.diskWriteBps ?? 0) })),
    [samples]
  );
  const netSeries = useMemo(
    () => samples.map((s) => ({ x: new Date(s.at).getTime(), y: (s.netRxBps ?? 0) + (s.netTxBps ?? 0) })),
    [samples]
  );
  const latest = samples.at(-1);

  const software = useMemo(() => {
    const list = inv?.software ?? [];
    const term = swQuery.trim().toLowerCase();
    return term
      ? list.filter((s) => [s.name, s.publisher, s.version].filter(Boolean).some((v) => String(v).toLowerCase().includes(term)))
      : list;
  }, [inv, swQuery]);

  if (loading && !agent) return <Spinner label="Loading computer…" />;
  if (!agent) return <Empty>Computer not found. {err && <span className="text-bad">{err}</span>}</Empty>;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <Link to="/computers" className="mb-1 inline-flex items-center gap-1 text-[12.5px] text-muted hover:text-ink">
            <ArrowLeft size={13} /> Computers
          </Link>
          <h1 className="truncate text-[20px] font-semibold tracking-tight">{agent.displayName || agent.hostname}</h1>
          <p className="text-[13px] text-muted">
            {platformLabel(agent.platform)} {agent.osVersion || ""} · {agent.primaryIp || "no IP"} · agent{" "}
            {agent.agentVersion || "?"}
          </p>
        </div>
        <div className="flex flex-wrap gap-1.5">
          <button
            className="btn"
            disabled={busy === "refresh"}
            onClick={() => void action("refresh", () => api(`/api/agents/${id}/refresh-inventory`, { method: "POST" }))}
            title="Ask the agent to send a fresh inventory on its next heartbeat"
          >
            <RefreshCw size={14} /> {busy === "refresh" ? "Requested…" : "Refresh inventory"}
          </button>
          <button className="btn" onClick={() => setRename(true)}>
            <UserRound size={14} /> Rename
          </button>
          <button
            className="btn"
            disabled={busy === "rotate"}
            onClick={() =>
              void action(
                "rotate",
                () => api(`/api/agents/${id}/rotate-token`, { method: "POST" }),
                "Rotate this agent's token? The old token keeps working for 10 minutes."
              )
            }
          >
            <RotateCcw size={14} /> Rotate token
          </button>
          <button
            className="btn"
            disabled={busy === "toggle"}
            onClick={() =>
              void action("toggle", () =>
                api(`/api/agents/${id}/${agent.status === "disabled" ? "enable" : "disable"}`, { method: "POST" })
              )
            }
          >
            <Power size={14} /> {agent.status === "disabled" ? "Enable" : "Disable"}
          </button>
          <button
            className="btn btn-danger"
            disabled={busy === "revoke"}
            onClick={() =>
              void action("revoke", () => api(`/api/agents/${id}/revoke`, { method: "POST" }), "Revoke this agent's token? It must be approved again.")
            }
          >
            <ShieldOff size={14} /> Revoke
          </button>
          <button
            className="btn btn-danger"
            disabled={busy === "delete"}
            onClick={() =>
              void action(
                "delete",
                async () => {
                  await api(`/api/agents/${id}`, { method: "DELETE" });
                  navigate("/computers");
                },
                "Delete this computer and all of its history? This cannot be undone."
              )
            }
          >
            <Trash2 size={14} /> Delete
          </button>
        </div>
      </div>

      {err && <ErrorNote onRetry={() => void load()}>{err}</ErrorNote>}

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Stat
          label="Status"
          value={agent.status === "approved" ? (agent.online ? "Online" : "Offline") : agent.status}
          sub={`last heartbeat ${when(agent.lastHeartbeatAt)}`}
          tone={agent.status !== "approved" ? "warn" : agent.online ? "ok" : "bad"}
        />
        <Stat label="CPU now" value={pct(latest?.cpuPercent)} sub={inv?.cpu.brand} />
        <Stat
          label="Memory now"
          value={pct(latest?.memPercent)}
          sub={latest ? `${bytes(latest.memUsedBytes)} / ${bytes(latest.memTotalBytes)}` : "no sample yet"}
        />
        <Stat
          label="Uptime"
          value={uptime(inv?.os.uptimeSeconds)}
          sub={inv?.os.bootTime ? `booted ${dt(inv.os.bootTime)}` : "reported by the agent"}
        />
      </div>

      {agent.lastError && (
        <div className="rounded-lg border border-warn/40 bg-warn/10 px-3 py-2 text-[13px] text-warn">
          Last agent error: {agent.lastError}
          {agent.pendingBuffered ? ` · ${agent.pendingBuffered} report(s) buffered locally` : ""}
        </div>
      )}

      <div className="flex flex-wrap gap-1.5">
        {tabs.map((t) => (
          <button key={t.key} className={`btn ${tab === t.key ? "btn-primary" : ""}`} onClick={() => setTab(t.key)}>
            {t.label}
          </button>
        ))}
        <div className="grow" />
        <button
          className="btn"
          onClick={() => void download(`/api/reports/inventory?format=csv&agents=${id}`, "inventory.csv")}
        >
          <Download size={14} /> CSV
        </button>
        <button
          className="btn"
          onClick={() => void download(`/api/reports/inventory?format=pdf&agents=${id}`, "inventory.pdf")}
        >
          <Download size={14} /> PDF
        </button>
      </div>

      {tab === "hardware" && (
        <div className="grid gap-3 lg:grid-cols-2">
          {!inv ? (
            <Card title="Inventory">
              <Empty>No inventory received from this computer yet.</Empty>
            </Card>
          ) : (
            <>
              <Card title="System">
                <KeyValue
                  rows={[
                    ["Hostname", inv.host.hostname],
                    ["Manufacturer", inv.host.manufacturer],
                    ["Model", inv.host.model],
                    ["Serial", inv.host.serial],
                    ["Chassis", inv.host.chassisType],
                    ["Virtual machine", inv.host.isVirtual ? "yes" : undefined],
                    ["Domain", inv.host.domain],
                  ]}
                />
              </Card>
              <Card title="Operating system">
                <KeyValue
                  rows={[
                    ["Name", [inv.os.name, inv.os.edition].filter(Boolean).join(" ")],
                    ["Version", inv.os.version],
                    ["Build", inv.os.build],
                    ["Kernel", inv.os.kernel],
                    ["Architecture", inv.os.arch],
                    ["Installed", dateOnly(inv.os.installDate)],
                    ["Timezone", inv.os.timezone],
                    ["Activation", inv.os.activationState],
                    ["Defender", inv.os.defenderEnabled === undefined ? undefined : inv.os.defenderEnabled ? "enabled" : "disabled"],
                    ["Package manager", inv.os.packageManager],
                  ]}
                />
              </Card>
              <Card title="CPU">
                <KeyValue
                  rows={[
                    ["Model", inv.cpu.brand],
                    ["Manufacturer", inv.cpu.manufacturer],
                    ["Cores / threads", `${inv.cpu.physicalCores} / ${inv.cpu.logicalCores}`],
                    ["Architecture", inv.cpu.architecture],
                    ["Socket", inv.cpu.socket],
                    ["Base clock", inv.cpu.baseFrequencyMHz ? `${num(inv.cpu.baseFrequencyMHz)} MHz` : undefined],
                    ["Cache", inv.cpu.cacheKB ? `${num(inv.cpu.cacheKB)} KB` : undefined],
                  ]}
                />
              </Card>
              <Card title={`Memory · ${bytes(inv.memory.totalBytes)}`}>
                {inv.memory.modulesSupported && inv.memory.modules?.length ? (
                  <Table head={["Slot", "Size", "Speed", "Type", "Part"]}>
                    {inv.memory.modules.map((m, i) => (
                      <tr key={i}>
                        <td className="td">{m.slot || "—"}</td>
                        <td className="td">{bytes(m.sizeBytes)}</td>
                        <td className="td">{m.speedMHz ? `${m.speedMHz} MHz` : "—"}</td>
                        <td className="td">{m.type || "—"}</td>
                        <td className="td text-muted">{m.partNumber || m.manufacturer || "—"}</td>
                      </tr>
                    ))}
                  </Table>
                ) : (
                  <Empty>
                    {bytes(inv.memory.totalBytes)} total. This platform does not expose individual modules
                    {inv.memory.modulesSupported ? "." : " (unified memory or no DMI access)."}
                  </Empty>
                )}
              </Card>
              <Card title="Motherboard and BIOS">
                <KeyValue
                  rows={[
                    ["Board", [inv.motherboard?.manufacturer, inv.motherboard?.product].filter(Boolean).join(" ")],
                    ["Board serial", inv.motherboard?.serial],
                    ["BIOS vendor", inv.bios?.vendor],
                    ["BIOS version", inv.bios?.version],
                    ["BIOS date", inv.bios?.releaseDate],
                  ]}
                />
              </Card>
              <Card title="Graphics">
                {inv.gpus?.length ? (
                  <Table head={["GPU", "VRAM", "Driver", "Resolution"]}>
                    {inv.gpus.map((g, i) => (
                      <tr key={i}>
                        <td className="td">{g.name}</td>
                        <td className="td">{g.vramBytes ? bytes(g.vramBytes) : "—"}</td>
                        <td className="td text-muted">{g.driverVersion || "—"}</td>
                        <td className="td text-muted">{g.resolution || "—"}</td>
                      </tr>
                    ))}
                  </Table>
                ) : (
                  <Empty>No GPU reported.</Empty>
                )}
              </Card>
              <Card title={`Physical disks · ${inv.physicalDisks?.length ?? 0}`}>
                {inv.physicalDisks?.length ? (
                  <Table head={["Model", "Size", "Media", "Health", "Hours"]}>
                    {inv.physicalDisks.map((d, i) => (
                      <tr key={i}>
                        <td className="td">
                          {d.model}
                          <div className="text-[11.5px] text-muted">{d.serial || d.busType || ""}</div>
                        </td>
                        <td className="td">{bytes(d.sizeBytes)}</td>
                        <td className="td">{d.mediaType || "—"}</td>
                        <td className="td">
                          <Tag tone={d.health === "ok" ? "ok" : d.health === "unknown" || !d.health ? "muted" : "warn"}>
                            {d.health || "unknown"}
                          </Tag>
                          {d.temperatureC ? <span className="ml-1.5 text-[11.5px] text-muted">{d.temperatureC}°C</span> : null}
                        </td>
                        <td className="td text-muted">{d.powerOnHours ? num(d.powerOnHours) : "—"}</td>
                      </tr>
                    ))}
                  </Table>
                ) : (
                  <Empty>No physical disks reported.</Empty>
                )}
              </Card>
              <Card title="Volumes">
                {inv.volumes?.length ? (
                  <Table head={["Mount", "Filesystem", "Size", "Free", "Used"]}>
                    {inv.volumes.map((v, i) => {
                      const used = v.totalBytes - v.freeBytes;
                      const p = v.totalBytes ? (used / v.totalBytes) * 100 : 0;
                      return (
                        <tr key={i}>
                          <td className="td">
                            {v.mountPoint} {v.label ? <span className="text-muted">({v.label})</span> : null}
                          </td>
                          <td className="td text-muted">{v.fileSystem || "—"}</td>
                          <td className="td">{bytes(v.totalBytes)}</td>
                          <td className="td">{bytes(v.freeBytes)}</td>
                          <td className="td">
                            <Tag tone={usageTone(p) === "bad" ? "bad" : usageTone(p) === "warn" ? "warn" : "muted"}>
                              {p.toFixed(0)}%
                            </Tag>
                          </td>
                        </tr>
                      );
                    })}
                  </Table>
                ) : (
                  <Empty>No volumes reported.</Empty>
                )}
              </Card>
              <Card title="Network adapters">
                {inv.networkAdapters?.length ? (
                  <Table head={["Adapter", "MAC", "IPv4", "Speed"]}>
                    {inv.networkAdapters.map((n, i) => (
                      <tr key={i}>
                        <td className="td">
                          {n.name} {n.isWireless ? <Tag tone="accent">Wi-Fi</Tag> : null}
                        </td>
                        <td className="td font-mono text-[12px] text-muted">{n.mac || "—"}</td>
                        <td className="td text-[12.5px]">{(n.ipv4 || []).join(", ") || "—"}</td>
                        <td className="td text-muted">{n.speedMbps ? `${num(n.speedMbps)} Mbps` : "—"}</td>
                      </tr>
                    ))}
                  </Table>
                ) : (
                  <Empty>No adapters reported.</Empty>
                )}
              </Card>
              <Card title="Peripherals">
                <KeyValue
                  rows={[
                    ["Monitors", inv.monitors?.map((m) => `${m.manufacturer || ""} ${m.model || ""}`.trim() + (m.width ? ` ${m.width}×${m.height}` : "")).join("; ")],
                    ["Batteries", inv.batteries?.map((b) => `${b.name} ${b.chargePercent ?? "?"}%`).join("; ")],
                    ["Audio", inv.audioDevices?.map((a) => a.name).join("; ")],
                    ["USB devices", inv.usbDevices?.length ? `${inv.usbDevices.length} connected` : undefined],
                    ["Bluetooth", inv.bluetoothAdapters?.map((b) => b.name).join("; ")],
                    ["Optical", inv.opticalDrives?.map((o) => o.model || o.name).join("; ")],
                    ["Printers", inv.printers?.map((p) => p.name).join("; ")],
                  ]}
                />
              </Card>
              <Card title="Collection notes">
                {inv.collectionErrors?.length ? (
                  <ul className="space-y-1.5 px-4 py-3 text-[12.5px] text-warn">
                    {inv.collectionErrors.map((e, i) => (
                      <li key={i}>
                        <span className="font-medium">{e.category}:</span> {e.error}
                      </li>
                    ))}
                  </ul>
                ) : (
                  <Empty>Every collector succeeded on this platform.</Empty>
                )}
                <div className="border-t border-line px-4 py-2.5 text-[11.5px] text-muted">
                  Collected {dt(invMeta.collectedAt || inv.collectedAt)} · received {dt(invMeta.receivedAt)} · fingerprint{" "}
                  <code className="font-mono">{(invMeta.hash || inv.hash || "").slice(0, 12)}</code>
                </div>
              </Card>
            </>
          )}
        </div>
      )}

      {tab === "performance" && (
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-1.5">
            {["1h", "6h", "24h", "7d", "30d"].map((r) => (
              <button key={r} className={`btn ${range === r ? "btn-primary" : ""}`} onClick={() => setRange(r)}>
                {r}
              </button>
            ))}
            <div className="grow" />
            <span className="text-[12px] text-muted">
              {samples.length} samples · {perf?.bucket || "raw"} resolution
            </span>
          </div>
          <div className="grid gap-3 lg:grid-cols-2">
            <Card title="CPU usage">
              <div className="px-4 py-3">
                <Spark series={cpuSeries} min={0} max={100} height={120} color="#4c8dff" />
                <div className="mt-2 flex justify-between text-[11.5px] text-muted">
                  <span>oldest {when(samples[0]?.at)}</span>
                  <span>newest {when(latest?.at)}</span>
                </div>
              </div>
            </Card>
            <Card title="Memory usage">
              <div className="px-4 py-3">
                <Spark series={memSeries} min={0} max={100} height={120} color="#2fbf71" />
                <div className="mt-2 flex justify-between text-[11.5px] text-muted">
                  <span>{bytes(latest?.memUsedBytes)} used</span>
                  <span>{bytes(latest?.memTotalBytes)} total</span>
                </div>
              </div>
            </Card>
            <Card title="Disk throughput">
              <div className="px-4 py-3">
                <Spark series={diskSeries} min={0} height={120} color="#f2b23c" />
                <div className="mt-2 text-[11.5px] text-muted">read + write, latest {rate((latest?.diskReadBps ?? 0) + (latest?.diskWriteBps ?? 0))}</div>
              </div>
            </Card>
            <Card title="Network throughput">
              <div className="px-4 py-3">
                <Spark series={netSeries} min={0} height={120} color="#a78bfa" />
                <div className="mt-2 text-[11.5px] text-muted">rx + tx, latest {rate((latest?.netRxBps ?? 0) + (latest?.netTxBps ?? 0))}</div>
              </div>
            </Card>
          </div>
          <Card title="CPU and memory together">
            <div className="px-4 py-3">
              <MultiLine
                min={0}
                max={100}
                series={[
                  { name: "CPU %", color: "#4c8dff", points: cpuSeries },
                  { name: "Memory %", color: "#2fbf71", points: memSeries },
                ]}
              />
            </div>
          </Card>
          {samples.length === 0 && (
            <Card>
              <Empty>No performance samples in this range. Samples arrive every metrics interval (default 30 s).</Empty>
            </Card>
          )}
        </div>
      )}

      {tab === "changes" && (
        <Card title={`Hardware changes · ${changes.length}`}>
          {changes.length ? (
            <Table head={["When", "Category", "Severity", "Change"]}>
              {changes.map((c) => (
                <tr key={c.id}>
                  <td className="td whitespace-nowrap text-muted">{dt(c.detectedAt)}</td>
                  <td className="td">
                    <Tag tone="accent">{c.category}</Tag>
                  </td>
                  <td className="td">
                    <Tag tone={c.severity === "critical" ? "bad" : c.severity === "warning" ? "warn" : "muted"}>
                      {c.severity}
                    </Tag>
                  </td>
                  <td className="td">{c.summary}</td>
                </tr>
              ))}
            </Table>
          ) : (
            <Empty>No hardware changes detected. A change is recorded when a new inventory fingerprint differs.</Empty>
          )}
        </Card>
      )}

      {tab === "software" && (
        <div className="space-y-3">
        <Card title="What changed since the last inventory">
          {!swDiff || !swDiff.available ? (
            <Empty>{swDiff?.message || "No comparison available yet: two inventory snapshots are needed."}</Empty>
          ) : (
            <>
              <div className="flex flex-wrap gap-4 px-4 py-3 text-[13px]">
                <span><span className="text-muted">Added</span> <b className="text-ok">{swDiff.added}</b></span>
                <span><span className="text-muted">Removed</span> <b className="text-bad">{swDiff.removed}</b></span>
                <span><span className="text-muted">Upgraded</span> <b className="text-accent">{swDiff.upgraded}</b></span>
                <span><span className="text-muted">Downgraded</span> <b className="text-warn">{swDiff.downgraded}</b></span>
                <span className="text-muted">
                  {dt(swDiff.fromCollectedAt)} → {dt(swDiff.toCollectedAt)}
                </span>
              </div>
              {swDiff.changes?.length ? (
                <Table head={["Change", "Application", "From", "To"]}>
                  {swDiff.changes.slice(0, 300).map((c, i) => (
                    <tr key={i}>
                      <td className="td">
                        <Tag tone={c.change === "added" ? "ok" : c.change === "removed" ? "bad" : c.change === "upgraded" ? "accent" : "warn"}>
                          {c.change}
                        </Tag>
                      </td>
                      <td className="td">
                        {c.name}
                        {c.publisher ? <div className="text-[11.5px] text-muted">{c.publisher}</div> : null}
                      </td>
                      <td className="td text-muted">{c.fromVersion || "—"}</td>
                      <td className="td">{c.toVersion || "—"}</td>
                    </tr>
                  ))}
                </Table>
              ) : (
                <Empty>No software changed between these two inventories.</Empty>
              )}
            </>
          )}
        </Card>
        <Card
          title={`Installed software · ${inv?.software?.length ?? 0}`}
          actions={
            <input
              className="input w-[240px] py-1.5"
              placeholder="Filter…"
              value={swQuery}
              onChange={(e) => setSwQuery(e.target.value)}
            />
          }
        >
          {software.length ? (
            <Table head={["Application", "Version", "Publisher", "Installed"]}>
              {software.slice(0, 500).map((s, i) => (
                <tr key={i}>
                  <td className="td">{s.name}</td>
                  <td className="td text-muted">{s.version || "—"}</td>
                  <td className="td text-muted">{s.publisher || "—"}</td>
                  <td className="td text-muted">{dateOnly(s.installDate)}</td>
                </tr>
              ))}
            </Table>
          ) : (
            <Empty>No software list received (or software collection is disabled for this agent).</Empty>
          )}
          {software.length > 500 && (
            <div className="px-4 py-2 text-[11.5px] text-muted">Showing the first 500 of {software.length} entries.</div>
          )}
        </Card>
        </div>
      )}

      {tab === "history" && (
        <Card title={`Inventory snapshots · ${snapshots.length}`}>
          {snapshots.length ? (
            <Table head={["Collected", "Received", "Reason", "Fingerprint"]}>
              {snapshots.map((s) => (
                <tr key={s.id}>
                  <td className="td whitespace-nowrap">{dt(s.collectedAt)}</td>
                  <td className="td whitespace-nowrap text-muted">{dt(s.receivedAt)}</td>
                  <td className="td">{s.reason || "scheduled"}</td>
                  <td className="td font-mono text-[12px] text-muted">{s.hash.slice(0, 16)}</td>
                </tr>
              ))}
            </Table>
          ) : (
            <Empty>No snapshots yet.</Empty>
          )}
        </Card>
      )}

      {rename && (
        <Modal
          title="Rename computer"
          onClose={() => setRename(false)}
          footer={
            <>
              <button className="btn" onClick={() => setRename(false)}>
                Cancel
              </button>
              <button
                className="btn btn-primary"
                onClick={async () => {
                  await action("rename", () =>
                    api(`/api/agents/${id}`, {
                      method: "PATCH",
                      body: { displayName: form.displayName, location: form.location, notes: form.notes },
                    })
                  );
                  setRename(false);
                }}
              >
                Save
              </button>
            </>
          }
        >
          <label className="mb-1 block text-[12.5px] text-muted">Display name</label>
          <input
            className="input mb-3"
            value={form.displayName}
            placeholder={agent.hostname}
            onChange={(e) => setForm({ ...form, displayName: e.target.value })}
          />
          <label className="mb-1 block text-[12.5px] text-muted">Location</label>
          <input
            className="input mb-3"
            value={form.location}
            placeholder="e.g. Office, Living room"
            onChange={(e) => setForm({ ...form, location: e.target.value })}
          />
          <label className="mb-1 block text-[12.5px] text-muted">Notes</label>
          <textarea
            className="input h-24 resize-y"
            value={form.notes}
            onChange={(e) => setForm({ ...form, notes: e.target.value })}
          />
        </Modal>
      )}
    </div>
  );
}
