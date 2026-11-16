# MarkTrace UI kit (`src/ui`)

In-house components and the design rules every page follows. Import everything from the barrel:

```tsx
import { Select, Combobox, DateRangePicker, Field, Toolbar, PageHeader, useQueryFilters } from "../ui";
```

Styles live in `src/styles.css` (one file, tokens on `:root`). Status/decision/reason badges stay in
`src/components.tsx` (`StatusBadge`, `ReasonBadge`, `DecisionBadge`, `SessionStatusBadge`, `StateBadge`,
`CodeLink`, `ErrorBox`, `Loading`, `Pager`). The Codes page (`pages/CodesPage.tsx`) is the reference
implementation of a filtered list page.

## Look and feel — rules

Sober internal back-office tool: dense, calm, one accent.

Do:
- System font, 14px base; headings 20px (page) / 16px (card), semibold. Codes, GTINs, op_id,
  item_code, reason codes in monospace (`.mono`, 13px).
- Numbers are tabular (global); numeric table columns get `className="num"` (right aligned).
- 1px borders (`--c-border`), radius 4–6px, spacing 4/8/12/16/24px.
- One accent (`--c-accent`, muted blue) for primary buttons, links, focus, active nav/segment.
- Status = small dot + text (`.status st-sold`), or a quiet tinted badge (`.badge badge-ok`).
  Red only for errors and rejections.
- Labels above inputs (`Field`), controls 32px high (28px for `sm`).
- Filters apply on change (text after 300ms), are stored in the URL, and there is a ghost
  "Сбросить" button; no "Применить" button.
- Empty table → one muted sentence in a full-width row (`EmptyRow`).

Don't:
- No gradients, glass/blur, emoji, decorative icons or illustrations, purple/neon accents.
- No big drop shadows (shadow only on popovers), no pill shapes, no oversized rounded cards,
  no coloured left/top borders on cards, no ALL-CAPS headings with letter-spacing.
- No animations longer than ~120ms, no marketing copy, no "✓/✗/→/←" glyphs as decoration
  (write "Назад"/"Далее", "Достигнута"/"Не достигнута").
- No native `<select>`; no `datetime-local` for filter periods (use `DateRangePicker`).
- No new colours inline in pages — use the tokens/classes below.

## Tokens (`:root` in styles.css)

| Token | Value | Use |
|---|---|---|
| `--c-text` / `--c-text-2` / `--c-text-3` | #1d2125 / #5e6670 / #8a929b | text, secondary, placeholder/faint |
| `--c-border` / `--c-border-strong` | #dfe2e6 / #c9ced4 | card/table borders / input borders |
| `--c-bg` / `--c-surface` / `--c-surface-2` | #f6f7f8 / #fff / #f9fafb | page / cards / table header |
| `--c-hover` / `--c-active` | #f2f4f6 / #e9ecef | hover / pressed |
| `--c-accent` (+`-hover`, `-soft`, `-border`) | #2457c5 | the only accent |
| `--c-focus` | accent at 30% | focus ring |
| `--c-ok` / `--c-bad` / `--c-warn` (+`-soft`, `-border`) | desaturated green / red / amber | semantic |
| `--st-issued` … `--st-disposed` | blue-grey, teal, green, amber, grey | code status dots |
| `--s-1` … `--s-6` | 4, 8, 12, 16, 24, 32px | spacing |
| `--r-sm` / `--r-md` | 4px / 6px | radius |
| `--h-control` / `--h-control-sm` | 32px / 28px | control height |
| `--fs-xs` / `--fs-sm` / `--fs-base` / `--fs-h2` / `--fs-h1` / `--fs-figure` | 12/13/14/16/20/24px | type scale |
| `--font-sans` / `--font-mono` | system / ui-monospace | |
| `--topbar-h` | 48px | sticky offset for table headers |

## CSS classes for pages

Layout: `.page-head` (use `PageHeader`), `.card` (flat, bordered, 16px padding), `.card-head`
(title + actions row inside a card), `.grid-2`, `.stack`, `.row`, `.row.wrap`, `.spacer`.
Tables: `.card.table-card > table.table` (edge-to-edge, sticky header under the top bar);
`.table-card.scroll-x` for a table that must scroll horizontally (sticky header then stops working);
`table.table.compact` for small inline tables. Cells: `td.num`, `td.code-cell` (mono, nowrap),
`td.truncate` + `style={{ maxWidth }}` + `title=` for long text, `td.nowrap`, `td.msg`, `td.raw`.
`.pager` (use `Pager`). `.result-bar` = small muted line above a table ("Найдено 512 · …").
Text: `.mono`, `.muted`, `.faint`, `.small`, `.xsmall`, `.ok`, `.bad`, `.warn`, `.num`, `.truncate`, `.nowrap`.
Buttons: `.btn` (+ `.btn-primary`, `.btn-secondary`, `.btn-ghost`, `.btn-danger`, `.btn-sm`,
`.btn-block`, `.btn-link`) — also on `<a>`/`<Link>`; or `btnClass("primary","sm")`.
Forms: `Field`, or legacy `.form label` (label wrapping text + control), `.form-stack`, `.form-actions`,
`.hint`, `details > summary`.
Alerts: `.alert.alert-error` (use `ErrorBox`), `.alert-info`, `.alert-ok`, `.alert-warn`.
Status: `.status.st-{issued|introduced|sold|returned|disposed}` (dot + label),
`.status.ss-{completed|running|failed|structure_rejected}`, `.status.state-{open|closed}`;
`.dot.dot-{status|ok|bad|warn|accent|neutral|open|closed}` for a bare dot (`open`/`closed` match `StateBadge`).
Badges (tinted, 4px): `.badge` + `.badge-ok|bad|warn|accent|neutral`, `.d-{accepted|rejected|duplicate}`,
`.badge.reason.r-{format|logic|snapshot|mock|auth}` (use `ReasonBadge`).
Other: `.kv` (use `DescriptionList`), `.json`, `.timeline` + `.tl-time`, `.result.result-{accepted|rejected|duplicate}`,
`.tabs > .tab.is-active`, `.segmented > .segment.is-active` (use `SegmentedStrip`),
`.chip(.active)` (legacy reason counters), `.stat-grid > Stat`, `.meter`, `.compare`, `.diff`,
`.empty-state` (use `EmptyState`).

## Components

### `Select`
Button trigger + floating listbox (flips up when there is no room below, aligns right near the
viewport edge). Keyboard: ↑/↓ open and move, Home/End, PageUp/PageDown, Enter/Space choose, Esc
closes, Tab closes, type-ahead by first letters. Click outside closes. ARIA combobox/listbox/option
with `aria-activedescendant`.

```ts
interface SelectOption { value: string; label: string; dot?: string; code?: string; hint?: string; disabled?: boolean }
interface SelectProps {
  value: string; onChange(value: string): void; options: SelectOption[];
  allLabel?: string;        // adds first option value "" ("Все статусы")
  placeholder?: string;     // muted text when nothing selected and no allLabel
  clearable?: boolean;      // × resets to ""
  disabled?: boolean; id?: string; size?: "sm" | "md"; width?: number | string; className?: string;
  "aria-label"?: string; "aria-labelledby"?: string;
}
```
```tsx
<Field label="Источник" htmlFor="v-source" width={180}>
  <Select id="v-source" value={f.values.source} onChange={(v) => f.set({ source: v })}
    allLabel="Все источники" clearable
    options={SOURCES.map((s) => ({ value: s, label: SOURCE_LABEL[s] }))} />
</Field>
```
`dot` takes a code status (`"sold"`) or `ok|bad|warn|accent|neutral`. `code` renders a monospace
prefix (e.g. reason code), `hint` a muted suffix (e.g. a count).

### `Combobox`
Searchable select for long lists (items ~100, reasons). Same option shape; the popover has a
search box that filters by `code` and `label` (substring, case-insensitive) and highlights the
match; "Ничего не найдено" when empty. Keyboard: typing on the closed trigger opens it with that
letter, ↑/↓/PageUp/PageDown, Enter choose, Esc close.

Props: everything `Select` takes, plus:
```ts
{
  searchPlaceholder?: string;   // "Поиск"
  emptyText?: string;           // "Ничего не найдено"
  popoverWidth?: number | string; // wider dropdown than the trigger, e.g. 440
  triggerShows?: "both" | "code" | "label"; // closed trigger text for coded options; "code" for reasons
}
```
```tsx
// items
<Combobox id="codes-item" value={f.values.item_code} onChange={(v) => f.set({ item_code: v })}
  options={items.map((it) => ({ value: it.item_code, code: it.item_code, label: it.name }))}
  allLabel="Все товары" searchPlaceholder="Код или наименование" popoverWidth={440} clearable />
// reasons
<Combobox value={f.values.reason} onChange={(v) => f.set({ reason: v })} allLabel="Все причины" clearable
  options={REASONS.map((r) => ({ value: r, code: r, label: REASON_LABEL[r] }))} popoverWidth={420} />
```

### `DateRangePicker`
One trigger ("01.09.2026 — 15.10.2026", "с 01.09.2026", "Любой период") and a popover with presets
(Сегодня, 7 дней, 30 дней, the two previous calendar months, Сбросить) and a two-month calendar
(react-day-picker 9, Russian locale, Monday first). First click sets the start, second click the
end and closes; closing after one click applies that single day. Values are `"YYYY-MM-DD"` or `""`,
`to` is inclusive.

```ts
interface DateRangePickerProps {
  from: string; to: string; onChange(r: { from: string; to: string }): void;
  placeholder?: string; presets?: { label: string; range(): { from: string; to: string } }[];
  clearable?: boolean /* default true */; disabled?: boolean; id?: string;
  size?: "sm" | "md"; width?: number | string; className?: string;
}
```
Keep the days in the URL; convert for the API with `dayBoundsToApi(from, to)` → local-day RFC 3339
bounds (`2026-09-01T00:00:00+03:00` … `2026-09-15T23:59:59+03:00`), so the filter matches the
local times shown in tables. Use the same converted values for CSV export links.

```tsx
<Field label="Период" htmlFor="v-period" width={250}>
  <DateRangePicker id="v-period" from={f.values.from} to={f.values.to}
    onChange={(r) => f.set({ from: r.from, to: r.to })} />
</Field>
const period = dayBoundsToApi(f.values.from, f.values.to); // → { from, to } for api.*()
```
Helpers: `parseYmd`, `toYmd`, `formatRange`, `defaultPresets`.

### `TextFilter`
Debounced text input for auto-applied filters: emits 300ms after typing stops, immediately on
Enter or when cleared; follows external value changes (reset, back/forward).
`{ value; onChange(v); placeholder?; mono?; id?; width?; delay?; "aria-label"? }`
```tsx
<TextFilter id="codes-q" mono value={f.values.q} onChange={(v) => f.set({ q: v })} placeholder="Часть кода" />
```

### `useQueryFilters(keys)`
URL-synced filter state (history `replace`, unknown keys preserved, page offset reset on change).
Returns `{ values: Record<K,string>, set(patch), reset(), active, offset, setOffset(n) }`.
```tsx
const f = useQueryFilters(["reason", "source", "state", "item_code", "actor", "from", "to", "q"] as const);
useEffect(() => { api.violations({ ...apiFilters, limit, offset: f.offset }) … }, [apiFilters, f.offset]);
<Pager total={data.total} limit={LIMIT} offset={f.offset} onChange={f.setOffset} />
```

### `Field`
Label above a control, optional hint or error below.
`{ label; htmlFor?; hint?; error?; width?; grow?; className?; children }` — `htmlFor` must equal
the control's `id`; `grow` makes it take the rest of a toolbar row.
```tsx
<Field label="Код маркировки" htmlFor="op-code" hint={`${code.length} / 24 символа`}>
  <input id="op-code" className="mono" value={code} onChange={…} />
</Field>
```

### `Toolbar`
Filter row card: fields on the left, optional `end` slot and a ghost "Сбросить" on the right.
`{ children; onReset?(); resetDisabled?; end?; className? }` — inside a `.table-card` (after `.card-head`) it renders as a flat inset band.
```tsx
<Toolbar onReset={f.reset} resetDisabled={!f.active}> <Field …/> <Field …/> </Toolbar>
```

### `PageHeader`
`{ title; subtitle?; actions?; back?: { to; label } }` — 20px title, muted one-line subtitle,
actions on the right, optional small back link above the title ("К списку кодов").
```tsx
<PageHeader title="Расхождения" subtitle="…" actions={<a className="btn" href={csvUrl} download>Экспорт CSV</a>} />
```

### `SegmentedStrip`
Counter strip that is also a filter. `{ segments: { value; label; count?; dot?; title?; mono? }[]; value; onChange(v); "aria-label"? }`
```tsx
<SegmentedStrip aria-label="Статус" value={f.values.status} onChange={(v) => f.set({ status: v })}
  segments={[{ value: "", label: "Все", count: total },
             ...STATUSES.map((s) => ({ value: s, label: STATUS_LABEL[s], dot: s, count: data?.counters[s] ?? null }))]} />
```
`count: null` renders "—" (loading). For reasons use `label: code, mono: true, title: REASON_LABEL[code]`.

### `EmptyState` / `EmptyRow`
`<EmptyState title="Сеансов пока нет">Загрузите CSV-файлы.</EmptyState>`;
in tables `<EmptyRow colSpan={5}>Нет кодов по заданным фильтрам.</EmptyRow>`.

### `DescriptionList`
`dl.kv` from items; falsy items are skipped, empty values render "—".
`{ items: ({ term; value; mono?; title? } | null | false | undefined)[]; className? }` (`className="kv-2"` for 4 columns).
```tsx
<DescriptionList items={[
  { term: "Статус", value: <StatusBadge status={cur.status} /> },
  { term: "Товар", value: cur.item_code, mono: true },
  data.item && { term: "GTIN", value: data.item.gtin, mono: true },
]} />
```

### `Stat` / `Meter`
KPI figure without a coloured card: id, label, 24px value, "Цель: …" with a dot + "Достигнута" /
"Не достигнута" (or "нет данных" when `ok` is null), optional meter and a muted footnote.
Put several in `<div className="stat-grid">` (one bordered block with dividers).
`Stat { id?; label; value; target?; ok?: boolean | null; meter?: { percent; target }; children? }`,
`Meter { percent; target; ok? }`.
```tsx
<div className="stat-grid">
  <Stat id="K1" label="Доля корректных операций" value={fmtPct(k1.percent)} target="≥ 99%"
    ok={k1.percent >= 99} meter={{ percent: k1.percent, target: 99 }}>
    принято {k1.accepted} · отклонено {k1.rejected}
  </Stat>
</div>
```

### `Button` / `btnClass`
`<Button variant="primary|secondary|ghost|danger" size="sm|md" block?>` (defaults to
`type="button"`; pass `type="submit"` in forms). `btnClass(variant, size)` for `<a>`/`<Link>`.

### Badges (`src/components.tsx`)
`StatusBadge` (dot + label), `SessionStatusBadge` (dot + label), `StateBadge` (open/closed dot + label),
`DecisionBadge` (tinted badge), `ReasonBadge` (mono tag, Russian meaning in `title`, group marker),
`Pager` ("1–50 из 512 · Стр. 1 из 11 · Назад · Далее"), `ErrorBox` (`role="alert"`), `Loading`.

## Notes
- Popovers are `position: absolute` inside their wrapper: do not put a Select inside an element
  with `overflow: hidden/auto` (e.g. `.table-card.scroll-x`) or it will be clipped.
- The table header is sticky at `top: var(--topbar-h)`; that only works when no ancestor scrolls
  horizontally (`.table-card` default). Below 900px the cards scroll horizontally instead.
- Operation form "Время события" stays a native `datetime-local` (restyled) and must still send
  RFC 3339 with the browser offset via `localToRfc3339`, or nothing for server time.
