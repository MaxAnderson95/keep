import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth, isAuthError } from "../auth";

interface PollState<T> {
  data?: T;
  error?: Error;
  refresh: () => void;
}

/**
 * Polls a fetcher while the tab is visible (W10). Keeps the last good data
 * when a poll fails — e.g. a 503 config_invalid — so the UI can show a
 * banner over stale-but-useful content. A 401 invalidates the session.
 */
export function usePoll<T>(fetcher: () => Promise<T>, intervalMs: number): PollState<T> {
  const [data, setData] = useState<T | undefined>(undefined);
  const [error, setError] = useState<Error | undefined>(undefined);
  const { invalidate } = useAuth();
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;
  const [generation, setGeneration] = useState(0);

  const refresh = useCallback(() => setGeneration((g) => g + 1), []);

  useEffect(() => {
    let cancelled = false;
    const tick = async () => {
      if (document.visibilityState !== "visible") return;
      try {
        const next = await fetcherRef.current();
        if (!cancelled) {
          setData(next);
          setError(undefined);
        }
      } catch (err) {
        if (cancelled) return;
        if (isAuthError(err)) {
          invalidate();
          return;
        }
        setError(err instanceof Error ? err : new Error(String(err)));
      }
    };
    void tick();
    const id = setInterval(() => void tick(), intervalMs);
    const onVisible = () => {
      if (document.visibilityState === "visible") void tick();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      cancelled = true;
      clearInterval(id);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [intervalMs, invalidate, generation]);

  return { data, error, refresh };
}
