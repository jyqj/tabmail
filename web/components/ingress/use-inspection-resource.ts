"use client";
import { useCallback, useEffect, useState } from "react";

// No shared cache. Switching receipt, filter, account or refresh generation hides
// old data immediately; aborted or out-of-order responses cannot revive it.
export function useInspectionResource<T>(
  key: string,
  load: (signal: AbortSignal) => Promise<T>,
) {
  const [generation, setGeneration] = useState(0);
  const version = JSON.stringify([key, generation]);
  const [result, setResult] = useState<{
    version: string;
    data?: T;
    error?: unknown;
  }>();
  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    load(controller.signal).then(
      (data) => {
        if (active) setResult({ version, data });
      },
      (error) => {
        if (active) setResult({ version, error });
      },
    );
    return () => {
      active = false;
      controller.abort();
    };
  }, [version, load]);
  const current = result?.version === version ? result : undefined;
  const refresh = useCallback(() => setGeneration((n) => n + 1), []);
  return {
    data: current?.data,
    error: current?.error,
    loading: !current,
    refresh,
  };
}
