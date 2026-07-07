import { useEffect, useState } from "react";
import { api, type Meta } from "../api";

/** One-shot fetch of /api/v1/meta; null until loaded or on failure. */
export function useMeta(): Meta | null {
  const [meta, setMeta] = useState<Meta | null>(null);
  useEffect(() => {
    let cancelled = false;
    api
      .meta()
      .then((m) => {
        if (!cancelled) setMeta(m);
      })
      .catch(() => {
        if (!cancelled) setMeta(null);
      });
    return () => {
      cancelled = true;
    };
  }, []);
  return meta;
}
