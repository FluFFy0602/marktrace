import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";

/** Calls `onOutside` on a mousedown/touchstart outside every element in `refs` while `enabled`. */
export function useClickOutside(
  refs: RefObject<HTMLElement | null>[],
  onOutside: () => void,
  enabled: boolean,
): void {
  const cb = useRef(onOutside);
  cb.current = onOutside;
  const refsRef = useRef(refs);
  refsRef.current = refs;
  useEffect(() => {
    if (!enabled) return;
    const handler = (e: MouseEvent | TouchEvent) => {
      const t = e.target as Node | null;
      if (t && refsRef.current.some((r) => r.current?.contains(t))) return;
      cb.current();
    };
    document.addEventListener("mousedown", handler);
    document.addEventListener("touchstart", handler);
    return () => {
      document.removeEventListener("mousedown", handler);
      document.removeEventListener("touchstart", handler);
    };
  }, [enabled]);
}

export interface Placement {
  /** Opens above the anchor when there is not enough room below. */
  up: boolean;
  /** Aligns to the anchor's right edge when the popover would overflow the viewport on the right. */
  end: boolean;
}

/**
 * Measures the popover after it mounts and decides whether to flip it up and/or align it to the
 * right edge. The popover is positioned with CSS (`.popover`, `.is-up`, `.is-end`).
 */
export function usePlacement(
  anchorRef: RefObject<HTMLElement | null>,
  popRef: RefObject<HTMLElement | null>,
  open: boolean,
): Placement {
  const [place, setPlace] = useState<Placement>({ up: false, end: false });
  useLayoutEffect(() => {
    if (!open) {
      setPlace({ up: false, end: false });
      return;
    }
    const measure = () => {
      const a = anchorRef.current;
      const p = popRef.current;
      if (!a || !p) return;
      const r = a.getBoundingClientRect();
      const h = p.offsetHeight;
      const w = p.offsetWidth;
      const below = window.innerHeight - r.bottom;
      const above = r.top;
      const up = below < h + 8 && above > below;
      const end = r.left + w > window.innerWidth - 8 && r.right - w >= 8;
      setPlace((prev) => (prev.up === up && prev.end === end ? prev : { up, end }));
    };
    measure();
    window.addEventListener("resize", measure);
    return () => window.removeEventListener("resize", measure);
  }, [open, anchorRef, popRef]);
  return place;
}

let seq = 0;
/** Stable unique id prefix for aria wiring (React 18 has useId, but this keeps ids CSS-safe). */
export function useStableId(prefix: string, given?: string): string {
  const ref = useRef<string>();
  if (!ref.current) ref.current = given ?? `${prefix}-${++seq}`;
  return given ?? ref.current;
}

/** Scrolls the element with `id` inside `container` into view (nearest edge). */
export function scrollOptionIntoView(container: HTMLElement | null, id: string | undefined): void {
  if (!container || !id) return;
  const el = container.querySelector<HTMLElement>(`[id="${id}"]`);
  el?.scrollIntoView({ block: "nearest" });
}
