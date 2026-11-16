import { useCallback, useEffect, useRef, useState, type DragEvent, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, rowsCreatedTotal, SOURCES, type ImportSession, type Source } from "../api";
import { canImport, useAuth } from "../auth";
import { ErrorBox, Loading, SessionStatusBadge } from "../components";
import { fmtTs, shortId, SOURCE_LABEL } from "../labels";
import { Button, btnClass, EmptyRow, PageHeader } from "../ui";
import "./imp.css";

/** Expected file name and CSV header per entity (SPEC §4). */
const FILE_SPEC: Record<Source, { file: string; header: string }> = {
  items: { file: "items.csv", header: "item_code,name,gtin" },
  codes: { file: "codes.csv", header: "code,item_code,status,ts" },
  operations: { file: "operations.csv", header: "op_id,code,op_type,ts,actor" },
};

/** "10,5 КБ" / "812 Б". */
export function fmtSize(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || !Number.isFinite(bytes)) return "—";
  if (bytes < 1024) return `${bytes} Б`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toLocaleString("ru-RU", { maximumFractionDigits: 1 })} КБ`;
  return `${(bytes / 1024 / 1024).toLocaleString("ru-RU", { maximumFractionDigits: 1 })} МБ`;
}

type Totals = { accepted: number; duplicate: number; rejected: number };

function sumCounters(s: ImportSession): Totals | null {
  let any = false;
  const t: Totals = { accepted: 0, duplicate: 0, rejected: 0 };
  for (const e of SOURCES) {
    const c = s.counters[e];
    if (!c) continue;
    any = true;
    t.accepted += c.accepted;
    t.duplicate += c.duplicate;
    t.rejected += c.rejected;
  }
  return any ? t : null;
}

/** Number cell: zero is faint, rejected > 0 is red. */
function Count({ n, bad }: { n: number | null | undefined; bad?: boolean }) {
  if (n === null || n === undefined) return <span className="faint">—</span>;
  if (n === 0) return <span className="faint">0</span>;
  return <span className={bad ? "bad" : undefined}>{n}</span>;
}

/** Per-entity counters + rows_created + snapshot check for one session. */
export function SessionSummary({ s }: { s: ImportSession }) {
  const total = rowsCreatedTotal(s.rows_created);
  const entities = SOURCES.filter((e) => s.counters[e]);
  const sum = sumCounters(s);
  const perTable = s.rows_created && typeof s.rows_created === "object" ? Object.entries(s.rows_created) : [];
  return (
    <div className="session-summary">
      {s.failure_reason && <div className="alert alert-error">{s.failure_reason}</div>}
      {entities.length > 0 ? (
        <table className="table compact imp-counters">
          <thead>
            <tr>
              <th>Файл</th>
              <th className="num">Принято</th>
              <th className="num">Дубли</th>
              <th className="num">Отклонено</th>
            </tr>
          </thead>
          <tbody>
            {entities.map((e) => {
              const c = s.counters[e]!;
              return (
                <tr key={e}>
                  <td>
                    {SOURCE_LABEL[e]} <span className="mono faint">{FILE_SPEC[e].file}</span>
                  </td>
                  <td className="num">
                    <Count n={c.accepted} />
                  </td>
                  <td className="num">
                    <Count n={c.duplicate} />
                  </td>
                  <td className="num">
                    <Count n={c.rejected} bad />
                  </td>
                </tr>
              );
            })}
          </tbody>
          {sum && entities.length > 1 && (
            <tfoot>
              <tr>
                <td>Итого</td>
                <td className="num">{sum.accepted}</td>
                <td className="num">{sum.duplicate}</td>
                <td className="num">
                  <Count n={sum.rejected} bad />
                </td>
              </tr>
            </tfoot>
          )}
        </table>
      ) : (
        <p className="muted small">Счётчиков по файлам нет.</p>
      )}
      <dl className="imp-facts">
        <div>
          <dt>Создано новых строк</dt>
          <dd>
            <strong>{total ?? "—"}</strong>
            {perTable.length > 0 && (
              <span className="muted"> ({perTable.map(([k, v]) => `${k}: ${v}`).join(", ")})</span>
            )}
            {total === 0 && sum && sum.duplicate > 0 && (
              <span className="muted"> · повторный импорт ничего не добавил (K3 = 0)</span>
            )}
          </dd>
        </div>
        {typeof s.snapshot_violations === "number" && (
          <div>
            <dt>Расхождения со снимком</dt>
            <dd>
              <strong className={s.snapshot_violations ? "bad" : undefined}>{s.snapshot_violations}</strong>
              <span className="mono faint"> SNAPSHOT_MISMATCH</span>
            </dd>
          </div>
        )}
      </dl>
    </div>
  );
}

/** One file slot: dashed drop zone, click or keyboard opens the file dialog. */
function DropZone({
  entity,
  file,
  disabled,
  onFile,
}: {
  entity: Source;
  file: File | undefined;
  disabled: boolean;
  onFile: (f: File | undefined) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const id = `imp-file-${entity}`;
  const spec = FILE_SPEC[entity];

  // Keep the native input in sync when the parent clears the selection (after import).
  useEffect(() => {
    if (!file && inputRef.current) inputRef.current.value = "";
  }, [file]);

  function accept(f: File | undefined) {
    if (f && !/\.csv$/i.test(f.name)) {
      setError(`Нужен файл .csv, получен «${f.name}»`);
      return;
    }
    setError(null);
    onFile(f);
  }

  function onDrop(e: DragEvent) {
    e.preventDefault();
    setOver(false);
    if (disabled) return;
    accept(e.dataTransfer.files?.[0]);
  }

  const cls = ["dropzone", file ? "has-file" : "", over ? "is-over" : "", error ? "has-error" : "", disabled ? "is-disabled" : ""]
    .filter(Boolean)
    .join(" ");

  return (
    <div className="dz-col">
      <div className="dz-title">
        <span className="dz-entity">{SOURCE_LABEL[entity]}</span>
        <span className="mono faint">{spec.file}</span>
      </div>
      <div
        className={cls}
        onDragEnter={(e) => {
          e.preventDefault();
          if (!disabled) setOver(true);
        }}
        onDragOver={(e) => e.preventDefault()}
        onDragLeave={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOver(false);
        }}
        onDrop={onDrop}
      >
        <input
          ref={inputRef}
          id={id}
          className="dz-input"
          type="file"
          accept=".csv,text/csv"
          disabled={disabled}
          aria-describedby={`${id}-hint`}
          onChange={(e) => accept(e.target.files?.[0])}
        />
        <label htmlFor={id} className="dz-label">
          {file ? (
            <>
              <span className="dz-name mono" title={file.name}>
                {file.name}
              </span>
              <span className="dz-meta">{fmtSize(file.size)} · заменить</span>
            </>
          ) : (
            <>
              <span className="dz-cta">Выбрать файл</span>
              <span className="dz-meta">или перетащите сюда</span>
            </>
          )}
        </label>
        {file && !disabled && (
          <button
            type="button"
            className="btn btn-ghost btn-sm dz-remove"
            onClick={() => {
              setError(null);
              onFile(undefined);
            }}
            aria-label={`Убрать ${spec.file}`}
          >
            Убрать
          </button>
        )}
      </div>
      {error ? (
        <div className="field-error" role="alert">
          {error}
        </div>
      ) : (
        <div className="dz-header" id={`${id}-hint`}>
          <span className="faint">Заголовок:</span> <span className="mono">{spec.header}</span>
        </div>
      )}
    </div>
  );
}

export default function ImportPage() {
  const { user } = useAuth();
  const allowed = canImport(user?.role);
  const [files, setFiles] = useState<Partial<Record<Source, File>>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [result, setResult] = useState<ImportSession | null>(null);
  const [sessions, setSessions] = useState<ImportSession[] | null>(null);
  const [listError, setListError] = useState<unknown>(null);
  const [listBusy, setListBusy] = useState(false);

  const loadList = useCallback(() => {
    setListBusy(true);
    api
      .imports()
      .then((l) => {
        setSessions(l);
        setListError(null);
      })
      .catch(setListError)
      .finally(() => setListBusy(false));
  }, []);
  useEffect(loadList, [loadList]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const fd = new FormData();
    for (const k of SOURCES) {
      const f = files[k];
      if (f) fd.append(k, f, f.name);
    }
    setBusy(true);
    setError(null);
    setResult(null);
    try {
      const s = await api.importFiles(fd);
      setResult(s);
      setFiles({});
      loadList();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  const chosen = SOURCES.filter((k) => files[k]).length;

  return (
    <div>
      <PageHeader
        title="Импорт CSV"
        subtitle="Порядок обработки: товары, коды, операции, затем сверка со снимком статусов. Повторная загрузка тех же файлов не создаёт новых строк."
      />

      {allowed ? (
        <form className="card imp-upload" onSubmit={submit} aria-labelledby="imp-upload-title">
          <div className="card-head">
            <h2 id="imp-upload-title">Новый сеанс импорта</h2>
            <span className="small muted">Любой непустой набор из трёх файлов</span>
          </div>
          <div className="dz-grid">
            {SOURCES.map((k) => (
              <DropZone
                key={k}
                entity={k}
                file={files[k]}
                disabled={busy}
                onFile={(f) => setFiles((prev) => ({ ...prev, [k]: f }))}
              />
            ))}
          </div>
          <div className="form-actions imp-actions">
            <Button type="submit" variant="primary" disabled={busy || chosen === 0}>
              {busy ? "Импорт…" : "Импортировать"}
            </Button>
            {chosen > 0 && !busy && (
              <Button variant="ghost" onClick={() => setFiles({})}>
                Очистить
              </Button>
            )}
            <span className="small muted">Выбрано файлов: {chosen} из 3</span>
          </div>
        </form>
      ) : (
        <div className="alert alert-info">Ваша роль может только просматривать сеансы импорта.</div>
      )}

      {(busy || error || result) && (
        <section className="card imp-result" aria-live="polite" aria-labelledby="imp-result-title">
          <div className="card-head">
            <h2 id="imp-result-title">Результат импорта</h2>
            {result && (
              <div className="actions">
                <SessionStatusBadge status={result.status} />
                {result.session_id && (
                  <Link className={btnClass("secondary", "sm")} to={`/imports/${encodeURIComponent(result.session_id)}`}>
                    Подробнее и отклонённые строки
                  </Link>
                )}
              </div>
            )}
          </div>
          <ErrorBox error={error} />
          {busy && <Loading text="Импорт выполняется…" />}
          {result && <SessionSummary s={result} />}
        </section>
      )}

      <section className="card table-card" aria-labelledby="imp-sessions-title">
        <div className="card-head">
          <h2 id="imp-sessions-title">
            Сеансы импорта {sessions && <span className="muted imp-count">{sessions.length}</span>}
          </h2>
          <Button size="sm" variant="ghost" onClick={loadList} disabled={listBusy}>
            {listBusy && sessions ? "Обновление…" : "Обновить"}
          </Button>
        </div>
        <ErrorBox error={listError} />
        {!sessions && !listError && <Loading />}
        {sessions && (
          <table className="table">
            <thead>
              <tr>
                <th>Сеанс</th>
                <th>Начат</th>
                <th>Инициатор</th>
                <th>Статус</th>
                <th>Файлы</th>
                <th className="num">Принято</th>
                <th className="num">Дубли</th>
                <th className="num">Отклонено</th>
                <th className="num">Новых строк</th>
              </tr>
            </thead>
            <tbody>
              {sessions.length === 0 && <EmptyRow colSpan={9}>Сеансов импорта пока нет.</EmptyRow>}
              {sessions.map((s) => {
                const sum = sumCounters(s);
                const ents = SOURCES.filter((e) => s.counters[e]);
                return (
                  <tr key={s.session_id}>
                    <td className="code-cell">
                      <Link to={`/imports/${encodeURIComponent(s.session_id)}`} title={s.session_id}>
                        {shortId(s.session_id)}
                      </Link>
                    </td>
                    <td className="nowrap">{fmtTs(s.started_at)}</td>
                    <td className="nowrap">
                      {s.started_by ?? <span className="faint">—</span>}
                      {s.role && <span className="muted small"> · {s.role}</span>}
                    </td>
                    <td className="nowrap">
                      <SessionStatusBadge status={s.status} />
                    </td>
                    <td className="muted">
                      {ents.length ? ents.map((e) => SOURCE_LABEL[e]).join(", ") : <span className="faint">—</span>}
                    </td>
                    <td className="num">
                      <Count n={sum?.accepted} />
                    </td>
                    <td className="num">
                      <Count n={sum?.duplicate} />
                    </td>
                    <td className="num">
                      <Count n={sum?.rejected} bad />
                    </td>
                    <td className="num">
                      <Count n={rowsCreatedTotal(s.rows_created)} />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
