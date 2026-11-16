import { useEffect, useRef, useState, type ButtonHTMLAttributes, type CSSProperties, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { FILTERS_RESET_EVENT } from "./useQueryState";

/* ---------- Button ---------- */

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: "sm" | "md";
  block?: boolean;
}

/** Class string for buttons and button-looking links: btnClass("primary", "sm"). */
export function btnClass(variant: ButtonVariant = "secondary", size: "sm" | "md" = "md", extra = ""): string {
  return ["btn", `btn-${variant}`, size === "sm" ? "btn-sm" : "", extra].filter(Boolean).join(" ");
}

export function Button({ variant = "secondary", size = "md", block, className = "", type = "button", ...rest }: ButtonProps) {
  return <button type={type} className={btnClass(variant, size, `${block ? "btn-block" : ""} ${className}`.trim())} {...rest} />;
}

/* ---------- Field ---------- */

export interface FieldProps {
  label: ReactNode;
  /** id of the control; the label gets htmlFor={htmlFor}. */
  htmlFor?: string;
  hint?: ReactNode;
  error?: ReactNode;
  width?: number | string;
  /** Lets the field take the remaining toolbar width. */
  grow?: boolean;
  className?: string;
  children: ReactNode;
}

/** Label above a control, optional hint/error below. */
export function Field({ label, htmlFor, hint, error, width, grow, className = "", children }: FieldProps) {
  const style: CSSProperties | undefined = width !== undefined ? { width, flex: grow ? undefined : "none" } : undefined;
  return (
    <div className={`field${grow ? " grow" : ""} ${className}`.trim()} style={style}>
      <label className="field-label" htmlFor={htmlFor} id={htmlFor ? `${htmlFor}-label` : undefined}>
        {label}
      </label>
      {children}
      {error ? <div className="field-error">{error}</div> : hint ? <div className="field-hint">{hint}</div> : null}
    </div>
  );
}

/* ---------- TextFilter: debounced text input for auto-applied filters ---------- */

export interface TextFilterProps {
  value: string;
  /** Called 300ms after the user stops typing (or immediately on Enter / clear). */
  onChange: (value: string) => void;
  placeholder?: string;
  mono?: boolean;
  id?: string;
  width?: number | string;
  delay?: number;
  "aria-label"?: string;
}

export function TextFilter({ value, onChange, placeholder, mono, id, width, delay = 300, ...aria }: TextFilterProps) {
  const [draft, setDraft] = useState(value);
  const timer = useRef<number>();
  const last = useRef(value);
  // External changes (reset, back/forward) overwrite the draft and cancel a pending emit.
  useEffect(() => {
    if (value !== last.current) {
      window.clearTimeout(timer.current);
      last.current = value;
      setDraft(value);
    }
  }, [value]);
  // A toolbar reset may leave `value` unchanged ("" → ""), so it is signalled separately.
  useEffect(() => {
    const onReset = () => {
      window.clearTimeout(timer.current);
      setDraft(last.current);
    };
    window.addEventListener(FILTERS_RESET_EVENT, onReset);
    return () => {
      window.removeEventListener(FILTERS_RESET_EVENT, onReset);
      window.clearTimeout(timer.current);
    };
  }, []);

  function emit(v: string) {
    window.clearTimeout(timer.current);
    if (v === last.current) return;
    last.current = v;
    onChange(v);
  }

  return (
    <input
      id={id}
      type="search"
      className={mono ? "mono" : undefined}
      style={width !== undefined ? { width } : undefined}
      value={draft}
      placeholder={placeholder}
      aria-label={aria["aria-label"]}
      autoComplete="off"
      spellCheck={false}
      onChange={(e) => {
        const v = e.target.value;
        setDraft(v);
        window.clearTimeout(timer.current);
        if (v === "") emit("");
        else timer.current = window.setTimeout(() => emit(v.trim()), delay);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          emit(draft.trim());
        }
      }}
    />
  );
}

/* ---------- PageHeader ---------- */

export interface PageHeaderProps {
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
  /** Small "back" link above the title. */
  back?: { to: string; label: string };
}

export function PageHeader({ title, subtitle, actions, back }: PageHeaderProps) {
  return (
    <div className="page-head">
      <div>
        {back && (
          <Link className="page-back" to={back.to}>
            {back.label}
          </Link>
        )}
        <h1>{title}</h1>
        {subtitle && <p className="page-sub">{subtitle}</p>}
      </div>
      {actions && <div className="actions">{actions}</div>}
    </div>
  );
}

/* ---------- Toolbar (filter row) ---------- */

export interface ToolbarProps {
  children: ReactNode;
  /** Renders a ghost "Сбросить" button on the right. */
  onReset?: () => void;
  /** Disables the reset button (e.g. when no filter is set). */
  resetDisabled?: boolean;
  /** Extra controls on the right, before the reset button. */
  end?: ReactNode;
  /** Extra class on the toolbar, e.g. for a page-specific grid of fields. */
  className?: string;
}

export function Toolbar({ children, onReset, resetDisabled, end, className = "" }: ToolbarProps) {
  return (
    <div className={`toolbar ${className}`.trim()} role="search">
      <div className="toolbar-fields">{children}</div>
      {(onReset || end) && (
        <div className="toolbar-end">
          {end}
          {onReset && (
            <button type="button" className="btn btn-ghost" onClick={onReset} disabled={resetDisabled}>
              Сбросить
            </button>
          )}
        </div>
      )}
    </div>
  );
}

/* ---------- EmptyState / EmptyRow ---------- */

export function EmptyState({ title, children }: { title?: ReactNode; children?: ReactNode }) {
  return (
    <div className="empty-state">
      {title && <div className="empty-state-title">{title}</div>}
      {children}
    </div>
  );
}

/** Full-width table row with a short muted sentence. */
export function EmptyRow({ colSpan, children }: { colSpan: number; children: ReactNode }) {
  return (
    <tr>
      <td colSpan={colSpan} className="empty-row">
        {children}
      </td>
    </tr>
  );
}

/* ---------- DescriptionList ---------- */

export interface DescriptionItem {
  term: ReactNode;
  value: ReactNode;
  mono?: boolean;
  title?: string;
}

/** Two-column key/value list (`dl.kv`). Falsy items are skipped so callers can inline conditions. */
export function DescriptionList({
  items,
  className = "",
}: {
  items: (DescriptionItem | null | false | undefined)[];
  className?: string;
}) {
  return (
    <dl className={`kv ${className}`.trim()}>
      {items.filter(Boolean).map((it, i) => {
        const d = it as DescriptionItem;
        return (
          <div key={i} style={{ display: "contents" }}>
            <dt>{d.term}</dt>
            <dd className={d.mono ? "mono" : undefined} title={d.title}>
              {d.value === null || d.value === undefined || d.value === "" ? <span className="faint">—</span> : d.value}
            </dd>
          </div>
        );
      })}
    </dl>
  );
}

/* ---------- SegmentedStrip ---------- */

export interface Segment {
  value: string;
  label: string;
  count?: number | null;
  /** Dot colour key (code status or ok/bad/warn/accent/neutral). */
  dot?: string;
  /** Tooltip, e.g. the Russian meaning of a reason code. */
  title?: string;
  mono?: boolean;
}

export interface SegmentedStripProps {
  segments: Segment[];
  value: string;
  onChange: (value: string) => void;
  "aria-label"?: string;
}

/** One-row counter strip that doubles as a filter: "Все 512 | ● Эмитирован 300 | …". */
export function SegmentedStrip({ segments, value, onChange, ...aria }: SegmentedStripProps) {
  return (
    <div className="segmented" role="group" aria-label={aria["aria-label"]}>
      {segments.map((s) => {
        const active = s.value === value;
        return (
          <button
            key={s.value}
            type="button"
            className={`segment${active ? " is-active" : ""}`}
            aria-pressed={active}
            title={s.title}
            onClick={() => onChange(s.value)}
          >
            {s.dot && <span className={`dot dot-${s.dot}`} />}
            <span className={s.mono ? "mono" : undefined}>{s.label}</span>
            {s.count !== undefined && <span className="segment-count">{s.count ?? "—"}</span>}
          </button>
        );
      })}
    </div>
  );
}

/* ---------- Stat (KPI figure) ---------- */

export interface StatProps {
  /** Short id shown above the label, e.g. "K1". */
  id?: string;
  label: ReactNode;
  value: ReactNode;
  /** Target text, e.g. "≥ 99%". */
  target?: ReactNode;
  /** true = target met, false = missed, null = not measured yet. */
  ok?: boolean | null;
  /** Progress bar 0–100 with a target tick. */
  meter?: { percent: number | null | undefined; target: number };
  children?: ReactNode;
}

export function Stat({ id, label, value, target, ok = null, meter, children }: StatProps) {
  return (
    <div className="stat">
      {id && <div className="stat-id">{id}</div>}
      <div className="stat-label">{label}</div>
      <div className="stat-value">{value}</div>
      {(target || ok !== null) && (
        <div className="stat-target">
          {target && <span>Цель: {target}</span>}
          {ok !== null && (
            <span className={`stat-mark ${ok ? "is-ok" : "is-bad"}`}>{ok ? "Достигнута" : "Не достигнута"}</span>
          )}
          {ok === null && target && <span className="faint">нет данных</span>}
        </div>
      )}
      {meter && <Meter percent={meter.percent} target={meter.target} ok={ok} />}
      {children && <div className="stat-extra">{children}</div>}
    </div>
  );
}

export function Meter({ percent, target, ok = null }: { percent: number | null | undefined; target: number; ok?: boolean | null }) {
  const p = Math.max(0, Math.min(100, percent ?? 0));
  const cls = ok === null ? "" : ok ? " is-ok" : " is-bad";
  return (
    <div className={`meter${cls}`} title={`Цель ${target}%`}>
      <div className="meter-fill" style={{ width: `${p}%` }} />
      <div className="meter-target" style={{ left: `${target}%` }} />
    </div>
  );
}
