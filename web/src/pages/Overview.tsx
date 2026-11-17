import { Link } from "react-router-dom";
import { useApp } from "../lib/store";
import { Card, DistributionBars, Empty, Spinner, Stat, Tag, Table } from "../components/ui";
import { bytes, num, pct, platformLabel, when } from "../lib/format";
import { AlertTriangle, ArrowRight, Cpu, HardDrive, MonitorSmartphone, Wifi } from "lucide-react";

export default function Overview() {
  const { dashboard: d, agents, loading, refresh } = useApp();

  if (loading && !d) return <Spinner label="Loading fleet…" />;
  if (!d) return <Empty>No data available.</Empty>;

  const st = d.stats;
  const approved = agents.filter((a) => a.status === "approved");
  const attention = [
    ...(d.highCpu ?? []).map((x) => ({ ...x, kind: "High CPU", value: pct(x.value), tone: "warn" as const })),
    ...(d.highRam ?? []).map((x) => ({ ...x, kind: "High RAM", value: pct(x.value), tone: "warn" as const })),
    ...(d.lowDisk ?? []).map((x) => ({ ...x, kind: "Low disk", value: pct(x.value) + " used", tone: "bad" as const })),
    ...(d.commErrors ?? []).map((x) => ({ ...x, kind: "Agent error", value: x.message ?? "", tone: "bad" as const })),
    ...(d.recentlyDisconnected ?? []).map((x) => ({ ...x, kind: "Offline", value: when(x.at), tone: "muted" as const })),
  ];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[20px] font-semibold tracking-tight">Fleet overview</h1>
          <p className="text-[13px] text-muted">
            {st.totalComputers} computer{st.totalComputers === 1 ? "" : "s"} · {st.online} online · {st.offline} offline
            {st.pending > 0 && ` · ${st.pending} awaiting approval`}
          </p>
        </div>
        <div className="flex gap-2">
          <button className="btn" onClick={() => void refresh()}>
            Refresh
          </button>
          <Link className="btn btn-primary" to="/computers">
            All computers <ArrowRight size={14} />
          </Link>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-6">
        <Stat
          label="Computers"
          value={st.totalComputers}
          sub={`${st.online} online · ${st.offline} offline`}
          tone={st.offline > 0 ? "warn" : "ok"}
        />
        <Stat label="Fleet CPU" value={pct(d.fleet.avgCpuPercent)} sub="average of reporting computers" />
        <Stat label="Fleet RAM" value={pct(d.fleet.avgRamPercent)} sub="average of reporting computers" />
        <Stat label="Reporting" value={d.fleet.reportingAgents} sub="sent metrics recently" />
        <Stat
          label="Active alerts"
          value={d.alertCounts?.active ?? d.alertCounts?.open ?? (d.activeAlerts ?? []).length}
          sub={`${d.alertCounts?.critical ?? 0} critical`}
          tone={(d.alertCounts?.active ?? 0) > 0 ? "bad" : "ok"}
        />
        <Stat
          label="Needs attention"
          value={st.needsAttention}
          sub="offline, errors, high load or low disk"
          tone={st.needsAttention > 0 ? "warn" : "ok"}
        />
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        <Card
          title={
            <span className="inline-flex items-center gap-2">
              <AlertTriangle size={13} /> Needs attention
            </span>
          }
          actions={<Link className="text-[12px] link" to="/computers">open computers</Link>}
        >
          {attention.length ? (
            <Table head={["Computer", "Issue", "Detail"]}>
              {attention.slice(0, 8).map((a, i) => (
                <tr key={i}>
                  <td className="td">
                    <Link className="link" to={`/computers/${a.agentId}`}>
                      {a.hostname}
                    </Link>
                    <div className="text-[11.5px] text-muted">{platformLabel(a.platform)}</div>
                  </td>
                  <td className="td">
                    <Tag tone={a.tone}>{a.kind}</Tag>
                  </td>
                  <td className="td text-muted">{a.value || "—"}</td>
                </tr>
              ))}
            </Table>
          ) : (
            <Empty>Nothing needs attention right now.</Empty>
          )}
        </Card>

        <Card title="Hardware changes" actions={<span className="text-[12px] text-muted">recent detections</span>}>
          {(d.latestChanges ?? []).length ? (
            <Table head={["When", "Computer", "Change"]}>
              {(d.latestChanges ?? []).slice(0, 8).map((c) => (
                <tr key={c.id}>
                  <td className="td whitespace-nowrap text-muted">{when(c.detectedAt)}</td>
                  <td className="td">{c.hostname}</td>
                  <td className="td">{c.summary}</td>
                </tr>
              ))}
            </Table>
          ) : (
            <Empty>No hardware changes recorded yet.</Empty>
          )}
        </Card>
      </div>

      <div className="grid gap-3 lg:grid-cols-3">
        <Card title={<span className="inline-flex items-center gap-2"><MonitorSmartphone size={13} /> Operating systems</span>}>
          <DistributionBars data={d.osDistribution ?? []} />
        </Card>
        <Card title={<span className="inline-flex items-center gap-2"><Cpu size={13} /> CPU vendors</span>}>
          <DistributionBars data={d.cpuManufacturers ?? []} />
        </Card>
        <Card title={<span className="inline-flex items-center gap-2"><HardDrive size={13} /> Storage</span>}>
          <DistributionBars data={d.storageDistribution ?? []} />
        </Card>
      </div>

      <div className="grid gap-3 lg:grid-cols-3">
        <Card title="Memory installed">
          <DistributionBars data={d.ramDistribution ?? []} />
        </Card>
        <Card title="Disk media">
          <DistributionBars data={d.mediaDistribution ?? []} />
        </Card>
        <Card title="OS age">
          <DistributionBars data={d.ageDistribution ?? []} label="derived from the OS install date" />
        </Card>
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        <Card title="Fleet totals">
          <div className="grid grid-cols-2 gap-x-4 gap-y-3 px-4 py-4 text-[13px]">
            <div>
              <div className="text-[11px] uppercase tracking-wider text-muted">RAM</div>
              <div className="text-[15px] font-semibold">{bytes(st.totalRamBytes)}</div>
            </div>
            <div>
              <div className="text-[11px] uppercase tracking-wider text-muted">Cores</div>
              <div className="text-[15px] font-semibold">{num(st.totalCores)}</div>
            </div>
            <div>
              <div className="text-[11px] uppercase tracking-wider text-muted">Storage</div>
              <div className="text-[15px] font-semibold">{bytes(st.totalStorageBytes)}</div>
            </div>
            <div>
              <div className="text-[11px] uppercase tracking-wider text-muted">Free storage</div>
              <div className="text-[15px] font-semibold">{bytes(st.freeStorageBytes)}</div>
            </div>
            <div>
              <div className="text-[11px] uppercase tracking-wider text-muted">Desktops / laptops</div>
              <div className="text-[15px] font-semibold">
                {st.desktops} / {st.laptops}
              </div>
            </div>
            <div>
              <div className="text-[11px] uppercase tracking-wider text-muted">Windows / macOS / Linux</div>
              <div className="text-[15px] font-semibold">
                {st.windows} / {st.macos} / {st.linux}
              </div>
            </div>
          </div>
        </Card>

        <Card
          title={
            <span className="inline-flex items-center gap-2">
              <Wifi size={13} /> Activity
            </span>
          }
        >
          {(d.recentEvents ?? []).length ? (
            <Table head={["When", "Computer", "Event"]}>
              {(d.recentEvents ?? []).slice(0, 10).map((e) => (
                <tr key={e.id}>
                  <td className="td whitespace-nowrap text-muted">{when(e.at)}</td>
                  <td className="td">{e.hostname}</td>
                  <td className="td">{e.message}</td>
                </tr>
              ))}
            </Table>
          ) : (
            <Empty>No events.</Empty>
          )}
        </Card>
      </div>

      {approved.length > 0 && (
        <Card title="Latest heartbeats">
          <div className="flex flex-wrap gap-2 px-4 py-3">
            {approved
              .slice()
              .sort((a, b) => (b.lastHeartbeatAt || "").localeCompare(a.lastHeartbeatAt || ""))
              .slice(0, 12)
              .map((a) => (
                <Link
                  key={a.id}
                  to={`/computers/${a.id}`}
                  className="inline-flex items-center gap-2 rounded-lg border border-line bg-panel2 px-2.5 py-1.5 text-[12.5px] hover:border-accent/60"
                >
                  <span className={`h-1.5 w-1.5 rounded-full ${a.online ? "bg-ok" : "bg-bad"}`} />
                  {a.displayName || a.hostname}
                  <span className="text-muted">{when(a.lastHeartbeatAt)}</span>
                </Link>
              ))}
          </div>
        </Card>
      )}

      <p className="pb-4 text-[11.5px] text-muted">
        Every value on this page comes from an agent on a real computer. The fleet CPU and RAM figures are the mean
        of the latest sample of each reporting computer; "—" means no sample has arrived yet, never zero.
      </p>
    </div>
  );
}
