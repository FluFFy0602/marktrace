import { useCallback, useEffect, useMemo, useState } from "react";
import { api, qs, REASONS, SOURCES, type Item, type ViolationsResponse } from "../api";
import { ErrorBox, Loading, Pager, reasonGroup } from "../components";
import { REASON_LABEL, SOURCE_LABEL } from "../labels";
import {
  Combobox,
  dayBoundsToApi,
  DateRangePicker,
  Field,
  PageHeader,
  Select,
  TextFilter,
  Toolbar,
  useQueryFilters,
  type SelectOption,
} from "../ui";
import ViolationsTable from "../ViolationsTable";
import "./ops.css";

const LIMIT = 50;
const FILTER_KEYS = ["reason", "source", "state", "item_code", "actor", "from", "to", "q"] as const;

const STATE_OPTIONS: SelectOption[] = [
  { value: "open", label: "Открытые", dot: "open" },
  { value: "closed", label: "Закрытые", dot: "closed" },
];
const SOURCE_OPTIONS: SelectOption[] = SOURCES.map((s) => ({ value: s, label: SOURCE_LABEL[s] ?? s }));

/** Counts by reason as a compact clickable grid; clicking an active reason clears the filter. */
function ReasonCounts({
  counts,
  active,
  onPick,
}: {
  counts: Record<string, number>;
  active: string;
  onPick: (reason: string) => void;
}) {
  const entries = Object.entries(counts)
    .filter(([r, c]) => c > 0 || r === active)
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  if (active && !entries.some(([r]) => r === active)) entries.push([active, 0]);
  const total = Object.values(counts).reduce((a, c) => a + c, 0);
  return (
    <div className="vio-reasons" role="group" aria-label="Расхождения по причинам">
      <button
        type="button"
        className={`vio-reason is-all${active === "" ? " is-active" : ""}`}
        aria-pressed={active === ""}
        onClick={() => onPick("")}
      >
        <span className="vio-reason-mark" aria-hidden />
        <span className="vio-reason-code">Все причины</span>
        <span className="vio-reason-count">{total}</span>
        <span className="vio-reason-label">по остальным фильтрам</span>
      </button>
      {entries.map(([r, c]) => (
        <button
          key={r}
          type="button"
          className={`vio-reason${active === r ? " is-active" : ""}`}
          aria-pressed={active === r}
          title={`${r} — ${REASON_LABEL[r] ?? r}`}
          onClick={() => onPick(active === r ? "" : r)}
        >
          <span className={`vio-reason-mark r-${reasonGroup(r)}`} aria-hidden />
          <span className="vio-reason-code">{r}</span>
          <span className="vio-reason-count">{c}</span>
          <span className="vio-reason-label">{REASON_LABEL[r] ?? "—"}</span>
        </button>
      ))}
    </div>
  );
}

export default function ViolationsPage() {
  const f = useQueryFilters(FILTER_KEYS);
  const { values, offset } = f;
  const [items, setItems] = useState<Item[]>([]);
  const [data, setData] = useState<ViolationsResponse | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(false);

  // Filters as sent to the API and the CSV export: the period is a pair of local calendar days.
  const filters = useMemo(() => {
    const period = dayBoundsToApi(values.from, values.to);
    return {
      reason: values.reason,
      source: values.source,
      state: values.state,
      item_code: values.item_code,
      actor: values.actor,
      from: period.from,
      to: period.to,
      q: values.q,
    };
  }, [values]);

  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    api.items().then(setItems).catch(() => setItems([]));
  }, []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .violations({ ...filters, limit: LIMIT, offset })
      .then((d) => !cancelled && setData(d))
      .catch((e) => !cancelled && setError(e))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [filters, offset, tick]);

  const reasonOptions: SelectOption[] = useMemo(
    () =>
      REASONS.map((r) => {
        const c = data?.counts[r];
        return { value: r, code: r, label: REASON_LABEL[r] ?? r, hint: c ? String(c) : undefined };
      }),
    [data],
  );
  const itemOptions: SelectOption[] = useMemo(
    () => items.map((it) => ({ value: it.item_code, code: it.item_code, label: it.name })),
    [items],
  );

  return (
    <div>
      <PageHeader
        title="Расхождения"
        subtitle="Отклонённые строки импорта и операции, расхождения со снимком. Фильтр по дате — по времени создания."
        actions={
          <a className="btn" href={`/api/violations/export.csv${qs(filters)}`} download>
            Экспорт CSV
          </a>
        }
      />

      {data && <ReasonCounts counts={data.counts} active={values.reason} onPick={(r) => f.set({ reason: r })} />}

      {/* Fixed 4-column grid (ops.css .vio-toolbar) so both rows line up at any width. */}
      <Toolbar onReset={f.reset} resetDisabled={!f.active} className="vio-toolbar">
        <Field label="Причина" htmlFor="vio-reason">
          <Combobox
            id="vio-reason"
            value={values.reason}
            onChange={(v) => f.set({ reason: v })}
            options={reasonOptions}
            allLabel="Все причины"
            searchPlaceholder="Код или описание"
            popoverWidth={420}
            triggerShows="code"
            clearable
          />
        </Field>
        <Field label="Товар" htmlFor="vio-item">
          <Combobox
            id="vio-item"
            value={values.item_code}
            onChange={(v) => f.set({ item_code: v })}
            options={itemOptions}
            allLabel="Все товары"
            searchPlaceholder="Код или наименование"
            popoverWidth={520}
            clearable
          />
        </Field>
        <Field label="Создано" htmlFor="vio-period">
          <DateRangePicker
            id="vio-period"
            from={values.from}
            to={values.to}
            onChange={(r) => f.set({ from: r.from, to: r.to })}
          />
        </Field>
        <Field label="Состояние" htmlFor="vio-state">
          <Select
            id="vio-state"
            value={values.state}
            onChange={(v) => f.set({ state: v })}
            options={STATE_OPTIONS}
            allLabel="Все"
            clearable
          />
        </Field>
        <Field label="Источник" htmlFor="vio-source">
          <Select
            id="vio-source"
            value={values.source}
            onChange={(v) => f.set({ source: v })}
            options={SOURCE_OPTIONS}
            allLabel="Все источники"
            clearable
          />
        </Field>
        <Field label="Исполнитель" htmlFor="vio-actor">
          <TextFilter id="vio-actor" value={values.actor} onChange={(v) => f.set({ actor: v })} placeholder="Логин" />
        </Field>
        <Field label="Поиск" htmlFor="vio-q" className="vio-span-2">
          <TextFilter
            id="vio-q"
            value={values.q}
            onChange={(v) => f.set({ q: v })}
            placeholder="Код, ref или текст сообщения"
          />
        </Field>
      </Toolbar>

      <ErrorBox error={error} />
      {data && (
        <div className="result-bar">
          <span>
            Найдено <strong>{data.total}</strong>
          </span>
          {loading && <span className="faint">Обновление…</span>}
        </div>
      )}
      {!data && loading && <Loading />}
      {data && (
        <div className="card table-card">
          <ViolationsTable rows={data.rows} onChanged={reload} />
          <Pager total={data.total} limit={LIMIT} offset={offset} onChange={f.setOffset} />
        </div>
      )}
    </div>
  );
}
