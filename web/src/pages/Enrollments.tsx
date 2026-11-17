import { useCallback, useEffect, useState } from "react";
import { api } from "../lib/api";
import { Card, Empty, ErrorNote, Spinner, Table, Tag } from "../components/ui";
import { dt, platformLabel } from "../lib/format";
import type { EnrollmentRequest } from "../lib/types";
import { Check, X } from "lucide-react";

export default function Enrollments() {
  const [rows, setRows] = useState<EnrollmentRequest[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await api<{ requests: EnrollmentRequest[] }>("/api/enrollments");
      setRows(res.requests || []);
      setErr(null);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
    const t = window.setInterval(() => void load(), 10000);
    return () => window.clearInterval(t);
  }, [load]);

  async function decide(id: string, approve: boolean) {
    setBusy(id);
    try {
      await api(`/api/agents/${id}/${approve ? "approve" : "reject"}`, { method: "POST" });
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  if (loading && !rows.length) return <Spinner label="Loading enrollments…" />;

  const pending = rows.filter((r) => r.decision === "pending");
  const decided = rows.filter((r) => r.decision !== "pending");

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-[20px] font-semibold tracking-tight">Enrollments</h1>
        <p className="text-[13px] text-muted">
          A new agent appears here the first time it contacts the server. Nothing is collected from it until you approve it.
        </p>
      </div>

      {err && <ErrorNote onRetry={() => void load()}>{err}</ErrorNote>}

      <Card title={`Awaiting approval · ${pending.length}`}>
        {pending.length ? (
          <Table head={["Computer", "Platform", "Address", "Requested", ""]}>
            {pending.map((r) => (
              <tr key={r.id}>
                <td className="td">
                  <div className="font-medium">{r.hostname}</div>
                  <div className="font-mono text-[11px] text-muted">{r.agentId}</div>
                </td>
                <td className="td">
                  {platformLabel(r.platform)} {r.osVersion || ""}
                  <div className="text-[11.5px] text-muted">
                    {r.arch} · agent {r.agentVersion || "?"}
                  </div>
                </td>
                <td className="td font-mono text-[12px] text-muted">{r.remoteAddr || "—"}</td>
                <td className="td whitespace-nowrap text-muted">{dt(r.requestedAt)}</td>
                <td className="td">
                  <div className="flex justify-end gap-1.5">
                    <button
                      className="btn btn-primary"
                      disabled={busy === r.agentId}
                      onClick={() => void decide(r.agentId, true)}
                    >
                      <Check size={14} /> Approve
                    </button>
                    <button
                      className="btn btn-danger"
                      disabled={busy === r.agentId}
                      onClick={() => void decide(r.agentId, false)}
                    >
                      <X size={14} /> Reject
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </Table>
        ) : (
          <Empty>No pending requests. Start the agent on a machine with <code className="font-mono">pcim-agent enroll</code>.</Empty>
        )}
      </Card>

      <Card title={`Recent decisions · ${decided.length}`}>
        {decided.length ? (
          <Table head={["Computer", "Platform", "Decision", "Requested"]}>
            {decided.slice(0, 50).map((r) => (
              <tr key={r.id}>
                <td className="td">{r.hostname}</td>
                <td className="td text-muted">{platformLabel(r.platform)}</td>
                <td className="td">
                  <Tag tone={r.decision === "approved" ? "ok" : r.decision === "rejected" ? "bad" : "muted"}>{r.decision}</Tag>
                </td>
                <td className="td text-muted">{dt(r.requestedAt)}</td>
              </tr>
            ))}
          </Table>
        ) : (
          <Empty>No decisions yet.</Empty>
        )}
      </Card>
    </div>
  );
}
