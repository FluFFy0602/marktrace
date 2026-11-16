import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";

/** Fired by QueryFilters.reset(); TextFilter drops its pending debounce on it. */
export const FILTERS_RESET_EVENT = "marktrace:filters-reset";

function liveParams(): URLSearchParams {
  return new URLSearchParams(window.location.search);
}

export interface QueryFilters<K extends string> {
  /** Current values from the URL ("" when absent). */
  values: Record<K, string>;
  /** Merges a patch into the URL; empty strings remove the key. Resets the page offset. */
  set: (patch: Partial<Record<K, string>>) => void;
  /** Removes every filter key and the offset. */
  reset: () => void;
  /** True when at least one filter key has a value. */
  active: boolean;
  offset: number;
  setOffset: (offset: number) => void;
}

/**
 * URL-synced filter state. Every change replaces the history entry, so typing in a filter does not
 * flood the back button; the URL stays shareable. Unknown query keys are preserved.
 *
 *   const f = useQueryFilters(["status", "item_code", "from", "to", "q"] as const);
 *   <Select value={f.values.status} onChange={(v) => f.set({ status: v })} … />
 */
export function useQueryFilters<K extends string>(keys: readonly K[]): QueryFilters<K> {
  const [sp, setSp] = useSearchParams();
  const keyStr = keys.join(",");

  const values = useMemo(() => {
    const v = {} as Record<K, string>;
    for (const k of keys) v[k] = sp.get(k) ?? "";
    return v;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sp, keyStr]);

  // Always build the next query from the live URL, not from the render-time snapshot that
  // setSearchParams(fn) would hand us: a debounced TextFilter fires from a stale closure and
  // would otherwise silently undo changes made in the meantime.
  const set = useCallback(
    (patch: Partial<Record<K, string>>) => {
      const n = liveParams();
      for (const [k, v] of Object.entries(patch) as [K, string | undefined][]) {
        if (v) n.set(k, v);
        else n.delete(k);
      }
      n.delete("offset");
      setSp(n, { replace: true });
    },
    [setSp],
  );

  const reset = useCallback(() => {
    // Pending debounced text edits must not re-apply themselves after a reset.
    window.dispatchEvent(new Event(FILTERS_RESET_EVENT));
    const n = liveParams();
    for (const k of keyStr.split(",")) n.delete(k);
    n.delete("offset");
    setSp(n, { replace: true });
  }, [setSp, keyStr]);

  const offset = Math.max(0, Number(sp.get("offset") ?? 0) || 0);
  const setOffset = useCallback(
    (o: number) => {
      const n = liveParams();
      if (o > 0) n.set("offset", String(o));
      else n.delete("offset");
      setSp(n);
    },
    [setSp],
  );

  const active = keys.some((k) => values[k] !== "");
  return { values, set, reset, active, offset, setOffset };
}
