import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, type IssueBatch, type IssueBatchRequest, type Item } from "../api";
import { ErrorBox, StatusBadge } from "../components";
import { fmtTs } from "../labels";
import { Button, Combobox, DataMatrix, DescriptionList, Field, PageHeader, type SelectOption } from "../ui";
import { normGtin } from "./codeMask";
import "./ops.css";
import "./issue.css";

const MAX_QTY = 1000;

/** RFC 4122 v4 UUID; crypto.randomUUID needs a secure context, getRandomValues does not. */
function newRequestId(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
  const b = crypto.getRandomValues(new Uint8Array(16));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = [...b].map((x) => x.toString(16).padStart(2, "0")).join("");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

function parseQty(raw: string): number | null {
  if (!/^\d+$/.test(raw)) return null;
  const n = Number(raw);
  return n >= 1 && n <= MAX_QTY ? n : null;
}

function downloadText(text: string, name: string, type: string) {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

export default function IssuePage() {
  const [items, setItems] = useState<Item[]>([]);
  const [itemsError, setItemsError] = useState<unknown>(null);
  const [itemCode, setItemCode] = useState("");
  const [qtyRaw, setQtyRaw] = useState("1");
  // Idempotency key of the request being prepared: kept across retries of the same input,
  // replaced when the input changes or a batch has been issued.
  const [requestId, setRequestId] = useState(newRequestId);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [batch, setBatch] = useState<IssueBatch | null>(null);
  const [lastReq, setLastReq] = useState<IssueBatchRequest | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    api.items().then(setItems).catch(setItemsError);
  }, []);

  const itemOptions: SelectOption[] = useMemo(
    () => items.map((it) => ({ value: it.item_code, code: it.item_code, label: it.name })),
    [items],
  );
  const item = items.find((it) => it.item_code === itemCode);
  const qty = parseQty(qtyRaw);

  function changeItem(v: string) {
    setItemCode(v);
    setRequestId(newRequestId());
  }
  function changeQty(v: string) {
    setQtyRaw(v);
    setRequestId(newRequestId());
  }

  async function send(req: IssueBatchRequest) {
    setBusy(true);
    setError(null);
    setCopied(false);
    try {
      const res = await api.issueBatch(req);
      setBatch(res);
      setLastReq(req);
      setRequestId(newRequestId());
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    if (!itemCode || qty === null) return;
    void send({ request_id: requestId, item_code: itemCode, quantity: qty });
  }

  async function copyCodes() {
    if (!batch) return;
    try {
      await navigator.clipboard.writeText(batch.codes.map((c) => c.code).join("\n"));
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  const qtyError = qtyRaw !== "" && qty === null ? `Целое число от 1 до ${MAX_QTY}` : null;

  return (
    <div>
      <PageHeader
        title="Выпуск кодов"
        subtitle="Новые коды маркировки для товара: сервер создаёт партию, регистрирует её в ЦРПТ (мок) и проводит эмиссию каждого кода."
      />
      <div className="ops-layout">
        <form className="card ops-form" onSubmit={submit} noValidate>
          <Field
            label="Товар"
            htmlFor="issue-item"
            hint={
              item ? (
                <span>
                  GTIN <span className="mono">{normGtin(item.gtin)}</span> · коды вида{" "}
                  <span className="mono">01{normGtin(item.gtin)}21XXXXXX</span>
                </span>
              ) : (
                "Коды получат GTIN выбранного товара"
              )
            }
            error={itemsError ? "Не удалось загрузить справочник товаров" : undefined}
          >
            <Combobox
              id="issue-item"
              value={itemCode}
              onChange={changeItem}
              options={itemOptions}
              placeholder="Выберите товар"
              searchPlaceholder="Код или наименование"
              popoverWidth={440}
            />
          </Field>
          <Field
            label="Количество экземпляров"
            htmlFor="issue-qty"
            hint={`От 1 до ${MAX_QTY} кодов за один выпуск`}
            error={qtyError}
          >
            <input
              id="issue-qty"
              className={`issue-qty${qtyError ? " ops-invalid" : ""}`}
              inputMode="numeric"
              value={qtyRaw}
              onChange={(e) => changeQty(e.target.value.trim())}
              autoComplete="off"
            />
          </Field>
          <div className="ops-actions">
            <Button type="submit" variant="primary" disabled={busy || !itemCode || qty === null}>
              {busy ? "Выпуск…" : "Выпустить коды"}
            </Button>
            <span className="ops-actions-note mono" title="Ключ идемпотентности запроса">
              {requestId.slice(0, 8)}
            </span>
          </div>
          <ErrorBox error={error} />
        </form>

        <div className="ops-side">
          <section className="card issue-result" aria-live="polite">
            <div className="card-head">
              <h2>{batch ? `Партия · ${batch.codes.length} шт.` : "Партия"}</h2>
              {batch && (
                <div className="issue-actions">
                  <Button size="sm" onClick={copyCodes}>
                    {copied ? "Скопировано" : "Скопировать коды"}
                  </Button>
                  <Button
                    size="sm"
                    onClick={() =>
                      downloadText(
                        "code\n" + batch.codes.map((c) => c.code).join("\n") + "\n",
                        `codes-${batch.item_code}-${batch.batch_id.slice(0, 8)}.csv`,
                        "text/csv",
                      )
                    }
                  >
                    Скачать CSV
                  </Button>
                  {lastReq && (
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={busy}
                      onClick={() => void send(lastReq)}
                      title="Отправить тот же запрос с тем же ключом: сервер вернёт эту же партию"
                    >
                      Повторить запрос
                    </Button>
                  )}
                </div>
              )}
            </div>
            {!batch ? (
              <p className="ops-placeholder">
                Здесь появятся выпущенные коды и Data Matrix для каждого. Повтор того же запроса возвращает ту же
                партию.
              </p>
            ) : (
              <>
                {batch.replayed && (
                  <div className="alert alert-info">Повтор запроса: партия уже выпущена, новые коды не созданы.</div>
                )}
                <DescriptionList
                  className="issue-kv"
                  items={[
                    {
                      term: "Товар",
                      value: (
                        <>
                          <span className="mono">{batch.item_code}</span> · {batch.item_name}
                        </>
                      ),
                    },
                    { term: "GTIN", value: batch.gtin, mono: true },
                    { term: "Выпуск", value: `${fmtTs(batch.ts)} · ${batch.actor}` },
                    { term: "Партия", value: batch.batch_id, mono: true },
                  ]}
                />
                <ol className="issue-grid">
                  {batch.codes.map((c, i) => (
                    <li key={c.code} className="issue-tile">
                      <DataMatrix value={c.code} size={112} />
                      <div className="issue-tile-body">
                        <span className="issue-tile-pos">№ {i + 1}</span>
                        <Link className="mono issue-tile-code" to={`/codes/${encodeURIComponent(c.code)}`}>
                          {c.code.slice(0, 18)}
                          <b>{c.code.slice(18)}</b>
                        </Link>
                        <StatusBadge status={c.status} />
                      </div>
                    </li>
                  ))}
                </ol>
              </>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}
