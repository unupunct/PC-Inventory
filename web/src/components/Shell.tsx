import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useApp } from "../lib/store";
import {
  Activity,
  Bell,
  HardDrive,
  LayoutDashboard,
  LogOut,
  MonitorSmartphone,
  Network,
  Settings,
  ShieldCheck,
  Users,
} from "lucide-react";
import type { ReactNode } from "react";

const nav: { to: string; label: string; icon: ReactNode }[] = [
  { to: "/", label: "Overview", icon: <LayoutDashboard size={16} /> },
  { to: "/computers", label: "Computers", icon: <MonitorSmartphone size={16} /> },
  { to: "/enrollments", label: "Enrollments", icon: <Users size={16} /> },
  { to: "/alerts", label: "Alerts", icon: <Bell size={16} /> },
  { to: "/discovery", label: "Discovery", icon: <Network size={16} /> },
  { to: "/reports", label: "Reports", icon: <Activity size={16} /> },
  { to: "/settings", label: "Settings", icon: <Settings size={16} /> },
];

export default function Shell() {
  const { me, signOut, live, dashboard, agents } = useApp();
  const navigate = useNavigate();
  const pending = agents.filter((a) => a.status === "pending").length;
  const activeAlerts = dashboard?.alertCounts?.active ?? dashboard?.activeAlerts?.length ?? 0;

  return (
    <div className="flex min-h-full">
      <aside className="sticky top-0 hidden h-screen w-[212px] shrink-0 flex-col border-r border-line bg-panel2/60 md:flex">
        <div className="flex items-center gap-2.5 px-4 py-4">
          <span className="grid h-8 w-8 place-items-center rounded-lg border border-line bg-panel">
            <HardDrive size={16} className="text-accent" />
          </span>
          <div className="leading-tight">
            <div className="text-[13.5px] font-semibold">
              PC <span className="text-accent">Inventory</span>
            </div>
            <div className="text-[10.5px] uppercase tracking-wider text-muted">Manager</div>
          </div>
        </div>
        <nav className="flex flex-col gap-0.5 px-2 py-2">
          {nav.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.to === "/"}
              className={({ isActive }) =>
                `flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-[13px] transition-colors ${
                  isActive ? "bg-accent/15 text-white" : "text-muted hover:bg-white/5 hover:text-ink"
                }`
              }
            >
              {n.icon}
              <span className="grow">{n.label}</span>
              {n.to === "/enrollments" && pending > 0 && (
                <span className="rounded-md bg-warn/20 px-1.5 text-[11px] font-semibold text-warn">{pending}</span>
              )}
              {n.to === "/alerts" && activeAlerts > 0 && (
                <span className="rounded-md bg-bad/20 px-1.5 text-[11px] font-semibold text-bad">{activeAlerts}</span>
              )}
            </NavLink>
          ))}
        </nav>
        <div className="grow" />
        <div className="border-t border-line px-3 py-3 text-[11.5px] text-muted">
          <div className="mb-1 flex items-center gap-1.5">
            <ShieldCheck size={12} />
            <span className="truncate">{me?.username}</span>
          </div>
          <div className="truncate text-muted/70">{me?.serverName}</div>
        </div>
      </aside>

      <div className="flex min-w-0 grow flex-col">
        <header className="sticky top-0 z-30 flex items-center gap-3 border-b border-line bg-bg/85 px-4 py-2.5 backdrop-blur md:px-6">
          <div className="md:hidden text-[13px] font-semibold">
            PC <span className="text-accent">Inventory</span>
          </div>
          <div className="grow" />
          <span className="inline-flex items-center gap-1.5 rounded-full border border-line bg-panel px-2.5 py-1 text-[11.5px] text-muted">
            <span
              className={`h-1.5 w-1.5 rounded-full ${
                live === "live" ? "bg-ok" : live === "offline" ? "bg-bad" : "bg-warn"
              }`}
            />
            {live === "live" ? "live" : live === "offline" ? "reconnecting" : "connecting"}
          </span>
          <button className="btn" onClick={() => navigate("/settings")}>
            <Settings size={14} />
            <span className="hidden sm:inline">Settings</span>
          </button>
          <button className="btn" onClick={() => void signOut()}>
            <LogOut size={14} />
            <span className="hidden sm:inline">Sign out</span>
          </button>
        </header>

        <main className="mx-auto w-full max-w-[1400px] px-4 py-5 md:px-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
