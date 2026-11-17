import { useCallback, useEffect, useState } from "react";
import { api } from "../lib/api";
import { Card, Empty, ErrorNote, Modal, Spinner, Table, Tag } from "../components/ui";
import { dt } from "../lib/format";
import type { DiscoveredDevice, Scan } from "../lib/types";
import { Radar, Trash2 } from "lucide-react";

export default function Discovery() {
  const [devices, setDevices] = useState<DiscoveredDevice[]>([]);
  const [scans, setScans] = useState<Scan[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [scanOpen, setScanOpen] = useState(false);
  const [cidr, setCidr] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const [d, s] = await Promise.all([
        api<{ devices: DiscoveredDevice[] }>("/api/discovery/devices"),
        api<{ scans: Scan[] }>("/api/discovery/scans"),
      ]);
      setDevices(d.devices || []);
      setScans(s.scans || []);
      setErr(null);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
    const t = window.setInterval(() => void load(), 15000);
    return () => window.clearInterval(t);
  }, [load]);

  async function startScan() {
    setBusy(true);
    try {
      await api("/api/discovery/scan", { method: "POST", body: { target: cidr.trim() } });
      setScanOpen(false);
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove(id: number) {
    if (!window.confirm("Remove this device from the list?")) return;
    try {
      await api(`/api/discovery/devices/${id}`, { method: "DELETE" });
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }

  if (loading && !devices.length) return <Spinner label="Loading discovery…" />;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[20px] font-semibold tracking-tight">Network discovery</h1>
          <p className="text-[13px] text-muted">
            Finds devices on a subnet so you can see what is on the network that has no agent installed.
          </p>
        </div>
        <button className="btn btn-primary" onClick={() => setScanOpen(true)}>
          <Radar size={14} /> Scan a subnet
        </button>
      </div>

      {err && <ErrorNote onRetry={() => void load()}>{err}</ErrorNote>}

      <Card title={`Known devices · ${devices.length}`}>
        {devices.length ? (
          <Table head={["IP", "Hostname", "MAC", "Vendor", "Ports", "Agent", "Last seen", ""]}>
            {devices.map((d) => (
              <tr key={d.id}>
                <td className="td font-mono text-[12.5px]">{d.ip}</td>
                <td className="td">{d.hostname || "—"}</td>
                <td className="td font-mono text-[12px] text-muted">{d.mac || "—"}</td>
                <td className="td text-muted">{d.vendor || "—"}</td>
                <td className="td text-muted">{d.openPorts || "—"}</td>
                <td className="td">{d.agentPresent ? <Tag tone="ok">{d.agentHostname || "agent"}</Tag> : <Tag tone="muted">no agent</Tag>}</td>
                <td className="td whitespace-nowrap text-muted">{dt(d.lastSeen)}</td>
                <td className="td text-right">
                  <button className="btn btn-danger" onClick={() => void remove(d.id)}>
                    <Trash2 size={13} />
                  </button>
                </td>
              </tr>
            ))}
          </Table>
        ) : (
          <Empty>No devices discovered yet. Start a scan with your subnet, e.g. 192.168.1.0/24.</Empty>
        )}
      </Card>

      <Card title={`Scans · ${scans.length}`}>
        {scans.length ? (
          <Table head={["Target", "Status", "Found", "Started", "Finished"]}>
            {scans.map((s) => (
              <tr key={s.id}>
                <td className="td font-mono text-[12.5px]">{s.target}</td>
                <td className="td">
                  <Tag tone={s.status === "done" ? "ok" : s.status === "error" ? "bad" : "warn"}>{s.status}</Tag>
                </td>
                <td className="td">{s.hostsFound} of {s.hostsScanned}</td>
                <td className="td whitespace-nowrap text-muted">{dt(s.startedAt)}</td>
                <td className="td whitespace-nowrap text-muted">{dt(s.finishedAt)}</td>
              </tr>
            ))}
          </Table>
        ) : (
          <Empty>No scans yet.</Empty>
        )}
      </Card>

      {scanOpen && (
        <Modal
          title="Scan a subnet"
          onClose={() => setScanOpen(false)}
          footer={
            <>
              <button className="btn" onClick={() => setScanOpen(false)}>
                Cancel
              </button>
              <button className="btn btn-primary" disabled={busy || !cidr.trim()} onClick={() => void startScan()}>
                {busy ? "Starting…" : "Start scan"}
              </button>
            </>
          }
        >
          <label className="mb-1 block text-[12.5px] text-muted">CIDR range</label>
          <input className="input" placeholder="192.168.1.0/24" value={cidr} onChange={(e) => setCidr(e.target.value)} />
          <p className="mt-2 text-[11.5px] leading-relaxed text-muted">
            Only scan networks you are allowed to scan. The scan sends an ICMP echo and a TCP connect to a small set of
            ports; it never authenticates and never reads data from the device.
          </p>
        </Modal>
      )}
    </div>
  );
}
