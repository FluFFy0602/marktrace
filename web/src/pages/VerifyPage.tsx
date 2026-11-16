import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, type VerifyResult } from "../api";
import { ErrorBox } from "../components";
import { btnClass, Button, DescriptionList, Field, PageHeader } from "../ui";
import { checkCode, CodeHint } from "./codeMask";
import "./ops.css";

const VERIFY_REASON: Record<string, string> = {
  INVALID_FORMAT: "Код не соответствует маске",
  UNKNOWN_CODE: "Код не найден в реестре ЦРПТ",
  GTIN_MISMATCH: "GTIN не совпадает с GTIN в коде",
  MOCK_UNAVAILABLE: "Сервис ЦРПТ недоступен",
};

function gtinHint(g: string): { text: string; cls?: string } {
  if (!g) return { text: "13 или 14 цифр. Пусто — сверка GTIN не выполняется" };
  if (!/^\d+$/.test(g)) return { text: "Только цифры", cls: "bad" };
  if (g.length !== 13 && g.length !== 14) return { text: `Нужно 13 или 14 цифр, введено ${g.length}`, cls: "bad" };
  return { text: `GTIN-${g.length}`, cls: "ok" };
}

export default function VerifyPage() {
  const [code, setCode] = useState("");
  const [gtin, setGtin] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [res, setRes] = useState<{ code: string; gtin: string; at: Date; r: VerifyResult } | null>(null);

  const codeCheck = checkCode(code);
  const gh = gtinHint(gtin);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!code) return;
    setBusy(true);
    setError(null);
    setRes(null);
    try {
      setRes({ code, gtin, at: new Date(), r: await api.verify(code, gtin || undefined) });
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  const valid = res?.r.is_valid;

  return (
    <div>
      <PageHeader
        title="Проверка кода в ЦРПТ"
        subtitle="Запрос к имитации ЦРПТ (/cz/verify). Проверка не меняет статус кода."
      />
      <div className="ops-layout">
        <form className="card ops-form" onSubmit={submit} noValidate>
          <Field label="Код маркировки" htmlFor="verify-code" hint={<CodeHint code={code} />}>
            <input
              id="verify-code"
              className={`mono${codeCheck.state === "bad" ? " ops-invalid" : ""}`}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="010468913592648321BKVhBt"
              autoComplete="off"
              spellCheck={false}
              aria-invalid={codeCheck.state === "bad" || undefined}
            />
          </Field>
          <Field
            label="GTIN (необязательно)"
            htmlFor="verify-gtin"
            hint={
              codeCheck.gtin && gtin !== codeCheck.gtin ? (
                <span className="ops-suggest">
                  <span className={gh.cls}>{gh.text}</span>
                  <button type="button" className="btn btn-link" onClick={() => setGtin(codeCheck.gtin!)}>
                    Взять из кода
                  </button>
                </span>
              ) : (
                <span className={gh.cls}>{gh.text}</span>
              )
            }
          >
            <input
              id="verify-gtin"
              className={`mono${gh.cls === "bad" ? " ops-invalid" : ""}`}
              value={gtin}
              onChange={(e) => setGtin(e.target.value)}
              placeholder="04689135926483"
              autoComplete="off"
              spellCheck={false}
              inputMode="numeric"
            />
          </Field>
          <div className="ops-actions">
            <Button type="submit" variant="primary" disabled={busy || !code}>
              {busy ? "Проверка…" : "Проверить"}
            </Button>
            <Button
              variant="ghost"
              disabled={busy || (!code && !gtin)}
              onClick={() => {
                setCode("");
                setGtin("");
              }}
            >
              Очистить
            </Button>
          </div>
        </form>

        <section className="card ops-result" aria-live="polite">
          <div className="card-head">
            <h2>Ответ ЦРПТ</h2>
          </div>
          <ErrorBox error={error} />
          {!res && !error && <p className="ops-placeholder">Введите код и нажмите «Проверить».</p>}
          {res && (
            <>
              <div className="ops-verdict">
                <span className={`ops-verdict-title ${valid ? "is-ok" : "is-bad"}`}>
                  <span className={`dot dot-${valid ? "ok" : "bad"}`} />
                  {valid ? "Код действителен" : "Код недействителен"}
                </span>
                <span className="ops-verdict-sub mono">{res.code}</span>
              </div>
              {res.r.reason && (
                <div className="ops-reason">
                  <span className="badge reason r-mock">{res.r.reason}</span>
                  <span className="ops-reason-label">{VERIFY_REASON[res.r.reason] ?? res.r.reason}</span>
                </div>
              )}
              <DescriptionList
                items={[
                  { term: "GTIN в запросе", value: res.gtin || "не передан", mono: !!res.gtin },
                  {
                    term: "Проверено",
                    value: res.at.toLocaleString("ru-RU", {
                      day: "2-digit",
                      month: "2-digit",
                      year: "numeric",
                      hour: "2-digit",
                      minute: "2-digit",
                      second: "2-digit",
                    }),
                  },
                ]}
              />
              <div className="ops-result-actions">
                <Link className={btnClass("secondary", "sm")} to={`/codes/${encodeURIComponent(res.code)}`}>
                  История кода
                </Link>
              </div>
              <details className="ops-raw">
                <summary>Ответ сервиса (JSON)</summary>
                <pre className="json">{JSON.stringify(res.r, null, 2)}</pre>
              </details>
            </>
          )}
        </section>
      </div>
    </div>
  );
}
