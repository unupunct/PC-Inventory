import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../lib/api";
import { Card, Empty, ErrorNote, Spinner, Table, Tag } from "../components/ui";
import { dt, when } from "../lib/format";
import type { Alert, AlertRule } from "../lib/types";
import { BellOff, Check, CheckCheck } from "lucide-react";

export default function Alerts() {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [rules, setRules] = useState<AlertRule[]>([]);
  const [status, setStatus] = useState<"active" | "acknowledged" | "resolved" | "all">("active");
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState<number | null>(null);

  const load = useCallback(async () => {
    try {
      const [a, r] = await Promise.all([
        api<{ alerts: Alert[] }>("/api/alerts", { query: { status } }),
        api<{ rules: AlertRule[] }>("/api/alerts/rules"),
      ]);
      setAlerts(a.alerts || []);
      setRules(r.rules || []);
      setErr(null);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [status]);

  useEffect(() => {
    void load();
  }, [load]);

  async function act(id: number, kind: "ack" | "resolve") {
    setBusy(id);
    try {
      await api(`/api/alerts/${id}/${kind}`, { method: "POST" });
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  async function toggleRule(r: AlertRule) {
    try {
      await api(`/api/alerts/rules/${r.key}`, {
        method: "PUT",
        body: { ...r, enabled: !r.enabled },
      });
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }

  if (loading && !alerts.length && !rules.length) return <Spinner label="Loading alerts…" />;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[20px] font-semibold tracking-tight">Alerts</h1>
          <p className="text-[13px] text-muted">Threshold rules evaluated on every metrics report.</p>
        </div>
        <div className="flex gap-1.5">
          {(["active", "acknowledged", "resolved", "all"] as const).map((s) => (
            <button key={s} className={`btn ${status === s ? "btn-primary" : ""}`} onClick={() => setStatus(s)}>
              {s}
            </button>
          ))}
        </div>
      </div>

      {err && <ErrorNote onRetry={() => void load()}>{err}</ErrorNote>}

      <Card title={`Alerts · ${alerts.length}`}>
        {alerts.length ? (
          <Table head={["Severity", "Computer", "Message", "Status", "Raised", ""]}>
            {alerts.map((a) => (
              <tr key={a.id}>
                <td className="td">
                  <Tag tone={a.severity === "critical" ? "bad" : a.severity === "warning" ? "warn" : "muted"}>
                    {a.severity}
                  </Tag>
                </td>
                <td className="td">
                  {a.agentId ? (
                    <Link className="link" to={`/computers/${a.agentId}`}>
                      {a.hostname || a.agentId}
                    </Link>
                  ) : (
                    "fleet"
                  )}
                </td>
                <td className="td">{a.message}</td>
                <td className="td">
                  <Tag tone={a.status === "active" ? "bad" : a.status === "acknowledged" ? "warn" : "ok"}>{a.status}</Tag>
                </td>
                <td className="td whitespace-nowrap text-muted">{dt(a.createdAt)}</td>
                <td className="td">
                  <div className="flex justify-end gap-1.5">
                    {a.status === "active" && (
                      <button className="btn" disabled={busy === a.id} onClick={() => void act(a.id, "ack")}>
                        <Check size={13} /> Ack
                      </button>
                    )}
                    {a.status !== "resolved" && (
                      <button className="btn" disabled={busy === a.id} onClick={() => void act(a.id, "resolve")}>
                        <CheckCheck size={13} /> Resolve
                      </button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </Table>
        ) : (
          <Empty>
            <span className="inline-flex items-center gap-2">
              <BellOff size={14} /> No {status === "all" ? "" : status} alerts.
            </span>
          </Empty>
        )}
      </Card>

      <Card title="Rules">
        {rules.length ? (
          <Table head={["Rule", "Threshold", "For", "Cooldown", "Severity", "Enabled"]}>
            {rules.map((r) => (
              <tr key={r.key}>
                <td className="td">
                  {r.name}
                  <div className="text-[11.5px] text-muted">{r.description || r.key}</div>
                </td>
                <td className="td font-mono text-[12px]">{r.threshold}</td>
                <td className="td text-muted">{r.durationSec ? `${r.durationSec}s` : "immediate"}</td>
                <td className="td text-muted">{r.cooldownSec ? `${r.cooldownSec}s` : "—"}</td>
                <td className="td">
                  <Tag tone={r.severity === "critical" ? "bad" : r.severity === "warning" ? "warn" : "muted"}>
                    {r.severity}
                  </Tag>
                </td>
                <td className="td">
                  <button className={`btn ${r.enabled ? "btn-primary" : ""}`} onClick={() => void toggleRule(r)}>
                    {r.enabled ? "on" : "off"}
                  </button>
                </td>
              </tr>
            ))}
          </Table>
        ) : (
          <Empty>No rules configured.</Empty>
        )}
      </Card>

      <p className="text-[11.5px] text-muted">
        Notifications are sent through the SMTP and Telegram settings on the Settings page. Last check {when(new Date().toISOString())}.
      </p>
    </div>
  );
}
