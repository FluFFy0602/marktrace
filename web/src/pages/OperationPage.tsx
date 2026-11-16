import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, type CodeStatus, type Item, type OperationRequest, type OperationResult, type OpType } from "../api";
import { opTypesFor, useAuth } from "../auth";
import { DecisionBadge, ErrorBox, ReasonBadge, StatusBadge } from "../components";
import { fmtTs, localToRfc3339, nowLocal, REASON_LABEL, STATUS_LABEL } from "../labels";
import { btnClass, Button, Combobox, DescriptionList, Field, PageHeader, Select, type SelectOption } from "../ui";
import { checkCode, CodeHint, normGtin } from "./codeMask";
import "./ops.css";

const OP_NAME: Record<OpType, string> = {
  issue: "Эмиссия",
  introduce: "Ввод в оборот",
  sell: "Продажа",
  return: "Возврат",
  dispose: "Выбытие",
};

// SPEC §1.4 lifecycle: allowed source statuses and the resulting status.
const TRANSITION: Record<OpType, { from: CodeStatus[] | null; to: CodeStatus }> = {
  issue: { from: null, to: "issued" },
  introduce: { from: ["issued"], to: "introduced" },
  sell: { from: ["introduced", "returned"], to: "sold" },
  return: { from: ["sold"], to: "returned" },
  dispose: { from: ["introduced", "sold", "returned"], to: "disposed" },
};

const VERDICT: Record<string, { title: string; cls: string; dot: string }> = {
  accepted: { title: "Операция принята", cls: "is-ok", dot: "ok" },
  rejected: { title: "Операция отклонена", cls: "is-bad", dot: "bad" },
  duplicate: { title: "Повтор: операция уже учтена", cls: "is-warn", dot: "warn" },
};

/** One submitted operation, kept for the "recent operations" list of this browser session. */
interface RecentOp {
  at: string;
  op_type: OpType;
  code: string;
  decision: string;
  reason?: string;
}

const RECENT_KEY = "marktrace.operation.recent";
const RECENT_MAX = 8;

function loadRecent(): RecentOp[] {
  try {
    const raw = window.sessionStorage.getItem(RECENT_KEY);
    const v = raw ? (JSON.parse(raw) as unknown) : [];
    return Array.isArray(v) ? (v as RecentOp[]).slice(0, RECENT_MAX) : [];
  } catch {
    return [];
  }
}

function saveRecent(list: RecentOp[]) {
  try {
    window.sessionStorage.setItem(RECENT_KEY, JSON.stringify(list));
  } catch {
    /* storage unavailable: the list just lives in memory */
  }
}

function fmtTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? iso
    : d.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function TransitionHint({ op }: { op: OpType }) {
  const t = TRANSITION[op];
  return (
    <div className="ops-transition">
      <span>{t.from ? "Из:" : "Новый код без принятой истории"}</span>
      {t.from?.map((s, i) => (
        <span key={s} className={`status st-${s}`}>
          {STATUS_LABEL[s]}
          {i < t.from!.length - 1 ? "," : ""}
        </span>
      ))}
      <span aria-label="переходит в">→</span>
      <span className={`status st-${t.to}`}>{STATUS_LABEL[t.to]}</span>
    </div>
  );
}

export default function OperationPage() {
  const { user } = useAuth();
  const allowed = opTypesFor(user?.role);
  const [opType, setOpType] = useState<OpType>(allowed[0] ?? "issue");
  const [code, setCode] = useState("");
  const [itemCode, setItemCode] = useState("");
  const [timeMode, setTimeMode] = useState<"server" | "explicit">("server");
  const [ts, setTs] = useState("");
  const [actor, setActor] = useState("");
  const [opId, setOpId] = useState("");
  const [items, setItems] = useState<Item[]>([]);
  const [itemsError, setItemsError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [result, setResult] = useState<{ req: OperationRequest; res: OperationResult } | null>(null);
  const [recent, setRecent] = useState<RecentOp[]>(loadRecent);

  useEffect(() => {
    api.items().then(setItems).catch(setItemsError);
  }, []);

  const opOptions: SelectOption[] = allowed.map((t) => ({ value: t, label: OP_NAME[t], hint: t }));
  const itemOptions: SelectOption[] = useMemo(
    () => items.map((it) => ({ value: it.item_code, code: it.item_code, label: it.name })),
    [items],
  );

  // For issue: the item whose GTIN matches the GTIN embedded in the code, offered as a shortcut.
  const codeCheck = checkCode(code);
  const gtinMatch = useMemo(() => {
    if (!codeCheck.gtin) return undefined;
    return items.find((it) => normGtin(it.gtin) === codeCheck.gtin);
  }, [items, codeCheck.gtin]);

  const isIssue = opType === "issue";
  const needItem = isIssue && !itemCode;
  const explicitTs = timeMode === "explicit" ? localToRfc3339(ts) : "";

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!code || needItem) return;
    setBusy(true);
    setError(null);
    setResult(null);
    const req: OperationRequest = { op_type: opType, code };
    if (isIssue && itemCode) req.item_code = itemCode;
    if (explicitTs) req.ts = explicitTs;
    if (actor.trim()) req.actor = actor;
    if (opId.trim()) req.op_id = opId;
    try {
      const res = await api.operation(req);
      setResult({ req, res });
      const entry: RecentOp = {
        at: new Date().toISOString(),
        op_type: opType,
        code,
        decision: res.decision,
        reason: res.reason,
      };
      setRecent((prev) => {
        const next = [entry, ...prev].slice(0, RECENT_MAX);
        saveRecent(next);
        return next;
      });
      // A reused (code, ts) or op_id would make the next operation an IDEMPOTENCY_CONFLICT:
      // after any recorded decision fall back to server time and a fresh op_id.
      setTs("");
      setTimeMode("server");
      setOpId("");
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  function clearForm() {
    setCode("");
    setItemCode("");
    setTs("");
    setTimeMode("server");
    setActor("");
    setOpId("");
  }

  const res = result?.res;
  const resultCode = res?.code?.code ?? result?.req.code;
  const verdict = res ? VERDICT[res.decision] ?? { title: res.decision, cls: "", dot: "neutral" } : null;

  return (
    <div>
      <PageHeader
        title="Операция с кодом"
        subtitle="Регистрация события жизненного цикла кода маркировки. Доступные операции зависят от роли."
      />
      <div className="ops-layout">
        <form className="card ops-form" onSubmit={submit} noValidate>
          <Field label="Тип операции" htmlFor="op-type" hint={<TransitionHint op={opType} />}>
            <Select
              id="op-type"
              value={opType}
              onChange={(v) => setOpType(v as OpType)}
              options={opOptions}
              disabled={allowed.length <= 1}
            />
          </Field>

          <Field label="Код маркировки" htmlFor="op-code" hint={<CodeHint code={code} />}>
            <input
              id="op-code"
              className={`mono${codeCheck.state === "bad" ? " ops-invalid" : ""}`}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="010468913592648321BKVhBt"
              autoComplete="off"
              spellCheck={false}
              aria-invalid={codeCheck.state === "bad" || undefined}
            />
          </Field>

          {isIssue && (
            <Field
              label="Товар"
              htmlFor="op-item"
              error={itemsError ? "Не удалось загрузить справочник товаров" : undefined}
              hint={
                gtinMatch && gtinMatch.item_code !== itemCode ? (
                  <span className="ops-suggest">
                    GTIN кода соответствует товару <span className="mono">{gtinMatch.item_code}</span>
                    <button type="button" className="btn btn-link" onClick={() => setItemCode(gtinMatch.item_code)}>
                      Подставить
                    </button>
                  </span>
                ) : (
                  "Обязательно для эмиссии: привязка кода к товару"
                )
              }
            >
              <Combobox
                id="op-item"
                value={itemCode}
                onChange={setItemCode}
                options={itemOptions}
                placeholder="Выберите товар"
                searchPlaceholder="Код или наименование"
                popoverWidth={440}
                clearable
              />
            </Field>
          )}

          <div className="ops-time">
          <fieldset className="ops-radio-group">
            <legend>Время события</legend>
            <label className="ops-radio">
              <input
                type="radio"
                name="op-time"
                checked={timeMode === "server"}
                onChange={() => setTimeMode("server")}
              />
              Сейчас (время сервера)
            </label>
            <label className="ops-radio">
              <input
                type="radio"
                name="op-time"
                checked={timeMode === "explicit"}
                onChange={() => {
                  setTimeMode("explicit");
                  if (!ts) setTs(nowLocal());
                }}
              />
              Указать вручную
            </label>
          </fieldset>
          {timeMode === "explicit" && (
            <>
              <div className="ops-time-row">
                <input
                  id="op-ts"
                  type="datetime-local"
                  step="1"
                  aria-label="Время события"
                  value={ts}
                  onChange={(e) => setTs(e.target.value)}
                />
                <Button size="sm" onClick={() => setTs(nowLocal())}>
                  Текущее
                </Button>
              </div>
              <div className="field-hint">
                {explicitTs ? (
                  <>
                    Будет отправлено: <span className="mono">{explicitTs}</span>
                  </>
                ) : (
                  "Время не задано — будет использовано время сервера"
                )}
              </div>
            </>
          )}
          </div>

          <details>
            <summary>Дополнительно: исполнитель и op_id</summary>
            <Field label="Исполнитель (actor)" htmlFor="op-actor" hint="По умолчанию — текущий пользователь">
              <input
                id="op-actor"
                value={actor}
                onChange={(e) => setActor(e.target.value)}
                placeholder={user?.username}
                autoComplete="off"
              />
            </Field>
            <Field label="op_id" htmlFor="op-id" hint="По умолчанию сервер сгенерирует UUID">
              <input
                id="op-id"
                className="mono"
                value={opId}
                onChange={(e) => setOpId(e.target.value)}
                placeholder="новый UUID"
                autoComplete="off"
                spellCheck={false}
              />
            </Field>
          </details>

          <div className="ops-actions">
            <Button type="submit" variant="primary" disabled={busy || !code || needItem}>
              {busy ? "Отправка…" : "Отправить"}
            </Button>
            <Button variant="ghost" onClick={clearForm} disabled={busy}>
              Очистить
            </Button>
            {needItem && code && <span className="ops-actions-note">Выберите товар</span>}
          </div>
        </form>

        <div className="ops-side">
          <section className="card ops-result" aria-live="polite">
            <div className="card-head">
              <h2>Результат</h2>
            </div>
            <ErrorBox error={error} />
            {!res && !error && (
              <p className="ops-placeholder">Здесь появится решение сервера по отправленной операции.</p>
            )}
            {res && verdict && (
              <>
                <div className="ops-verdict">
                  <span className={`ops-verdict-title ${verdict.cls}`}>
                    <span className={`dot dot-${verdict.dot}`} />
                    {verdict.title}
                  </span>
                  <span className="ops-verdict-sub">
                    {OP_NAME[result!.req.op_type]} · <span className="mono">{result!.req.code}</span>
                  </span>
                </div>
                {res.reason && (
                  <div className="ops-reason">
                    <ReasonBadge reason={res.reason} />
                    <span className="ops-reason-label">{REASON_LABEL[res.reason] ?? res.reason}</span>
                  </div>
                )}
                {res.message && <p className="ops-message">{res.message}</p>}
                <DescriptionList
                  items={[
                    res.code && { term: "Статус кода", value: <StatusBadge status={res.code.status} /> },
                    res.code && {
                      term: "Последнее событие",
                      value: `${fmtTs(res.code.last_ts)} · ${res.code.last_actor}`,
                      title: res.code.last_ts,
                    },
                    res.code && { term: "Товар", value: res.code.item_code, mono: true },
                    { term: "op_id", value: res.op_id, mono: true },
                    { term: "attempt_id", value: res.attempt_id, mono: true },
                  ]}
                />
                {resultCode && (
                  <div className="ops-result-actions">
                    <Link className={btnClass("secondary", "sm")} to={`/codes/${encodeURIComponent(resultCode)}`}>
                      История кода
                    </Link>
                  </div>
                )}
              </>
            )}
          </section>

          <section className="card ops-recent">
            <div className="card-head">
              <h2>Последние операции в этой сессии</h2>
              {recent.length > 0 && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setRecent([]);
                    saveRecent([]);
                  }}
                >
                  Очистить список
                </Button>
              )}
            </div>
            {recent.length === 0 ? (
              <p className="ops-placeholder">Операций пока не было.</p>
            ) : (
              <table className="table compact">
                <thead>
                  <tr>
                    <th style={{ width: 72 }}>Время</th>
                    <th>Операция</th>
                    <th>Код</th>
                    <th>Решение</th>
                  </tr>
                </thead>
                <tbody>
                  {recent.map((r, i) => (
                    <tr key={`${r.at}-${i}`}>
                      <td className="nowrap muted">{fmtTime(r.at)}</td>
                      <td className="nowrap">{OP_NAME[r.op_type] ?? r.op_type}</td>
                      <td className="code-cell">
                        <Link className="mono" to={`/codes/${encodeURIComponent(r.code)}`} title={r.code}>
                          {r.code}
                        </Link>
                      </td>
                      <td className="nowrap">
                        <DecisionBadge decision={r.decision} />
                        {r.reason && (
                          <span className="mono xsmall muted" title={REASON_LABEL[r.reason] ?? r.reason}>
                            {" "}
                            {r.reason}
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}
