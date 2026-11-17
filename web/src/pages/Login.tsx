import { useState } from "react";
import { useApp } from "../lib/store";
import { HardDrive, Server } from "lucide-react";

export default function Login() {
  const { signIn } = useApp();
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    setBusy(true);
    try {
      await signIn(username.trim(), password);
    } catch (e2) {
      setErr(e2 instanceof Error ? e2.message : String(e2));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-full items-center justify-center px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex items-center gap-2.5">
          <span className="grid h-9 w-9 place-items-center rounded-lg border border-line bg-panel">
            <HardDrive size={18} className="text-accent" />
          </span>
          <div>
            <div className="text-[15px] font-semibold leading-tight">
              PC <span className="text-accent">Inventory</span> Manager
            </div>
            <div className="text-[11.5px] text-muted">self-hosted hardware inventory</div>
          </div>
        </div>

        <form onSubmit={submit} className="card p-5">
          <label className="mb-1 block text-[12.5px] text-muted">Username</label>
          <input
            className="input mb-3"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            autoFocus
          />
          <label className="mb-1 block text-[12.5px] text-muted">Password</label>
          <input
            className="input"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
          {err && (
            <div className="mt-3 rounded-lg border border-bad/40 bg-bad/10 px-3 py-2 text-[13px] text-bad">{err}</div>
          )}
          <button className="btn btn-primary mt-4 w-full justify-center" disabled={busy || !password}>
            {busy ? "Signing in…" : "Sign in"}
          </button>
          <p className="mt-3 flex items-start gap-1.5 text-[11.5px] leading-relaxed text-muted">
            <Server size={12} className="mt-0.5 shrink-0" />
            Five failed attempts lock the account for 5 minutes. The initial password is in
            <code className="mx-1 rounded bg-panel2 px-1">initial-admin-password.txt</code> in the server's data folder.
          </p>
        </form>
      </div>
    </div>
  );
}
