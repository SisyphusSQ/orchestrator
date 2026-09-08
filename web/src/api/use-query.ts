import { useCallback, useEffect, useRef, useState } from "react";
import { query } from "./client";

// Refreshes never overlap; stale responses cannot replace a newly selected resource.
export function useQuery<T>(
  path: string | null,
  interval = 15000,
): {
  data?: T;
  error?: Error;
  loading: boolean;
  at?: number;
  refresh: () => void;
} {
  const [snapshot, setSnapshot] = useState<{
    path: string | null;
    data?: T;
    error?: Error;
    loading: boolean;
    at?: number;
  }>({ path, loading: true });
  const [revision, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision((n) => n + 1), []);
  const intervalRef = useRef(interval);
  intervalRef.current = interval;
  useEffect(() => {
    if (!path) {
      setSnapshot({ path, loading: false });
      return;
    }
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    const controller = new AbortController();
    const fetchData = async () => {
      setSnapshot((old) => ({
        ...old,
        path,
        data: old.path === path ? old.data : undefined,
        loading: true,
      }));
      try {
        const data = await query<T>(path, controller.signal);
        if (active) setSnapshot({ path, data, loading: false, at: Date.now() });
      } catch (error) {
        if (active)
          setSnapshot((old) => ({
            ...old,
            error: error as Error,
            loading: false,
          }));
      }
      if (active && intervalRef.current > 0)
        timer = setTimeout(fetchData, intervalRef.current);
    };
    void fetchData();
    return () => {
      active = false;
      controller.abort();
      clearTimeout(timer);
    };
  }, [path, revision, interval]);
  useEffect(() => {
    window.addEventListener("orch:refresh", refresh);
    return () => window.removeEventListener("orch:refresh", refresh);
  }, [refresh]);
  return {
    ...(snapshot.path === path ? snapshot : { loading: true }),
    refresh,
  };
}
export const refreshAll = () => window.dispatchEvent(new Event("orch:refresh"));
