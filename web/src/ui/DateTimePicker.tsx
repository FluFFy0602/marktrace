import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import { DayPicker } from "react-day-picker";
import { format, isValid, parse } from "date-fns";
import { ru } from "date-fns/locale/ru";
import { ChevronDown } from "./icons";
import { useClickOutside, usePlacement, useStableId } from "./usePopover";

/**
 * Local date-time in the datetime-local wire format "YYYY-MM-DDTHH:MM:SS" (no offset), or "".
 * Kept identical to <input type="datetime-local" step="1"> so labels.localToRfc3339 still applies.
 */
export interface DateTimePickerProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
  id?: string;
  size?: "sm" | "md";
  width?: number | string;
  className?: string;
  "aria-label"?: string;
  "aria-labelledby"?: string;
}

const WIRE = "yyyy-MM-dd'T'HH:mm:ss";
const pad = (n: number) => String(n).padStart(2, "0");
const capitalize = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

function parseWire(v: string): Date | undefined {
  if (!v) return undefined;
  const s = v.length === 16 ? `${v}:00` : v.slice(0, 19);
  const d = parse(s, WIRE, new Date());
  return isValid(d) ? d : undefined;
}

type TimeParts = { h: string; m: string; s: string };

function partsOf(d: Date | undefined): TimeParts {
  if (!d) return { h: "", m: "", s: "" };
  return { h: pad(d.getHours()), m: pad(d.getMinutes()), s: pad(d.getSeconds()) };
}

function clampPart(raw: string, max: number): number {
  const n = Number.parseInt(raw, 10);
  if (!Number.isFinite(n) || n < 0) return 0;
  return Math.min(n, max);
}

/** Trigger showing "16.11.2026, 20:13:45" + popover with a Monday-first ru calendar and 24h time. */
export function DateTimePicker({
  value,
  onChange,
  placeholder = "Выберите дату и время",
  disabled = false,
  id,
  size = "md",
  width,
  className = "",
  ...aria
}: DateTimePickerProps) {
  const baseId = useStableId("dtp", id);
  const [open, setOpen] = useState(false);
  const current = parseWire(value);
  // Draft lives only while the popover is open; "Готово" / Enter / click-outside commit it.
  const [day, setDay] = useState<Date | undefined>(current);
  const [month, setMonth] = useState<Date>(current ?? new Date());
  const [time, setTime] = useState<TimeParts>(partsOf(current));
  const wrapRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  const place = usePlacement(triggerRef, popRef, open);

  function openPop() {
    if (disabled) return;
    const base = parseWire(value) ?? new Date();
    setDay(base);
    setMonth(base);
    setTime(partsOf(base));
    setOpen(true);
  }

  function draftValue(): string {
    if (!day) return "";
    const h = clampPart(time.h, 23);
    const m = clampPart(time.m, 59);
    const s = clampPart(time.s, 59);
    return `${format(day, "yyyy-MM-dd")}T${pad(h)}:${pad(m)}:${pad(s)}`;
  }

  function commit(refocus: boolean) {
    const next = draftValue();
    setOpen(false);
    if (refocus) triggerRef.current?.focus();
    if (next && next !== value) onChange(next);
  }

  function setNow() {
    const now = new Date();
    setDay(now);
    setMonth(now);
    setTime(partsOf(now));
  }

  useClickOutside([wrapRef], () => commit(false), open);

  useEffect(() => {
    if (!open) return;
    const t = window.setTimeout(() => {
      const el =
        popRef.current?.querySelector<HTMLButtonElement>(".rdp-selected .rdp-day_button") ??
        popRef.current?.querySelector<HTMLButtonElement>(".rdp-today .rdp-day_button");
      el?.focus();
    }, 0);
    return () => window.clearTimeout(t);
  }, [open]);

  function onPopKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      setOpen(false);
      triggerRef.current?.focus();
    } else if (e.key === "Enter" && (e.target as HTMLElement).tagName === "INPUT") {
      e.preventDefault();
      commit(true);
    }
  }

  function timeInput(part: keyof TimeParts, max: number, label: string) {
    return (
      <input
        className="dtp-time-input mono"
        inputMode="numeric"
        maxLength={2}
        aria-label={label}
        value={time[part]}
        placeholder="00"
        onChange={(e) => setTime({ ...time, [part]: e.target.value.replace(/\D/g, "").slice(0, 2) })}
        onBlur={() => setTime((t) => ({ ...t, [part]: t[part] === "" ? "00" : pad(clampPart(t[part], max)) }))}
        onFocus={(e) => e.target.select()}
        onKeyDown={(e) => {
          if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return;
          e.preventDefault();
          const n = clampPart(time[part], max);
          const next = e.key === "ArrowUp" ? (n + 1) % (max + 1) : (n + max) % (max + 1);
          setTime({ ...time, [part]: pad(next) });
        }}
      />
    );
  }

  const label = current ? format(current, "dd.MM.yyyy, HH:mm:ss") : "";
  const style: CSSProperties | undefined = width !== undefined ? { width } : undefined;

  return (
    <div
      ref={wrapRef}
      className={`daterange select-${size} ${className}`}
      style={style}
      onBlur={(e) => {
        const next = e.relatedTarget as Node | null;
        if (open && next && !wrapRef.current?.contains(next)) commit(false);
      }}
    >
      <button
        ref={triggerRef}
        id={baseId}
        type="button"
        className="select-trigger"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={aria["aria-label"]}
        aria-labelledby={aria["aria-labelledby"]}
        disabled={disabled}
        onClick={() => (open ? commit(false) : openPop())}
        onKeyDown={(e) => {
          if (!open && e.key === "ArrowDown") {
            e.preventDefault();
            openPop();
          }
        }}
      >
        <span className={`select-value${label ? "" : " is-placeholder"}`}>
          <span className="select-text mono-nums">{label || placeholder}</span>
        </span>
        <ChevronDown />
      </button>
      {open && (
        <div
          ref={popRef}
          role="dialog"
          aria-label="Выбор даты и времени"
          className={`popover${place.up ? " is-up" : ""}${place.end ? " is-end" : ""}`}
          onKeyDown={onPopKeyDown}
        >
          <div className="dtp">
            <DayPicker
              mode="single"
              required
              locale={ru}
              weekStartsOn={1}
              showOutsideDays
              month={month}
              onMonthChange={setMonth}
              selected={day}
              onSelect={(d) => d && setDay(d)}
              formatters={{
                formatCaption: (m: Date) => capitalize(format(m, "LLLL yyyy", { locale: ru })),
              }}
              labels={{
                labelPrevious: () => "Предыдущий месяц",
                labelNext: () => "Следующий месяц",
              }}
            />
            <div className="dtp-time">
              <span className="dtp-time-label">Время</span>
              <div className="dtp-time-fields">
                {timeInput("h", 23, "Часы")}
                <span className="dtp-colon">:</span>
                {timeInput("m", 59, "Минуты")}
                <span className="dtp-colon">:</span>
                {timeInput("s", 59, "Секунды")}
              </div>
            </div>
            <div className="drp-foot">
              <button type="button" className="btn btn-ghost btn-sm" onClick={setNow}>
                Сейчас
              </button>
              <button type="button" className="btn btn-primary btn-sm" onClick={() => commit(true)}>
                Готово
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
