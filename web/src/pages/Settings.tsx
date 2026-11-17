import { useCallback, useEffect, useState } from "react";
import { api } from "../lib/api";
import { useApp } from "../lib/store";
import { Card, Empty, ErrorNote, KeyValue, Spinner, Table } from "../components/ui";
import { dt } from "../lib/format";
import type { AgentConfig, SettingsResponse } from "../lib/types";
import { Database, KeyRound, Save, Send } from "lucide-react";

const toggles: { key: keyof AgentConfig; label: string; hint?: string }[] = [
  { key: "collectSoftware", label: "Installed software", hint: "Adds the application list to each inventory." },
  { key: "collectMonitors", label: "Monitors" },
  { key: "collectUsb", label: "USB devices" },
  { key: "collectPrinters", label: "Printers" },
  { key: "collectProcesses", label: "Process count" },
  { key: "collectPerCoreCpu", label: "Per-core CPU" },
  { key: "collectTemperatures", label: "Temperatures", hint: "Only reported when the platform exposes a sensor." },
  { key: "collectInstalledApps", label: "Installed applications (package list)" },
  { key: "collectWindowsUpdates", label: "Windows updates" },
  { key: "collectLoggedInUser", label: "Logged-in user" },
];

export default function Settings() {
  const { refresh } = useApp();
  const [data, setData] = useState<SettingsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [settings, setSettings] = useState<Record<string, string>>({});
  const [cfg, setCfg] = useState<AgentConfig | null>(null);
  const [pw, setPw] = useState({ current: "", next: "", confirm: "" });

  const load = useCallback(async () => {
    try {
      const r = await api<SettingsResponse>("/api/settings");
      setData(r);
      setSettings(r.settings || {});
      setCfg(r.agentConfig as unknown as AgentConfig);
      setErr(null);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function save() {
    if (!cfg) return;
    setBusy("save");
    setMsg(null);
    try {
      await api("/api/settings", {
        method: "PUT",
        body: {
          settings,
          agentConfig: {
            heartbeatIntervalSec: Number(cfg.heartbeatIntervalSec),
            metricsIntervalSec: Number(cfg.metricsIntervalSec),
            inventoryIntervalSec: Number(cfg.inventoryIntervalSec),
            collectSoftware: cfg.collectSoftware,
            collectMonitors: cfg.collectMonitors,
            collectUsb: cfg.collectUsb,
            collectPrinters: cfg.collectPrinters,
            collectProcesses: cfg.collectProcesses,
            collectPerCoreCpu: cfg.collectPerCoreCpu,
            collectTemperatures: cfg.collectTemperatures,
            collectInstalledApps: cfg.collectInstalledApps,
            collectWindowsUpdates: cfg.collectWindowsUpdates,
            collectLoggedInUser: cfg.collectLoggedInUser,
          },
        },
      });
      setMsg("Saved. Agents pick the new config up on their next heartbeat.");
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  async function changePassword() {
    if (pw.next !== pw.confirm) {
      setErr("The new passwords do not match.");
      return;
    }
    setBusy("pw");
    setMsg(null);
    try {
      await api("/api/auth/password", {
        method: "POST",
        body: { currentPassword: pw.current, newPassword: pw.next },
      });
      setPw({ current: "", next: "", confirm: "" });
      setMsg("Password changed.");
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  async function backup() {
    setBusy("backup");
    setMsg(null);
    try {
      const r = await api<{ path?: string; bytes?: number }>("/api/backup", { method: "POST" });
      setMsg(`Backup written to ${r.path ?? "the backup folder"}.`);
      await refresh();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  async function testNotify() {
    setBusy("notify");
    setMsg(null);
    try {
      await api("/api/alerts/test-notification", { method: "POST" });
      setMsg("Test notification sent to every configured channel.");
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  if (loading && !data) return <Spinner label="Loading settings…" />;
  if (!data) return <Empty>Could not load settings. {err}</Empty>;

  const info = data.serverInfo as Record<string, unknown>;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[20px] font-semibold tracking-tight">Settings</h1>
          <p className="text-[13px] text-muted">Server, agent and notification configuration.</p>
        </div>
        <button className="btn btn-primary" disabled={busy === "save"} onClick={() => void save()}>
          <Save size={14} /> {busy === "save" ? "Saving…" : "Save changes"}
        </button>
      </div>

      {err && <ErrorNote>{err}</ErrorNote>}
      {msg && <div className="rounded-lg border border-ok/40 bg-ok/10 px-3 py-2 text-[13px] text-ok">{msg}</div>}

      <div className="grid gap-3 lg:grid-cols-2">
        <Card title="Server">
          <div className="grid grid-cols-2 gap-x-4 gap-y-3 px-4 py-3 text-[13px]">
            <label className="col-span-2">
              <span className="mb-1 block text-[12.5px] text-muted">Server name</span>
              <input
                className="input"
                value={settings.server_name ?? ""}
                placeholder="PC Inventory"
                onChange={(e) => setSettings({ ...settings, server_name: e.target.value })}
              />
            </label>
            <label>
              <span className="mb-1 block text-[12.5px] text-muted">Enrollment open</span>
              <select
                className="input"
                value={settings.enrollment_enabled ?? "true"}
                onChange={(e) => setSettings({ ...settings, enrollment_enabled: e.target.value })}
              >
                <option value="true">yes</option>
                <option value="false">no</option>
              </select>
            </label>
            <label>
              <span className="mb-1 block text-[12.5px] text-muted">Auto-approve agents</span>
              <select
                className="input"
                value={settings.auto_approve_agents ?? "false"}
                onChange={(e) => setSettings({ ...settings, auto_approve_agents: e.target.value })}
              >
                <option value="false">no — approve by hand</option>
                <option value="true">yes</option>
              </select>
            </label>
            <label>
              <span className="mb-1 block text-[12.5px] text-muted">Low disk threshold (%)</span>
              <input
                className="input"
                value={settings.low_disk_threshold ?? "10"}
                onChange={(e) => setSettings({ ...settings, low_disk_threshold: e.target.value })}
              />
            </label>
            <label>
              <span className="mb-1 block text-[12.5px] text-muted">Dashboard refresh (s)</span>
              <input
                className="input"
                value={settings.dashboard_refresh_sec ?? "15"}
                onChange={(e) => setSettings({ ...settings, dashboard_refresh_sec: e.target.value })}
              />
            </label>
          </div>
        </Card>

        <Card title="Agent collection">
          {cfg ? (
            <div className="px-4 py-3">
              <div className="mb-3 grid grid-cols-3 gap-2">
                {([
                  ["heartbeatIntervalSec", "Heartbeat (s)"],
                  ["metricsIntervalSec", "Metrics (s)"],
                  ["inventoryIntervalSec", "Inventory (s)"],
                ] as const).map(([k, label]) => (
                  <label key={k}>
                    <span className="mb-1 block text-[12px] text-muted">{label}</span>
                    <input
                      className="input"
                      type="number"
                      value={cfg[k]}
                      onChange={(e) => setCfg({ ...cfg, [k]: Number(e.target.value) })}
                    />
                  </label>
                ))}
              </div>
              <div className="grid gap-1.5 sm:grid-cols-2">
                {toggles.map((t) => (
                  <label
                    key={String(t.key)}
                    className="flex items-start gap-2 rounded-lg px-1.5 py-1 text-[12.5px] hover:bg-white/[0.03]"
                    title={t.hint}
                  >
                    <input
                      type="checkbox"
                      className="mt-0.5 accent-[#4c8dff]"
                      checked={Boolean(cfg[t.key])}
                      onChange={(e) => setCfg({ ...cfg, [t.key]: e.target.checked })}
                    />
                    <span>
                      {t.label}
                      {t.hint && <span className="block text-[11px] text-muted">{t.hint}</span>}
                    </span>
                  </label>
                ))}
              </div>
              <p className="mt-2 text-[11.5px] text-muted">Config version {cfg.configVersion}</p>
            </div>
          ) : (
            <Empty>No agent config.</Empty>
          )}
        </Card>
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        <Card title="Server information">
          <KeyValue
            rows={[
              ["Listen address", `${info.listenHost}:${info.port}`],
              ["Public URL", String(info.publicUrl ?? "")],
              ["TLS", info.tls ? "enabled" : "disabled"],
              ["Data folder", String(info.dataDir ?? "")],
              ["Database", String(info.dbPath ?? "")],
              ["Backups", String(info.backupDir ?? "")],
              ["Allowed CIDRs", String(info.allowedCidrs ?? "any")],
              ["Offline threshold", `${info.offlineThresholdSec}s`],
              ["Retention", `${info.retentionDays} days`],
              ["Raw samples kept", `${info.rawSampleHours}h`],
              ["Email notifications", info.emailConfigured ? "configured" : "not configured"],
              ["Telegram notifications", info.telegramConfigured ? "configured" : "not configured"],
              ["Version", String(info.version ?? "")],
            ]}
          />
        </Card>

        <Card title="Password">
          <div className="px-4 py-3">
            <label className="mb-1 block text-[12.5px] text-muted">Current password</label>
            <input
              className="input mb-2"
              type="password"
              value={pw.current}
              onChange={(e) => setPw({ ...pw, current: e.target.value })}
            />
            <label className="mb-1 block text-[12.5px] text-muted">New password</label>
            <input
              className="input mb-2"
              type="password"
              value={pw.next}
              onChange={(e) => setPw({ ...pw, next: e.target.value })}
            />
            <label className="mb-1 block text-[12.5px] text-muted">Repeat new password</label>
            <input
              className="input mb-3"
              type="password"
              value={pw.confirm}
              onChange={(e) => setPw({ ...pw, confirm: e.target.value })}
            />
            <div className="flex flex-wrap gap-1.5">
              <button
                className="btn"
                disabled={busy === "pw" || !pw.current || pw.next.length < 8}
                onClick={() => void changePassword()}
              >
                <KeyRound size={13} /> Change password
              </button>
              <button className="btn" disabled={busy === "backup"} onClick={() => void backup()}>
                <Database size={13} /> {busy === "backup" ? "Backing up…" : "Back up now"}
              </button>
              <button className="btn" disabled={busy === "notify"} onClick={() => void testNotify()}>
                <Send size={13} /> Test notification
              </button>
            </div>
            <p className="mt-2 text-[11.5px] leading-relaxed text-muted">
              At least 8 characters. The new password takes effect immediately for new sign-ins.
            </p>
          </div>
        </Card>
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        <Card title="Agent auto-update">
          <div className="space-y-3 px-4 py-3">
            <label className="flex items-start gap-2 text-[12.5px]">
              <input
                type="checkbox"
                className="mt-0.5 accent-[#4c8dff]"
                checked={(settings.agent_auto_update ?? "false") === "true"}
                onChange={(e) => setSettings({ ...settings, agent_auto_update: e.target.checked ? "true" : "false" })}
              />
              <span>
                Offer agents a newer build
                <span className="block text-[11px] text-muted">
                  Agents check every 6 hours, verify the SHA256 the server advertises, replace themselves and restart.
                  Publish a build by putting it in <code className="font-mono">updates/&lt;platform&gt;/&lt;arch&gt;/</code> in
                  the server data folder.
                </span>
              </span>
            </label>
            <div className="grid grid-cols-2 gap-2 text-[12.5px]">
              <span className="text-muted">Published releases</span>
              <span>see the server log at start-up</span>
            </div>
          </div>
        </Card>

        <Card title="Scheduled reports">
          <div className="space-y-2 px-4 py-3 text-[12.5px]">
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                className="accent-[#4c8dff]"
                checked={(settings.report_schedule_enabled ?? "false") === "true"}
                onChange={(e) => setSettings({ ...settings, report_schedule_enabled: e.target.checked ? "true" : "false" })}
              />
              Email reports daily
            </label>
            <div className="grid grid-cols-2 gap-2">
              <label>
                <span className="mb-1 block text-muted">Time (HH:MM)</span>
                <input
                  className="input"
                  value={settings.report_schedule ?? "07:00"}
                  onChange={(e) => setSettings({ ...settings, report_schedule: e.target.value })}
                />
              </label>
              <label>
                <span className="mb-1 block text-muted">Format</span>
                <select
                  className="input"
                  value={settings.report_format ?? "pdf"}
                  onChange={(e) => setSettings({ ...settings, report_format: e.target.value })}
                >
                  <option value="pdf">PDF</option>
                  <option value="csv">CSV</option>
                </select>
              </label>
              <label className="col-span-2">
                <span className="mb-1 block text-muted">Reports</span>
                <input
                  className="input"
                  placeholder="inventory,changes"
                  value={settings.report_kinds ?? "inventory"}
                  onChange={(e) => setSettings({ ...settings, report_kinds: e.target.value })}
                />
              </label>
              <label className="col-span-2">
                <span className="mb-1 block text-muted">Recipients (comma separated)</span>
                <input
                  className="input"
                  placeholder="it@example.com"
                  value={settings.report_recipients ?? ""}
                  onChange={(e) => setSettings({ ...settings, report_recipients: e.target.value })}
                />
              </label>
              <label className="col-span-2">
                <span className="mb-1 block text-muted">Prometheus scrape token</span>
                <input
                  className="input"
                  placeholder="set a token to open /metrics"
                  value={settings.metrics_token ?? ""}
                  onChange={(e) => setSettings({ ...settings, metrics_token: e.target.value })}
                />
              </label>
            </div>
            <p className="text-[11px] leading-relaxed text-muted">
              Scrape URL: <code className="font-mono">/metrics?token=&lt;token&gt;</code>. Without a token, only an admin
              session can read it. Email needs SMTP in the server config; without it nothing is sent and the reason is
              written to the server event log.
            </p>
          </div>
        </Card>
      </div>

      <Card title="Alert rules">
        <Table head={["Rule", "Severity", "Threshold", "Enabled"]}>
          {data.alertRules.map((r) => (
            <tr key={r.key}>
              <td className="td">
                {r.name}
                <div className="text-[11.5px] text-muted">{r.key}</div>
              </td>
              <td className="td">{r.severity}</td>
              <td className="td font-mono text-[12px]">{r.threshold}</td>
              <td className="td">{r.enabled ? "yes" : "no"}</td>
            </tr>
          ))}
        </Table>
        <div className="border-t border-line px-4 py-2.5 text-[11.5px] text-muted">
          Rules are edited on the Alerts page. Last loaded {dt(new Date().toISOString())}.
        </div>
      </Card>
    </div>
  );
}
