import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent, type MouseEvent } from "react";
import { Check, ChevronDown } from "./icons";
import { scrollOptionIntoView, useClickOutside, usePlacement, useStableId } from "./usePopover";

export interface SelectOption {
  value: string;
  label: string;
  /** Dot colour key: a code status (issued, introduced, sold, returned, disposed) or ok/bad/warn/accent/neutral. */
  dot?: string;
  /** Monospace prefix shown before the label (e.g. a reason code). */
  code?: string;
  /** Muted text on the right side of the option (e.g. a count). */
  hint?: string;
  disabled?: boolean;
}

export interface SelectProps {
  value: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  /** Adds a first option with value "" and this label, e.g. "Все статусы". */
  allLabel?: string;
  /** Shown in muted text when nothing is selected and there is no `allLabel`. */
  placeholder?: string;
  /** Shows a × button that resets the value to "". */
  clearable?: boolean;
  disabled?: boolean;
  id?: string;
  size?: "sm" | "md";
  /** CSS width of the control, e.g. 180 or "100%". */
  width?: number | string;
  className?: string;
  "aria-label"?: string;
  "aria-labelledby"?: string;
}

export function withAll(options: SelectOption[], allLabel?: string): SelectOption[] {
  return allLabel !== undefined ? [{ value: "", label: allLabel }, ...options] : options;
}

export function OptionBody({ o }: { o: SelectOption }) {
  return (
    <>
      {o.dot && <span className={`dot dot-${o.dot}`} />}
      {o.code && <span className="option-code">{o.code}</span>}
      <span className="option-text">{o.label}</span>
      {o.hint && <span className="option-hint">{o.hint}</span>}
    </>
  );
}

/** Button-trigger select with a floating listbox (ARIA select-only combobox pattern). */
export function Select({
  value,
  onChange,
  options,
  allLabel,
  placeholder = "Не выбрано",
  clearable = false,
  disabled = false,
  id,
  size = "md",
  width,
  className = "",
  ...aria
}: SelectProps) {
  const baseId = useStableId("sel", id);
  const listId = `${baseId}-list`;
  const list = withAll(options, allLabel);
  const selectedIdx = list.findIndex((o) => o.value === value);
  const selected = selectedIdx >= 0 ? list[selectedIdx] : undefined;

  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const wrapRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const typeahead = useRef({ text: "", at: 0 });
  const place = usePlacement(triggerRef, popRef, open);

  useClickOutside([wrapRef], () => setOpen(false), open);

  const optId = (i: number) => `${baseId}-opt-${i}`;
  useEffect(() => {
    if (open && active >= 0) scrollOptionIntoView(listRef.current, optId(active));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, active]);

  const firstEnabled = (from: number, dir: 1 | -1): number => {
    for (let i = from; i >= 0 && i < list.length; i += dir) if (!list[i].disabled) return i;
    return -1;
  };

  function openList(at?: number) {
    if (disabled) return;
    setActive(at ?? (selectedIdx >= 0 ? selectedIdx : firstEnabled(0, 1)));
    setOpen(true);
  }

  function choose(i: number) {
    const o = list[i];
    if (!o || o.disabled) return;
    setOpen(false);
    triggerRef.current?.focus();
    if (o.value !== value) onChange(o.value);
  }

  function move(dir: 1 | -1) {
    const start = active < 0 ? (dir === 1 ? 0 : list.length - 1) : active + dir;
    const next = firstEnabled(Math.max(0, Math.min(list.length - 1, start)), dir);
    if (next >= 0) setActive(next);
  }

  function typeAhead(ch: string) {
    const now = Date.now();
    const t = typeahead.current;
    t.text = now - t.at > 600 ? ch : t.text + ch;
    t.at = now;
    const q = t.text.toLowerCase();
    const from = open && active >= 0 ? active : selectedIdx;
    // A repeated single character cycles through matches; otherwise search from the current one.
    const startAt = q.length === 1 ? from + 1 : Math.max(from, 0);
    for (let k = 0; k < list.length; k++) {
      const i = (startAt + k) % list.length;
      const o = list[i];
      if (!o.disabled && (o.label.toLowerCase().startsWith(q) || (o.code ?? "").toLowerCase().startsWith(q))) {
        if (open) setActive(i);
        else openList(i);
        return;
      }
    }
  }

  function onKeyDown(e: KeyboardEvent<HTMLButtonElement>) {
    if (disabled) return;
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        if (!open) openList();
        else move(1);
        return;
      case "ArrowUp":
        e.preventDefault();
        if (!open) openList();
        else move(-1);
        return;
      case "Home":
        if (open) {
          e.preventDefault();
          setActive(firstEnabled(0, 1));
        }
        return;
      case "End":
        if (open) {
          e.preventDefault();
          setActive(firstEnabled(list.length - 1, -1));
        }
        return;
      case "PageDown":
      case "PageUp":
        if (open) {
          e.preventDefault();
          const step = e.key === "PageDown" ? 8 : -8;
          const target = Math.max(0, Math.min(list.length - 1, (active < 0 ? 0 : active) + step));
          const i = firstEnabled(target, step > 0 ? -1 : 1);
          if (i >= 0) setActive(i);
        }
        return;
      case "Enter":
      case " ":
        e.preventDefault();
        if (e.key === " " && open && Date.now() - typeahead.current.at < 600) {
          typeAhead(" ");
          return;
        }
        if (!open) openList();
        else if (active >= 0) choose(active);
        else setOpen(false);
        return;
      case "Escape":
        if (open) {
          e.preventDefault();
          e.stopPropagation();
          setOpen(false);
        }
        return;
      case "Tab":
        if (open) setOpen(false);
        return;
      default:
        if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
          e.preventDefault();
          typeAhead(e.key);
        }
    }
  }

  function onTriggerClick(e: MouseEvent<HTMLButtonElement>) {
    // Keyboard activation is handled in onKeyDown; ignore synthetic clicks (detail === 0).
    if (e.detail === 0) return;
    if (open) setOpen(false);
    else openList();
  }

  const showClear = clearable && !disabled && value !== "";
  const isPlaceholder = !selected || (selected.value === "" && allLabel === undefined);
  const style: CSSProperties | undefined = width !== undefined ? { width } : undefined;

  return (
    <div ref={wrapRef} className={`select select-${size} ${className}`} style={style}>
      <button
        ref={triggerRef}
        id={baseId}
        type="button"
        role="combobox"
        className={`select-trigger${showClear ? " has-clear" : ""}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listId}
        aria-activedescendant={open && active >= 0 ? optId(active) : undefined}
        aria-label={aria["aria-label"]}
        aria-labelledby={aria["aria-labelledby"]}
        disabled={disabled}
        onClick={onTriggerClick}
        onKeyDown={onKeyDown}
      >
        <span className={`select-value${isPlaceholder ? " is-placeholder" : ""}`}>
          {isPlaceholder ? (
            <span className="select-text">{placeholder}</span>
          ) : (
            <>
              {selected.dot && <span className={`dot dot-${selected.dot}`} />}
              {selected.code && <span className="option-code">{selected.code}</span>}
              <span className="select-text">{selected.label}</span>
            </>
          )}
        </span>
        <ChevronDown />
      </button>
      {showClear && (
        <button
          type="button"
          className="select-clear"
          aria-label="Сбросить"
          title="Сбросить"
          onClick={() => {
            onChange("");
            triggerRef.current?.focus();
          }}
        >
          ×
        </button>
      )}
      {open && (
        <div
          ref={popRef}
          className={`popover${place.up ? " is-up" : ""}${place.end ? " is-end" : ""}`}
          onMouseDown={(e) => e.preventDefault()}
        >
          <ul ref={listRef} id={listId} role="listbox" className="listbox" aria-labelledby={baseId}>
            {list.map((o, i) => (
              <li
                key={`${o.value}-${i}`}
                id={optId(i)}
                role="option"
                aria-selected={o.value === value}
                aria-disabled={o.disabled || undefined}
                className={`option${i === active ? " is-active" : ""}`}
                onMouseMove={() => i !== active && !o.disabled && setActive(i)}
                onClick={() => choose(i)}
              >
                <OptionBody o={o} />
                <Check className={`option-check${o.value === value ? "" : " is-hidden"}`} />
              </li>
            ))}
            {list.length === 0 && <li className="option-empty">Нет вариантов</li>}
          </ul>
        </div>
      )}
    </div>
  );
}
