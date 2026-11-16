import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
} from "react";
import { Check, ChevronDown } from "./icons";
import { withAll, type SelectOption } from "./Select";
import { scrollOptionIntoView, useClickOutside, usePlacement, useStableId } from "./usePopover";

export interface ComboboxProps {
  value: string;
  onChange: (value: string) => void;
  /** Options; `code` is shown in monospace before `label`, both are searched. */
  options: SelectOption[];
  /** Adds a first option with value "" (hidden while a search query is typed). */
  allLabel?: string;
  placeholder?: string;
  searchPlaceholder?: string;
  emptyText?: string;
  clearable?: boolean;
  disabled?: boolean;
  id?: string;
  size?: "sm" | "md";
  width?: number | string;
  /** Width of the dropdown when it should be wider than the trigger, e.g. 420. */
  popoverWidth?: number | string;
  /** What the closed trigger shows for an option with a `code`; the full text stays in its tooltip. */
  triggerShows?: "both" | "code" | "label";
  className?: string;
  "aria-label"?: string;
  "aria-labelledby"?: string;
}

/** Wraps the first case-insensitive occurrence of `q` in <mark>. */
export function highlight(text: string, q: string): ReactNode {
  if (!q) return text;
  const i = text.toLowerCase().indexOf(q.toLowerCase());
  if (i < 0) return text;
  return (
    <>
      {text.slice(0, i)}
      <mark>{text.slice(i, i + q.length)}</mark>
      {text.slice(i + q.length)}
    </>
  );
}

/** Searchable select: trigger button + popover with a search input and a filtered listbox. */
export function Combobox({
  value,
  onChange,
  options,
  allLabel,
  placeholder = "Не выбрано",
  searchPlaceholder = "Поиск",
  emptyText = "Ничего не найдено",
  clearable = false,
  disabled = false,
  id,
  size = "md",
  width,
  popoverWidth,
  triggerShows = "both",
  className = "",
  ...aria
}: ComboboxProps) {
  const baseId = useStableId("cbx", id);
  const listId = `${baseId}-list`;
  const all = withAll(options, allLabel);
  const selected = all.find((o) => o.value === value);
  // Longest monospace code: the code column gets this width so labels line up.
  const codeCh = options.reduce((m, o) => Math.max(m, o.code?.length ?? 0), 0);

  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(-1);
  const wrapRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const place = usePlacement(triggerRef, popRef, open);

  const q = query.trim();
  const filtered = useMemo(() => {
    if (!q) return all;
    const lq = q.toLowerCase();
    return options.filter(
      (o) => o.label.toLowerCase().includes(lq) || (o.code ?? "").toLowerCase().includes(lq) || o.value.toLowerCase() === lq,
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, options, allLabel]);

  useClickOutside([wrapRef], () => setOpen(false), open);

  const optId = (i: number) => `${baseId}-opt-${i}`;
  useEffect(() => {
    if (open && active >= 0) scrollOptionIntoView(listRef.current, optId(active));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, active]);

  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  function openList(initialQuery = "") {
    if (disabled) return;
    setQuery(initialQuery);
    if (initialQuery) setActive(0);
    else {
      const i = all.findIndex((o) => o.value === value);
      setActive(i >= 0 ? i : 0);
    }
    setOpen(true);
  }

  function close(refocus: boolean) {
    setOpen(false);
    setQuery("");
    if (refocus) triggerRef.current?.focus();
  }

  function choose(o: SelectOption | undefined) {
    if (!o || o.disabled) return;
    close(true);
    if (o.value !== value) onChange(o.value);
  }

  function onTriggerKeyDown(e: KeyboardEvent<HTMLButtonElement>) {
    if (disabled || open) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      openList();
    } else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
      e.preventDefault();
      openList(e.key);
    }
  }

  function onInputKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    const n = filtered.length;
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        if (n) setActive((a) => Math.min(n - 1, a + 1));
        return;
      case "ArrowUp":
        e.preventDefault();
        if (n) setActive((a) => Math.max(0, a - 1));
        return;
      case "PageDown":
        e.preventDefault();
        if (n) setActive((a) => Math.min(n - 1, a + 8));
        return;
      case "PageUp":
        e.preventDefault();
        if (n) setActive((a) => Math.max(0, a - 8));
        return;
      case "Home":
        if (e.ctrlKey || !query) {
          e.preventDefault();
          setActive(0);
        }
        return;
      case "End":
        if (e.ctrlKey || !query) {
          e.preventDefault();
          setActive(n - 1);
        }
        return;
      case "Enter":
        e.preventDefault();
        if (active >= 0 && active < n) choose(filtered[active]);
        return;
      case "Escape":
        e.preventDefault();
        e.stopPropagation();
        close(true);
        return;
      case "Tab":
        close(false);
        return;
    }
  }

  function onTriggerClick(e: MouseEvent<HTMLButtonElement>) {
    if (e.detail === 0) return;
    if (open) close(false);
    else openList();
  }

  const showClear = clearable && !disabled && value !== "";
  const isPlaceholder = !selected || (selected.value === "" && allLabel === undefined);
  const style: CSSProperties | undefined = width !== undefined ? { width } : undefined;
  const popStyle: CSSProperties | undefined = popoverWidth !== undefined ? { width: popoverWidth } : undefined;

  return (
    <div ref={wrapRef} className={`combobox select-${size} ${className}`} style={style}>
      <button
        ref={triggerRef}
        id={baseId}
        type="button"
        className={`select-trigger${showClear ? " has-clear" : ""}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={aria["aria-label"]}
        aria-labelledby={aria["aria-labelledby"]}
        disabled={disabled}
        onClick={onTriggerClick}
        onKeyDown={onTriggerKeyDown}
        title={selected && !isPlaceholder ? [selected.code, selected.label].filter(Boolean).join(" — ") : undefined}
      >
        <span className={`select-value${isPlaceholder ? " is-placeholder" : ""}`}>
          {isPlaceholder ? (
            <span className="select-text">{placeholder}</span>
          ) : (
            <>
              {selected.dot && <span className={`dot dot-${selected.dot}`} />}
              {selected.code && triggerShows === "code" ? (
                <span className="select-text mono">{selected.code}</span>
              ) : (
                <>
                  {selected.code && triggerShows !== "label" && <span className="option-code">{selected.code}</span>}
                  <span className="select-text">{selected.label}</span>
                </>
              )}
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
          style={popStyle}
        >
          <div className="combobox-search">
            <input
              ref={inputRef}
              type="text"
              role="combobox"
              aria-autocomplete="list"
              aria-expanded="true"
              aria-controls={listId}
              aria-activedescendant={active >= 0 && active < filtered.length ? optId(active) : undefined}
              aria-label={searchPlaceholder}
              placeholder={searchPlaceholder}
              autoComplete="off"
              spellCheck={false}
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setActive(0);
              }}
              onKeyDown={onInputKeyDown}
            />
          </div>
          <ul
            ref={listRef}
            id={listId}
            role="listbox"
            className="listbox"
            aria-labelledby={baseId}
            style={codeCh ? ({ "--code-w": `${codeCh}ch` } as CSSProperties) : undefined}
            onMouseDown={(e) => e.preventDefault()}
          >
            {filtered.map((o, i) => (
              <li
                key={`${o.value}-${i}`}
                id={optId(i)}
                role="option"
                aria-selected={o.value === value}
                aria-disabled={o.disabled || undefined}
                className={`option${i === active ? " is-active" : ""}`}
                onMouseMove={() => i !== active && setActive(i)}
                onClick={() => choose(o)}
                title={[o.code, o.label].filter(Boolean).join(" — ")}
              >
                {o.dot && <span className={`dot dot-${o.dot}`} />}
                {o.code && <span className="option-code">{highlight(o.code, q)}</span>}
                <span className="option-text">{highlight(o.label, q)}</span>
                {o.hint && <span className="option-hint">{o.hint}</span>}
                <Check className={`option-check${o.value === value ? "" : " is-hidden"}`} />
              </li>
            ))}
            {filtered.length === 0 && <li className="option-empty">{emptyText}</li>}
          </ul>
        </div>
      )}
    </div>
  );
}
