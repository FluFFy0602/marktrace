import { useCallback, useEffect, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { api, type CompareSection, type GoldenCompare, type Kpi } from "../api";
import { ErrorBox, Loading } from "../components";
import { fmtPct, fmtTs, shortId } from "../labels";
import { Button, EmptyState, PageHeader, Stat } from "../ui";
import "./imp.css";

function rowText(r: unknown): string {
  if (typeof r === "string") return r;
  if (Array.isArray(r)) return r.join(",");
  if (r && typeof r === "object") return Object.values(r as Record<string, unknown>).join(",");
  return String(r);
}

const isNum = (v: unknown): v is number => typeof v === "number" && Number.isFinite(v);

/** Figure with a smaller muted unit: 0.7 мс. */
function Figure({ value, unit }: { value: ReactNode; unit?: string }) {
  return (
    <>
      {value}
      {unit && <span className="k-unit">{unit}</span>}
    </>
  );
}

/** Two muted lines under a Stat: what is measured, and the raw numbers behind it. */
function Note({ formula, children }: { formula: ReactNode; children?: ReactNode }) {
  return (
    <>
      <div className="k-formula">{formula}</div>
      {children && <div className="k-facts">{children}</div>}
    </>
  );
}

function DiffList({ title, rows, kind }: { title: string; rows: unknown[]; kind: "missing" | "extra" }) {
  return (
    <div className="gd-list">
      <div className="gd-list-title">
        {title} <span className="muted">{rows.length}</span>
      </div>
      {rows.length === 0 ? (
        <div className="faint small">Нет</div>
      ) : (
        <ol className={`gd-rows mono gd-${kind}`}>
          {rows.map((r, i) => (
            <li key={i}>{rowText(r)}</li>
          ))}
        </ol>
      )}
    </div>
  );
}

/** One golden file as a table row; differences expand into a full-width row below. */
function CompareRow({ file, s }: { file: string; s: CompareSection | undefined }) {
  const missing = s?.missing ?? [];
  const extra = s?.extra ?? [];
  const diffs = missing.length + extra.length;
  const [open, setOpen] = useState(!!s && !s.match);
  useEffect(() => setOpen(!!s && !s.match), [s]);
  if (!s) {
    return (
      <tr>
        <td className="code-cell">{file}</td>
        <td colSpan={5} className="faint">
          Нет данных сверки
        </td>
      </tr>
    );
  }
  const detailsId = `gd-${file.replace(/\W/g, "-")}`;
  return (
    <>
      <tr>
        <td className="code-cell">{file}</td>
        <td className="nowrap">
          <span className={`stat-mark ${s.match ? "is-ok" : "is-bad"}`}>{s.match ? "Совпадает" : "Есть отличия"}</span>
        </td>
        <td className="num">{s.expected_rows}</td>
        <td className="num">{s.actual_rows}</td>
        <td className="num">{missing.length ? <span className="bad">{missing.length}</span> : <span className="faint">0</span>}</td>
        <td className="num">{extra.length ? <span className="bad">{extra.length}</span> : <span className="faint">0</span>}</td>
        <td className="gd-toggle">
          {diffs > 0 && (
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              aria-expanded={open}
              aria-controls={detailsId}
              onClick={() => setOpen((o) => !o)}
            >
              {open ? "Скрыть строки" : "Показать строки"}
            </button>
          )}
        </td>
      </tr>
      {diffs > 0 && open && (
        <tr className="gd-details" id={detailsId}>
          <td colSpan={7}>
            <div className="gd-lists">
              <DiffList title="Нет в выгрузке (есть в эталоне)" rows={missing} kind="missing" />
              <DiffList title="Лишние в выгрузке (нет в эталоне)" rows={extra} kind="extra" />
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

export default function KpiPage() {
  const [kpi, setKpi] = useState<Kpi | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(false);
  const [golden, setGolden] = useState<GoldenCompare | null>(null);
  const [goldenAt, setGoldenAt] = useState<string | null>(null);
  const [goldenError, setGoldenError] = useState<unknown>(null);
  const [goldenBusy, setGoldenBusy] = useState(false);

  const load = useCallback(() => {
    setError(null);
    setLoading(true);
    api
      .kpi()
      .then(setKpi)
      .catch(setError)
      .finally(() => setLoading(false));
  }, []);
  useEffect(load, [load]);

  async function compare() {
    setGoldenBusy(true);
    setGoldenError(null);
    try {
      setGolden(await api.golden());
      setGoldenAt(new Date().toISOString());
      load();
    } catch (e) {
      setGoldenError(e);
    } finally {
      setGoldenBusy(false);
    }
  }

  const k5 = golden?.percent ?? kpi?.k5?.percent ?? null;
  const k1Target = kpi?.k1.target ?? 99;
  const k3 = kpi?.k3.rows_created;
  const k4 = kpi?.k4.last_ms;

  return (
    <div>
      <PageHeader
        title="Сводка KPI"
        subtitle="Показатели качества по текущим данным. Пересчитываются при каждом открытии страницы."
        actions={
          <Button onClick={load} disabled={loading}>
            {loading && kpi ? "Обновление…" : "Обновить"}
          </Button>
        }
      />
      <ErrorBox error={error} />
      {!kpi && !error && <Loading />}
      {kpi && (
        <div className="stat-grid kpi-stats">
          <Stat
            id="K1"
            label="Доля корректных операций"
            value={fmtPct(kpi.k1.percent)}
            target={`≥ ${k1Target}%`}
            ok={isNum(kpi.k1.percent) ? kpi.k1.percent >= k1Target : null}
            meter={{ percent: kpi.k1.percent, target: k1Target }}
          >
            <Note formula="принято / (принято + отклонено), повторы не учитываются">
              принято {kpi.k1.accepted} · отклонено {kpi.k1.rejected}
            </Note>
          </Stat>
          <Stat
            id="K2"
            label="Согласованность статусов"
            value={fmtPct(kpi.k2.percent)}
            target="100%"
            ok={kpi.k2.total === 0 || !isNum(kpi.k2.percent) ? null : kpi.k2.percent >= 100}
            meter={{ percent: kpi.k2.percent, target: 100 }}
          >
            <Note formula="коды, чей статус совпадает с последней принятой операцией">
              {kpi.k2.consistent} из {kpi.k2.total} кодов
            </Note>
          </Stat>
          <Stat
            id="K3"
            label="Идемпотентность импорта"
            value={isNum(k3) ? <Figure value={k3} unit="строк" /> : "—"}
            target="0 строк"
            ok={isNum(k3) ? k3 === 0 : null}
          >
            <Note formula="новых строк при повторном импорте тех же файлов">
              {kpi.k3.last_session_id ? (
                <>
                  сеанс{" "}
                  <Link className="mono" to={`/imports/${encodeURIComponent(kpi.k3.last_session_id)}`}>
                    {shortId(kpi.k3.last_session_id)}
                  </Link>
                </>
              ) : (
                "повторный импорт ещё не выполнялся"
              )}
            </Note>
          </Stat>
          <Stat
            id="K4"
            label="Время отчёта по статусам"
            value={isNum(k4) ? <Figure value={String(k4)} unit="мс" /> : "—"}
            target="≤ 2 с"
            ok={isNum(k4) ? k4 <= 2000 : null}
          >
            <Note formula="время ответа последнего запроса отчёта">
              <Link to="/codes">Отчёт «Коды»</Link>
            </Note>
          </Stat>
          <Stat
            id="K5"
            label="Совпадение с эталоном"
            value={fmtPct(k5)}
            target="100%"
            ok={isNum(k5) ? k5 >= 100 : null}
            meter={{ percent: k5, target: 100 }}
          >
            <Note formula="совпавшие строки status_summary.csv и violations.csv" />
          </Stat>
        </div>
      )}

      <section className="card table-card gd-card" aria-labelledby="gd-title">
        <div className="card-head">
          <div>
            <h2 id="gd-title">Сверка с эталоном</h2>
            <div className="small muted gd-sub">
              Выгрузки status_summary.csv и violations.csv сравниваются построчно с эталонными файлами.
            </div>
          </div>
          <Button id="kpi-golden" variant="primary" onClick={compare} disabled={goldenBusy}>
            {goldenBusy ? "Сверка…" : "Сверить с эталоном"}
          </Button>
        </div>
        <ErrorBox error={goldenError} />
        {!golden && !goldenError && !goldenBusy && (
          <EmptyState>Нажмите «Сверить с эталоном» — результат появится здесь по каждому файлу.</EmptyState>
        )}
        {goldenBusy && !golden && <Loading text="Сверка…" />}
        {golden && (
          <>
            <div className="meta-line gd-summary">
              Совпадение{" "}
              <strong className={golden.percent >= 100 ? undefined : "bad"}>{fmtPct(golden.percent)}</strong>
              {goldenAt && <span> · проверено {fmtTs(goldenAt)}</span>}
            </div>
            <table className="table gd-table">
              <thead>
                <tr>
                  <th>Файл</th>
                  <th>Результат</th>
                  <th className="num">Строк в эталоне</th>
                  <th className="num">Строк в выгрузке</th>
                  <th className="num">Не хватает</th>
                  <th className="num">Лишних</th>
                  <th aria-label="Отличия" />
                </tr>
              </thead>
              <tbody>
                <CompareRow file="status_summary.csv" s={golden.status_summary} />
                <CompareRow file="violations.csv" s={golden.violations} />
              </tbody>
            </table>
          </>
        )}
      </section>
    </div>
  );
}
