import { useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import { DayPicker } from "react-day-picker";
import { endOfMonth, format, isValid, parse, startOfMonth, subDays, subMonths } from "date-fns";
import { ru } from "date-fns/locale/ru";
import { ChevronDown } from "./icons";
import { useClickOutside, usePlacement, useStableId } from "./usePopover";

/** Period as plain calendar days, "YYYY-MM-DD" or "" (open bound). `to` is inclusive. */
export interface DateRangeValue {
  from: string;
  to: string;
}

export interface DateRangePreset {
  label: string;
  range: () => DateRangeValue;
}

export interface DateRangePickerProps {
  from: string;
  to: string;
  onChange: (range: DateRangeValue) => void;
  placeholder?: string;
  /** Replaces the default presets (today, 7 days, 30 days, the two previous calendar months). */
  presets?: DateRangePreset[];
  clearable?: boolean;
  disabled?: boolean;
  id?: string;
  size?: "sm" | "md";
  width?: number | string;
  className?: string;
  "aria-label"?: string;
  "aria-labelledby"?: string;
}

const YMD = "yyyy-MM-dd";

export function parseYmd(s: string | null | undefined): Date | undefined {
  if (!s) return undefined;
  const d = parse(s.slice(0, 10), YMD, new Date());
  return isValid(d) ? d : undefined;
}

export function toYmd(d: Date): string {
  return format(d, YMD);
}

/** "01.09.2026 — 15.10.2026", "с 01.09.2026", "по 15.10.2026", or "" for an empty range. */
export function formatRange(from: string, to: string): string {
  const f = parseYmd(from);
  const t = parseYmd(to);
  const fmt = (d: Date) => format(d, "dd.MM.yyyy");
  if (f && t) return from === to ? fmt(f) : `${fmt(f)} — ${fmt(t)}`;
  if (f) return `с ${fmt(f)}`;
  if (t) return `по ${fmt(t)}`;
  return "";
}

const capitalize = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

function monthPreset(d: Date): DateRangePreset {
  return {
    label: capitalize(format(d, "LLLL yyyy", { locale: ru })),
    range: () => ({ from: toYmd(startOfMonth(d)), to: toYmd(endOfMonth(d)) }),
  };
}

export function defaultPresets(today = new Date()): DateRangePreset[] {
  return [
    { label: "Сегодня", range: () => ({ from: toYmd(today), to: toYmd(today) }) },
    { label: "7 дней", range: () => ({ from: toYmd(subDays(today, 6)), to: toYmd(today) }) },
    { label: "30 дней", range: () => ({ from: toYmd(subDays(today, 29)), to: toYmd(today) }) },
    monthPreset(subMonths(today, 2)),
    monthPreset(subMonths(today, 1)),
  ];
}

/**
 * Local-day bounds for the API: "2026-09-01" → "2026-09-01T00:00:00+03:00" and
 * "2026-09-15" → "2026-09-15T23:59:59+03:00", so the period matches the dates the user sees
 * (timestamps in tables are shown in browser time). Empty input stays empty.
 */
export function dayBoundsToApi(from: string, to: string): DateRangeValue {
  const withOffset = (ymd: string, time: string) => {
    const d = parseYmd(ymd);
    if (!d) return "";
    const off = -d.getTimezoneOffset();
    const sign = off >= 0 ? "+" : "-";
    const abs = Math.abs(off);
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${toYmd(d)}T${time}${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`;
  };
  return { from: withOffset(from, "00:00:00"), to: withOffset(to, "23:59:59") };
}

/** Single trigger + popover with presets and a two-month range calendar (Monday first, ru). */
export function DateRangePicker({
  from,
  to,
  onChange,
  placeholder = "Любой период",
  presets,
  clearable = true,
  disabled = false,
  id,
  size = "md",
  width,
  className = "",
  ...aria
}: DateRangePickerProps) {
  const baseId = useStableId("drp", id);
  const [open, setOpen] = useState(false);
  // First click picks the anchor; the second click completes the range and closes.
  const [anchor, setAnchor] = useState<Date | undefined>();
  const [month, setMonth] = useState<Date>(() => startOfMonth(subMonths(new Date(), 1)));
  const [twoMonths, setTwoMonths] = useState(true);
  const wrapRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  const place = usePlacement(triggerRef, popRef, open);
  const presetList = useMemo(() => presets ?? defaultPresets(), [presets]);

  const fromD = parseYmd(from);
  const toD = parseYmd(to);

  function openPop() {
    if (disabled) return;
    const wide = window.innerWidth >= 760;
    setTwoMonths(wide);
    const base = fromD ?? toD;
    setMonth(startOfMonth(base ?? (wide ? subMonths(new Date(), 1) : new Date())));
    setAnchor(undefined);
    setOpen(true);
  }

  function close(refocus: boolean) {
    // A half-picked range (one click) is applied as a single day.
    if (anchor) {
      const d = toYmd(anchor);
      if (d !== from || d !== to) onChange({ from: d, to: d });
    }
    setAnchor(undefined);
    setOpen(false);
    if (refocus) triggerRef.current?.focus();
  }

  function commit(r: DateRangeValue) {
    setAnchor(undefined);
    setOpen(false);
    triggerRef.current?.focus();
    if (r.from !== from || r.to !== to) onChange(r);
  }

  function onDay(day: Date) {
    if (!anchor) {
      setAnchor(day);
      return;
    }
    const [a, b] = anchor <= day ? [anchor, day] : [day, anchor];
    commit({ from: toYmd(a), to: toYmd(b) });
  }

  useClickOutside([wrapRef], () => close(false), open);

  useEffect(() => {
    if (!open) return;
    // Move focus into the calendar so arrow keys work right away.
    const t = window.setTimeout(() => {
      const el =
        popRef.current?.querySelector<HTMLButtonElement>(".rdp-selected .rdp-day_button") ??
        popRef.current?.querySelector<HTMLButtonElement>(".rdp-today .rdp-day_button") ??
        popRef.current?.querySelector<HTMLButtonElement>(".drp-preset");
      el?.focus();
    }, 0);
    return () => window.clearTimeout(t);
  }, [open]);

  function onPopKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      setAnchor(undefined);
      setOpen(false);
      triggerRef.current?.focus();
    }
  }

  const label = formatRange(from, to);
  const activePreset = presetList.findIndex((p) => {
    const r = p.range();
    return r.from === from && r.to === to;
  });
  const showClear = clearable && !disabled && (from !== "" || to !== "");
  const style: CSSProperties | undefined = width !== undefined ? { width } : undefined;

  const selected = anchor ? { from: anchor, to: undefined } : fromD || toD ? { from: fromD ?? toD, to: toD ?? fromD } : undefined;

  return (
    <div
      ref={wrapRef}
      className={`daterange select-${size} ${className}`}
      style={style}
      onBlur={(e) => {
        // Tab (or any focus move) out of the picker closes it, like Select/Combobox. A null
        // relatedTarget (click on a non-focusable spot inside) is left to click-outside.
        const next = e.relatedTarget as Node | null;
        if (open && next && !wrapRef.current?.contains(next)) close(false);
      }}
    >
      <button
        ref={triggerRef}
        id={baseId}
        type="button"
        className={`select-trigger${showClear ? " has-clear" : ""}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={aria["aria-label"]}
        aria-labelledby={aria["aria-labelledby"]}
        disabled={disabled}
        onClick={() => (open ? close(false) : openPop())}
        onKeyDown={(e) => {
          if (!open && e.key === "ArrowDown") {
            e.preventDefault();
            openPop();
          }
        }}
      >
        <span className={`select-value${label ? "" : " is-placeholder"}`}>
          <span className="select-text">{label || placeholder}</span>
        </span>
        <ChevronDown />
      </button>
      {showClear && (
        <button
          type="button"
          className="select-clear"
          aria-label="Сбросить период"
          title="Сбросить период"
          onClick={() => {
            onChange({ from: "", to: "" });
            triggerRef.current?.focus();
          }}
        >
          ×
        </button>
      )}
      {open && (
        <div
          ref={popRef}
          role="dialog"
          aria-label="Выбор периода"
          className={`popover${place.up ? " is-up" : ""}${place.end ? " is-end" : ""}`}
          onKeyDown={onPopKeyDown}
        >
          <div className="drp">
            <div className="drp-presets">
              {presetList.map((p, i) => (
                <button
                  key={p.label}
                  type="button"
                  className={`drp-preset${i === activePreset ? " is-active" : ""}`}
                  onClick={() => commit(p.range())}
                >
                  {p.label}
                </button>
              ))}
              <div className="drp-preset-sep" />
              <button
                type="button"
                className="drp-preset drp-preset-reset"
                onClick={() => commit({ from: "", to: "" })}
              >
                Сбросить
              </button>
            </div>
            <div className="drp-main">
              <DayPicker
                mode="range"
                locale={ru}
                weekStartsOn={1}
                showOutsideDays={false}
                numberOfMonths={twoMonths ? 2 : 1}
                month={month}
                onMonthChange={setMonth}
                selected={selected}
                onSelect={(_range, day) => onDay(day)}
                formatters={{
                  formatCaption: (m: Date) => capitalize(format(m, "LLLL yyyy", { locale: ru })),
                }}
                labels={{
                  labelPrevious: () => "Предыдущий месяц",
                  labelNext: () => "Следующий месяц",
                }}
              />
              <div className="drp-foot">
                <span>
                  {anchor ? (
                    <>
                      С <strong>{format(anchor, "dd.MM.yyyy")}</strong> — выберите конечную дату
                    </>
                  ) : label ? (
                    <>
                      Период: <strong>{label}</strong>
                    </>
                  ) : (
                    "Выберите начальную дату"
                  )}
                </span>
                <button type="button" className="btn btn-ghost btn-sm" onClick={() => close(true)}>
                  Закрыть
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
