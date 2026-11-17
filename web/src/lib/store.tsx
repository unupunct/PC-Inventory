// Live state: the agent list, dashboard snapshot and the SSE connection.
// One provider keeps polling and streaming so every page sees the same data.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { api, ApiError } from "./api";
import type { Agent, Dashboard, Me } from "./types";

type LiveStatus = "connecting" | "live" | "offline";

interface AppState {
  me: Me | null;
  agents: Agent[];
  dashboard: Dashboard | null;
  loading: boolean;
  error: string | null;
  live: LiveStatus;
  lastEventAt: number | null;
  refresh: () => Promise<void>;
  signIn: (username: string, password: string) => Promise<void>;
  signOut: () => Promise<void>;
  sessionExpired: boolean;
}

const Ctx = createContext<AppState | null>(null);

export function useApp(): AppState {
  const v = useContext(Ctx);
  if (!v) throw new Error("useApp outside AppProvider");
  return v;
}

export function AppProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null>(null);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [dashboard, setDashboard] = useState<Dashboard | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [live, setLive] = useState<LiveStatus>("connecting");
  const [lastEventAt, setLastEventAt] = useState<number | null>(null);
  const [sessionExpired, setSessionExpired] = useState(false);
  const esRef = useRef<EventSource | null>(null);
  const timerRef = useRef<number | null>(null);

  const refresh = useCallback(async () => {
    try {
      const [agentsRes, dash] = await Promise.all([
        api<{ agents: Agent[] }>("/api/agents"),
        api<Dashboard>("/api/dashboard"),
      ]);
      setAgents(agentsRes.agents || []);
      setDashboard(dash);
      setError(null);
      setSessionExpired(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        setSessionExpired(true);
        setMe(null);
      } else {
        setError(e instanceof Error ? e.message : String(e));
      }
    } finally {
      setLoading(false);
    }
  }, []);

  // Bootstrap: who am I?
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const who = await api<Me>("/api/auth/me");
        if (!cancelled) {
          setMe(who);
          await refresh();
        }
      } catch {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [refresh]);

  // Poll while signed in (SSE only carries change notifications).
  useEffect(() => {
    if (!me) return;
    const every = Math.max(5, Number(dashboard?.stats ? 15 : 15)) * 1000;
    timerRef.current = window.setInterval(() => {
      if (document.visibilityState === "visible") void refresh();
    }, every);
    return () => {
      if (timerRef.current) window.clearInterval(timerRef.current);
    };
  }, [me, refresh, dashboard?.stats]);

  // SSE for instant updates.
  useEffect(() => {
    if (!me) {
      esRef.current?.close();
      esRef.current = null;
      return;
    }
    const es = new EventSource("/api/stream");
    esRef.current = es;
    es.onopen = () => setLive("live");
    es.onerror = () => setLive("offline");
    es.onmessage = (ev) => {
      setLive("live");
      setLastEventAt(Date.now());
      try {
        const payload = JSON.parse(ev.data) as { type?: string };
        const t = payload.type || "";
        if (
          t.startsWith("agent") ||
          t.startsWith("metrics") ||
          t.startsWith("inventory") ||
          t.startsWith("alert") ||
          t.startsWith("change") ||
          t.startsWith("enrollment")
        ) {
          void refresh();
        }
      } catch {
        void refresh();
      }
    };
    return () => {
      es.close();
      esRef.current = null;
    };
  }, [me, refresh]);

  const signIn = useCallback(
    async (username: string, password: string) => {
      await api("/api/auth/login", { method: "POST", body: { username, password } });
      const who = await api<Me>("/api/auth/me");
      setMe(who);
      setLoading(true);
      await refresh();
    },
    [refresh]
  );

  const signOut = useCallback(async () => {
    try {
      await api("/api/auth/logout", { method: "POST" });
    } catch {
      /* ignore */
    }
    esRef.current?.close();
    setMe(null);
    setAgents([]);
    setDashboard(null);
    setLive("connecting");
  }, []);

  const value = useMemo<AppState>(
    () => ({ me, agents, dashboard, loading, error, live, lastEventAt, refresh, signIn, signOut, sessionExpired }),
    [me, agents, dashboard, loading, error, live, lastEventAt, refresh, signIn, signOut, sessionExpired]
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
