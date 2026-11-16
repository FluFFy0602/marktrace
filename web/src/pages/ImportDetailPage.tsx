import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { api, SOURCES, type ImportRow, type ImportSession } from "../api";
import { CodeLink, ErrorBox, Loading, ReasonBadge, SessionStatusBadge } from "../components";
import { fmtTs, REASON_LABEL, shortId, SOURCE_LABEL } from "../labels";
import { Combobox, DescriptionList, EmptyRow, Field, PageHeader, Select, TextFilter, Toolbar, useQueryFilters } from "../ui";
import { fmtSize, SessionSummary } from "./ImportPage";
import "./imp.css";

function rawText(raw: unknown): string {
  if (raw === null || raw === undefined) return "";
  if (typeof raw === "string") return raw;
  if (Array.isArray(raw)) return raw.map((v) => (v !== null && typeof v === "object" ? rawText(v) : String(v))).join(",");
  if (typeof raw === "object") {
    return Object.entries(raw as Record<string, unknown>)
      .map(([k, v]) => `${k}=${v}`)
      .join(", ");
  }
  return String(raw);
}

type FileMeta = { entity?: string; filename?: string; size?: number; sha256?: string };

/** Uploaded files, one per line: "Товары  items.csv  10,5 КБ". */
function FilesList({ files }: { files: unknown }) {
  if (!files) return null;
  if (!Array.isArray(files)) return <span className="mono">{rawText(files)}</span>;
  if (files.length === 0) return null;
  return (
    <ul className="imp-files">
      {(files as FileMeta[]).map((f, i) =>
        f && typeof f === "object" ? (
          <li key={i} title={f.sha256 ? `sha256 ${f.sha256}` : undefined}>
            <span className="imp-files-entity">{SOURCE_LABEL[f.entity ?? ""] ?? f.entity}</span>
            <span className="mono">{f.filename}</span>
            <span className="muted num">{fmtSize(f.size)}</span>
          </li>
        ) : (
          <li key={i} className="mono">
            {String(f)}
          </li>
        ),
      )}
    </ul>
  );
}

function duration(a?: string, b?: string | null): string | null {
  if (!a || !b) return null;
  const ms = new Date(b).getTime() - new Date(a).getTime();
  if (!Number.isFinite(ms) || ms < 0) return null;
  return ms < 1000 ? `${ms} мс` : `${(ms / 1000).toLocaleString("ru-RU", { maximumFractionDigits: 1 })} с`;
}

function countBy(rows: ImportRow[], key: (r: ImportRow) => string | null | undefined): Map<string, number> {
  const m = new Map<string, number>();
  for (const r of rows) {
    const k = key(r);
    if (k) m.set(k, (m.get(k) ?? 0) + 1);
  }
  return m;
}

function RejectedRows({ rows }: { rows: ImportRow[] }) {
  const f = useQueryFilters(["entity", "reason", "q"] as const);
  const { entity, reason, q } = f.values;

  const byEntity = useMemo(() => countBy(rows, (r) => r.entity), [rows]);
  const byReason = useMemo(() => countBy(rows, (r) => r.reason), [rows]);

  const entityOptions = useMemo(
    () =>
      [...SOURCES, ...[...byEntity.keys()].filter((e) => !SOURCES.includes(e as never))]
        .filter((e) => byEntity.has(e))
        .map((e) => ({ value: e, label: SOURCE_LABEL[e] ?? e, hint: String(byEntity.get(e)) })),
    [byEntity],
  );
  const reasonOptions = useMemo(
    () =>
      [...byReason.entries()]
        .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
        .map(([r, n]) => ({ value: r, code: r, label: REASON_LABEL[r] ?? r, hint: String(n) })),
    [byReason],
  );

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return rows.filter((r) => {
      if (entity && r.entity !== entity) return false;
      if (reason && r.reason !== reason) return false;
      if (needle) {
        const hay = `${r.key ?? ""} ${r.message ?? ""} ${rawText(r.raw)} ${r.line_no}`.toLowerCase();
        if (!hay.includes(needle)) return false;
      }
      return true;
    });
  }, [rows, entity, reason, q]);

  return (
    <section className="card table-card imp-rejected" aria-labelledby="imp-rejected-title">
      <div className="card-head">
        <h2 id="imp-rejected-title">
          Отклонённые строки <span className="muted imp-count">{rows.length}</span>
        </h2>
      </div>
      {rows.length > 0 && (
        <Toolbar onReset={f.reset} resetDisabled={!f.active} className="toolbar-inset">
          <Field label="Файл" htmlFor="imp-f-entity" width={190}>
            <Select
              id="imp-f-entity"
              value={entity}
              onChange={(v) => f.set({ entity: v })}
              allLabel="Все файлы"
              clearable
              options={entityOptions}
            />
          </Field>
          <Field label="Причина" htmlFor="imp-f-reason" width={300}>
            <Combobox
              id="imp-f-reason"
              value={reason}
              onChange={(v) => f.set({ reason: v })}
              allLabel="Все причины"
              clearable
              searchPlaceholder="Код или описание"
              popoverWidth={420}
              triggerShows="code"
              options={reasonOptions}
            />
          </Field>
          <Field label="Поиск" htmlFor="imp-f-q" grow>
            <TextFilter
              id="imp-f-q"
              value={q}
              onChange={(v) => f.set({ q: v })}
              placeholder="Ключ, сообщение или исходная строка"
            />
          </Field>
        </Toolbar>
      )}
      {f.active && rows.length > 0 && (
        <div className="result-bar">
          <span>
            Показано <strong>{shown.length}</strong> из {rows.length}
          </span>
        </div>
      )}
      <table className="table">
          <thead>
            <tr>
              <th>Файл</th>
              <th className="num">Строка</th>
              <th>Ключ</th>
              <th>Причина</th>
              <th>Сообщение</th>
              <th>Исходная строка</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && <EmptyRow colSpan={6}>Все строки приняты, отклонённых нет.</EmptyRow>}
            {rows.length > 0 && shown.length === 0 && <EmptyRow colSpan={6}>Нет строк по заданным фильтрам.</EmptyRow>}
            {shown.map((r, i) => {
              const raw = rawText(r.raw);
              return (
                <tr key={r.row_id ?? `${r.entity}-${r.line_no}-${i}`}>
                  <td className="nowrap">{SOURCE_LABEL[r.entity] ?? r.entity}</td>
                  <td className="num">{r.line_no}</td>
                  <td className="code-cell">
                    {/* Only a duplicate refers to a code that is actually registered; other
                        rejected codes were never stored and would open CODE_NOT_FOUND. */}
                    {r.entity === "codes" && r.key && r.reason === "DUPLICATE_CODE" ? (
                      <CodeLink code={r.key} />
                    ) : (
                      r.key || <span className="faint">—</span>
                    )}
                  </td>
                  <td className="nowrap">
                    <ReasonBadge reason={r.reason} />
                    <span className="imp-reason-label">{(r.reason && REASON_LABEL[r.reason]) || ""}</span>
                  </td>
                  <td className="msg">{r.message || <span className="faint">—</span>}</td>
                  <td className="raw imp-raw" title={raw}>
                    {raw || <span className="faint">—</span>}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
    </section>
  );
}

export default function ImportDetailPage() {
  const { id = "" } = useParams();
  const [s, setS] = useState<ImportSession | null>(null);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    setS(null);
    setError(null);
    api.importDetail(id).then(setS).catch(setError);
  }, [id]);

  return (
    <div>
      <PageHeader
        back={{ to: "/imports", label: "Все сеансы импорта" }}
        title={
          <>
            Сеанс импорта <span className="mono imp-title-id">{shortId(s?.session_id || id)}</span>
          </>
        }
        subtitle={
          s && (
            <>
              <SessionStatusBadge status={s.status} />
              <span className="imp-sub-sep">·</span>
              {fmtTs(s.started_at)}
              {s.started_by && (
                <>
                  <span className="imp-sub-sep">·</span>
                  {s.started_by}
                </>
              )}
            </>
          )
        }
      />
      <ErrorBox error={error} />
      {!s && !error && <Loading />}
      {s && (
        <>
          <div className="grid-2">
            <section className="card" aria-labelledby="imp-info-title">
              <h2 id="imp-info-title">Сведения</h2>
              <DescriptionList
                items={[
                  { term: "Идентификатор", value: s.session_id, mono: true },
                  { term: "Начат", value: fmtTs(s.started_at) },
                  {
                    term: "Завершён",
                    value: s.finished_at ? (
                      <>
                        {fmtTs(s.finished_at)}
                        {duration(s.started_at, s.finished_at) && (
                          <span className="muted"> · {duration(s.started_at, s.finished_at)}</span>
                        )}
                      </>
                    ) : null,
                  },
                  {
                    term: "Инициатор",
                    value: s.started_by ? (
                      <>
                        {s.started_by}
                        {s.role && <span className="muted"> · {s.role}</span>}
                      </>
                    ) : null,
                  },
                  {
                    term: "Файлы",
                    value:
                      s.files && !(Array.isArray(s.files) && s.files.length === 0) ? <FilesList files={s.files} /> : null,
                  },
                ]}
              />
            </section>
            <section className="card" aria-labelledby="imp-sum-title">
              <h2 id="imp-sum-title">Итоги</h2>
              <SessionSummary s={s} />
            </section>
          </div>
          <RejectedRows rows={s.rejected_rows} />
        </>
      )}
    </div>
  );
}
