import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useParams } from "react-router-dom";
import { api, type CodeDetail, type CodeStatus } from "../api";
import { DecisionBadge, ErrorBox, Loading, ReasonBadge, StatusBadge, useBackTo } from "../components";
import { fmtTs, OP_LABEL, SOURCE_LABEL } from "../labels";
import { Button, DescriptionList, EmptyRow, PageHeader } from "../ui";
import ViolationsTable from "../ViolationsTable";
import "./codes.css";

/** Status a code reaches after an accepted operation (SPEC lifecycle); colours the timeline dot. */
const OP_RESULT: Record<string, CodeStatus> = {
  issue: "issued",
  introduce: "introduced",
  sell: "sold",
  return: "returned",
  dispose: "disposed",
};

/** "Ввод в оборот (introduce)" → "Ввод в оборот"; unknown types stay as sent. */
function opName(t: string | null | undefined): string {
  if (!t) return "—";
  const label = OP_LABEL[t as keyof typeof OP_LABEL];
  return label ? label.replace(/\s*\([^)]*\)\s*$/, "") : t;
}

function sameInstant(a: string | null | undefined, b: string | null | undefined): boolean {
  if (!a || !b) return false;
  const x = Date.parse(a);
  const y = Date.parse(b);
  return Number.isFinite(x) && x === y;
}

function plural(n: number, one: string, few: string, many: string): string {
  const m10 = n % 10;
  const m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return one;
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few;
  return many;
}

function CopyButton({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  const timer = useRef<number>();
  useEffect(() => () => window.clearTimeout(timer.current), []);
  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setDone(true);
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setDone(false), 1500);
    } catch {
      /* clipboard unavailable (insecure origin): nothing to do */
    }
  }
  return (
    <Button variant="ghost" size="sm" onClick={copy} title="Скопировать код в буфер обмена">
      {done ? "Скопировано" : "Копировать код"}
    </Button>
  );
}

export default function CodePage() {
  const { code = "" } = useParams();
  const [data, setData] = useState<CodeDetail | null>(null);
  const [error, setError] = useState<unknown>(null);

  const load = useCallback(() => {
    setError(null);
    api.code(code).then(setData).catch(setError);
  }, [code]);

  useEffect(() => {
    setData(null);
    load();
  }, [load]);

  const cur = data?.current ?? null;
  const reg = data?.registry ?? null;
  const item = data?.item ?? null;
  const itemCode = cur?.item_code || reg?.item_code || item?.item_code || "";

  // Return to the exact list the user came from (filters + page), if any.
  const fromList = useBackTo("/codes", "");
  const fromVio = useBackTo("/violations", "");
  const back = fromVio
    ? { to: fromVio, label: "К расхождениям" }
    : { to: fromList || "/codes", label: "К списку кодов" };

  const subtitle: ReactNode = data ? (
    itemCode ? (
      <span className="code-sub-item">
        <span className="mono">{itemCode}</span>
        {item?.name ?? <span className="faint">наименование не найдено</span>}
      </span>
    ) : (
      "Товар не определён"
    )
  ) : undefined;

  return (
    <div>
      <PageHeader
        back={back}
        title={
          <span className="code-title">
            <span className="code-title-value">{code}</span>
            {cur && <StatusBadge status={cur.status} />}
          </span>
        }
        subtitle={subtitle}
        actions={data ? <CopyButton text={code} /> : undefined}
      />
      <ErrorBox error={error} />
      {!data && !error && <Loading />}
      {data && <CodeBody data={data} onChanged={load} />}
    </div>
  );
}

function CodeBody({ data, onChanged }: { data: CodeDetail; onChanged: () => void }) {
  const cur = data.current;
  const reg = data.registry;
  const item = data.item;

  // Expected snapshot (from the registry) against the current projection.
  let snapshot: ReactNode = null;
  if (reg) {
    if (!reg.expected_status) {
      snapshot = <span className="faint">не задан (код создан формой)</span>;
    } else {
      const matches = !!cur && cur.status === reg.expected_status && sameInstant(cur.last_ts, reg.expected_ts);
      snapshot = (
        <>
          <StatusBadge status={reg.expected_status} />
          <span className="kv-sub"> · </span>
          <span title={reg.expected_ts ?? undefined}>{fmtTs(reg.expected_ts)}</span>
          {matches ? (
            <span className="badge badge-ok kv-note">Совпадает</span>
          ) : (
            <span
              className="badge badge-bad kv-note"
              title="Текущий статус или время последнего события отличаются от ожидаемого снимка"
            >
              Не совпадает
            </span>
          )}
        </>
      );
    }
  }

  const binding: ReactNode = reg ? (
    <>
      <span className="mono">{reg.item_code}</span>
      {cur && cur.item_code && cur.item_code !== reg.item_code && (
        <span className="badge badge-bad kv-note" title={`Код учтён под товаром ${cur.item_code}`}>
          Отличается от товара кода
        </span>
      )}
    </>
  ) : (
    <span className="faint">нет в реестре привязок</span>
  );

  const itemCode = cur?.item_code || reg?.item_code || item?.item_code || "";
  const ops = data.operations;
  const attempts = data.attempts;
  const decided = attempts.reduce<Record<string, number>>((acc, a) => {
    acc[a.decision] = (acc[a.decision] ?? 0) + 1;
    return acc;
  }, {});
  const openViolations = data.violations.filter((v) => v.state !== "closed").length;

  return (
    <>
      <div className="code-grid">
        <section className="card" aria-labelledby="code-summary-h">
          <div className="card-head">
            <h2 id="code-summary-h">Сведения</h2>
          </div>
          <DescriptionList
            className="code-kv"
            items={[
              {
                term: "Статус",
                value: cur ? <StatusBadge status={cur.status} /> : <span className="faint">нет принятых операций</span>,
              },
              {
                term: "Товар",
                value: itemCode ? (
                  <>
                    <span className="mono">{itemCode}</span>
                    {item?.name && <span className="kv-sub"> · {item.name}</span>}
                  </>
                ) : null,
              },
              { term: "GTIN", value: item?.gtin, mono: true },
              { term: "Привязка", value: binding },
              { term: "Ожидаемый снимок", value: snapshot ?? <span className="faint">—</span> },
              {
                term: "Последнее событие",
                value: cur ? (
                  <>
                    <span title={cur.last_ts}>{fmtTs(cur.last_ts)}</span>
                    {cur.last_actor && <span className="kv-sub"> · {cur.last_actor}</span>}
                  </>
                ) : null,
              },
            ]}
          />
        </section>

        <section className="card" aria-labelledby="code-ops-h">
          <div className="card-head">
            <h2 id="code-ops-h">Хронология операций</h2>
            <span className="card-head-meta">
              {ops.length} {plural(ops.length, "операция", "операции", "операций")}
            </span>
          </div>
          {ops.length === 0 ? (
            <p className="code-empty">Принятых операций по коду нет.</p>
          ) : (
            <ol className="op-tl">
              {ops.map((o) => (
                <li key={o.op_id} className={`dot-${OP_RESULT[o.op_type] ?? "neutral"}`}>
                  <div className="op-tl-head">
                    <span className="op-tl-type" title={o.op_type}>
                      {opName(o.op_type)}
                    </span>
                    <span className="op-tl-time" title={o.ts}>
                      {fmtTs(o.ts)}
                    </span>
                  </div>
                  <div className="op-tl-meta">
                    <span>{o.actor || "—"}</span>
                    <span className="mono" title="op_id">
                      {o.op_id}
                    </span>
                  </div>
                </li>
              ))}
            </ol>
          )}
        </section>
      </div>

      <section className="card table-card attempts-table" aria-labelledby="code-attempts-h">
        <div className="card-head">
          <h2 id="code-attempts-h">Попытки операций</h2>
          {attempts.length > 0 && (
            <span className="card-head-meta">
              Принято <strong>{decided.accepted ?? 0}</strong> · Повтор <strong>{decided.duplicate ?? 0}</strong> ·
              Отклонено <strong>{decided.rejected ?? 0}</strong>
            </span>
          )}
        </div>
        <table className="table">
          <thead>
            <tr>
              <th style={{ width: 160 }}>Создано</th>
              <th>Источник</th>
              <th>Операция</th>
              <th>Время события, как передано</th>
              <th>Исполнитель</th>
              <th>Решение</th>
              <th>Причина</th>
              <th>op_id</th>
            </tr>
          </thead>
          <tbody>
            {attempts.length === 0 && <EmptyRow colSpan={8}>Попыток операций по коду нет.</EmptyRow>}
            {attempts.map((a) => (
              <tr key={a.attempt_id}>
                <td className="nowrap" title={a.created_at}>
                  {fmtTs(a.created_at)}
                </td>
                <td>{SOURCE_LABEL[a.source] ?? a.source}</td>
                <td className="nowrap" title={a.op_type}>
                  {opName(a.op_type)}
                </td>
                <td className="code-cell">{a.ts_raw || <span className="faint">—</span>}</td>
                <td>
                  {a.actor || <span className="faint">—</span>}
                  {a.initiator && (
                    <div className="attempt-sub" title="Инициатор (пользователь, отправивший операцию)">
                      {a.initiator}
                      {a.role ? ` · ${a.role}` : ""}
                    </div>
                  )}
                </td>
                <td>
                  <DecisionBadge decision={a.decision} />
                </td>
                <td className="attempt-reason">
                  {a.reason ? <ReasonBadge reason={a.reason} /> : <span className="faint">—</span>}
                  {a.message && <div className="attempt-msg">{a.message}</div>}
                </td>
                <td className="code-cell">{a.op_id || <span className="faint">—</span>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <section className="card table-card" aria-labelledby="code-viol-h">
        <div className="card-head">
          <h2 id="code-viol-h">Расхождения по коду</h2>
          {data.violations.length > 0 && (
            <span className="card-head-meta">
              Открыто <strong>{openViolations}</strong> из <strong>{data.violations.length}</strong>
            </span>
          )}
        </div>
        {data.violations.length === 0 ? (
          <p className="code-violations-empty">Расхождений по коду нет.</p>
        ) : (
          <ViolationsTable rows={data.violations} onChanged={onChanged} showCode={false} />
        )}
      </section>
    </>
  );
}
