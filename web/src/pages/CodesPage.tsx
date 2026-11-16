import { useEffect, useMemo, useState } from "react";
import { api, qs, STATUSES, type CodesResponse, type Item } from "../api";
import { CodeLink, ErrorBox, Loading, Pager, StatusBadge } from "../components";
import { fmtTs, STATUS_LABEL } from "../labels";
import {
  btnClass,
  Combobox,
  dayBoundsToApi,
  DateRangePicker,
  EmptyRow,
  Field,
  PageHeader,
  SegmentedStrip,
  TextFilter,
  Toolbar,
  useQueryFilters,
  type SelectOption,
} from "../ui";
import "./codes.css";

const LIMIT = 50;
const FILTER_KEYS = ["status", "item_code", "actor", "from", "to", "q"] as const;

function fmtMs(v: number): string {
  if (!Number.isFinite(v)) return "—";
  return `${Number.isInteger(v) ? v : v.toFixed(2)} мс`;
}

export default function CodesPage() {
  const f = useQueryFilters(FILTER_KEYS);
  const { values, offset } = f;
  const [items, setItems] = useState<Item[]>([]);
  const [data, setData] = useState<CodesResponse | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(false);

  // Filters as sent to the API: the period is a pair of local calendar days.
  const filters = useMemo(() => {
    const period = dayBoundsToApi(values.from, values.to);
    return {
      status: values.status,
      item_code: values.item_code,
      actor: values.actor,
      from: period.from,
      to: period.to,
      q: values.q,
    };
  }, [values]);

  useEffect(() => {
    api.items().then(setItems).catch(() => setItems([]));
  }, []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .codes({ ...filters, limit: LIMIT, offset })
      .then((d) => !cancelled && setData(d))
      .catch((e) => !cancelled && setError(e))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [filters, offset]);

  const itemOptions: SelectOption[] = useMemo(
    () => items.map((it) => ({ value: it.item_code, code: it.item_code, label: it.name })),
    [items],
  );
  const itemName = useMemo(() => new Map(items.map((it) => [it.item_code, it.name])), [items]);

  const counterTotal = data ? STATUSES.reduce((a, s) => a + (data.counters[s] ?? 0), 0) : null;
  const segments = [
    { value: "", label: "Все", count: counterTotal },
    ...STATUSES.map((s) => ({ value: s, label: STATUS_LABEL[s], dot: s, count: data ? data.counters[s] ?? 0 : null })),
  ];

  return (
    <div>
      <PageHeader
        title="Коды маркировки"
        subtitle="Текущий статус каждого кода по принятым операциям"
        actions={
          <>
            <a
              className={btnClass("secondary")}
              href={`/api/codes/export.csv${qs(filters)}`}
              title="Коды с текущими фильтрами: code, item_code, status, last_ts"
              download
            >
              Экспорт CSV
            </a>
            <a
              className={btnClass("ghost")}
              href="/api/status-summary.csv"
              title="Количество кодов по статусам (все коды): status, count"
              download
            >
              Сводка по статусам, CSV
            </a>
          </>
        }
      />

      <div className="codes-strip">
        <SegmentedStrip
          aria-label="Фильтр по статусу"
          segments={segments}
          value={values.status}
          onChange={(v) => f.set({ status: v })}
        />
      </div>

      <Toolbar onReset={f.reset} resetDisabled={!f.active}>
        <Field label="Товар" htmlFor="codes-item" width={300}>
          <Combobox
            id="codes-item"
            value={values.item_code}
            onChange={(v) => f.set({ item_code: v })}
            options={itemOptions}
            allLabel="Все товары"
            searchPlaceholder="Код или наименование"
            popoverWidth={440}
            clearable
          />
        </Field>
        <Field label="Период последнего события" htmlFor="codes-period" width={250}>
          <DateRangePicker
            id="codes-period"
            from={values.from}
            to={values.to}
            onChange={(r) => f.set({ from: r.from, to: r.to })}
          />
        </Field>
        <Field label="Исполнитель" htmlFor="codes-actor" width={170}>
          <TextFilter id="codes-actor" value={values.actor} onChange={(v) => f.set({ actor: v })} placeholder="Логин" />
        </Field>
        <Field label="Поиск по коду" htmlFor="codes-q" grow>
          <TextFilter id="codes-q" mono value={values.q} onChange={(v) => f.set({ q: v })} placeholder="Часть кода" />
        </Field>
      </Toolbar>

      <ErrorBox error={error} />
      {data && (
        <div className="result-bar" aria-live="polite">
          <span>
            Найдено <strong>{data.total}</strong>
          </span>
          <span className="codes-result-sep" aria-hidden="true">
            ·
          </span>
          <span title="Время построения отчёта на сервере (K4, цель не более 2 с)">
            отчёт построен за{" "}
            <strong className={data.elapsed_ms <= 2000 ? undefined : "bad"}>{fmtMs(data.elapsed_ms)}</strong>
          </span>
          {loading && <span className="faint">Обновление…</span>}
        </div>
      )}
      {!data && loading && <Loading />}
      {data && (
        <div className={`card table-card codes-table${loading ? " is-loading" : ""}`} aria-busy={loading}>
          <table className="table">
            <thead>
              <tr>
                <th style={{ width: 230 }}>Код</th>
                <th>Товар</th>
                <th style={{ width: 140 }}>Статус</th>
                <th style={{ width: 170 }}>Последнее событие</th>
                <th style={{ width: 150 }}>Исполнитель</th>
              </tr>
            </thead>
            <tbody>
              {data.rows.length === 0 && (
                <EmptyRow colSpan={5}>
                  {f.active ? "Нет кодов по заданным фильтрам." : "Кодов пока нет. Загрузите данные на странице «Импорт»."}
                </EmptyRow>
              )}
              {data.rows.map((r) => {
                const name = itemName.get(r.item_code);
                return (
                  <tr key={r.code}>
                    <td className="code-cell">
                      <CodeLink code={r.code} />
                    </td>
                    <td className="truncate codes-item" title={name ? `${r.item_code} — ${name}` : r.item_code}>
                      <span className="mono">{r.item_code}</span>
                      {name && <span className="muted">{name}</span>}
                    </td>
                    <td>
                      <StatusBadge status={r.status} />
                    </td>
                    <td className="nowrap" title={r.last_ts}>
                      {fmtTs(r.last_ts)}
                    </td>
                    <td className="truncate" style={{ maxWidth: 150 }} title={r.last_actor}>
                      {r.last_actor || <span className="faint">—</span>}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          <Pager total={data.total} limit={LIMIT} offset={offset} onChange={f.setOffset} />
        </div>
      )}
    </div>
  );
}
