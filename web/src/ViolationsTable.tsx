import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { api, type Violation } from "./api";
import { canResolve, useAuth } from "./auth";
import { CodeLink, ErrorBox, ReasonBadge, StateBadge } from "./components";
import { fmtTs, REASON_LABEL, SOURCE_LABEL } from "./labels";
import { Button, EmptyRow, useClickOutside, usePlacement } from "./ui";
import "./pages/ops.css";

/** "16.11 19:22" in local time; the full timestamp goes into the tooltip. */
function fmtShort(v: string): string {
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  return d.toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" });
}

/** Date on the first line, time muted on the second: keeps the column narrow. */
function CreatedAt({ ts }: { ts: string }) {
  const s = fmtTs(ts);
  const i = s.indexOf(", ");
  if (i < 0) return <>{s}</>;
  return (
    <>
      <div>{s.slice(0, i)}</div>
      <div className="vt-meta">{s.slice(i + 2)}</div>
    </>
  );
}

/** Violations table; A3/admin get inline "Комментарий" / "Закрыть" with a small anchored form. */
export default function ViolationsTable({
  rows,
  onChanged,
  showCode = true,
  emptyText = "Расхождений по заданным фильтрам нет.",
}: {
  rows: Violation[];
  onChanged: () => void;
  showCode?: boolean;
  /** Sentence for the empty row. */
  emptyText?: string;
}) {
  const { user } = useAuth();
  const resolver = canResolve(user?.role);
  const cols = 5 + (showCode ? 1 : 0);
  return (
    <table className="table vt">
      <thead>
        <tr>
          <th>Причина</th>
          {showCode && <th>Код</th>}
          <th>Источник / ref</th>
          <th>Сообщение</th>
          <th>Создано</th>
          <th>Состояние и резолюции</th>
        </tr>
      </thead>
      <tbody>
        {rows.length === 0 && <EmptyRow colSpan={cols}>{emptyText}</EmptyRow>}
        {rows.map((v) => (
          <ViolationRow key={v.violation_id} v={v} showCode={showCode} resolver={resolver} onChanged={onChanged} />
        ))}
      </tbody>
    </table>
  );
}

function ViolationRow({
  v,
  showCode,
  resolver,
  onChanged,
}: {
  v: Violation;
  showCode: boolean;
  resolver: boolean;
  onChanged: () => void;
}) {
  const closed = v.state === "closed";
  return (
    <tr className={closed ? "row-closed" : ""}>
      <td>
        <ReasonBadge reason={v.reason} />
        <span className="vt-reason-label">{REASON_LABEL[v.reason] ?? ""}</span>
      </td>
      {showCode && (
        <td className="code-cell">
          <CodeLink code={v.code} />
        </td>
      )}
      <td className="nowrap">
        <div>{SOURCE_LABEL[v.source] ?? v.source}</div>
        {/* For source "codes" the ref is the code itself, already shown in the code column. */}
        {v.ref && !(showCode && v.ref === v.code) && (
          <span className="vt-ref" title={v.ref}>
            {v.ref}
          </span>
        )}
      </td>
      <td className="msg">
        {v.message}
        {(v.actor || v.op_ts) && (
          <div className="vt-meta">
            {[v.actor && `исполнитель: ${v.actor}`, v.op_ts && `время операции: ${fmtTs(v.op_ts)}`]
              .filter(Boolean)
              .join(" · ")}
          </div>
        )}
      </td>
      <td className="nowrap" title={v.created_at}>
        <CreatedAt ts={v.created_at} />
      </td>
      <td className="vt-actions">
        <div className="vt-state">
          <StateBadge state={v.state} />
        </div>
        {v.resolutions.length > 0 && (
          <ul className="vt-res">
            {v.resolutions.map((r) => (
              <li key={r.id}>
                <div className="vt-res-meta">
                  <span>
                    <span className={`dot dot-${r.action === "close" ? "ok" : "neutral"}`} />
                    {r.action === "close" ? "Закрыто" : "Комментарий"} · {r.author}
                  </span>
                  <span className="vt-res-time" title={fmtTs(r.created_at)}>
                    {fmtShort(r.created_at)}
                  </span>
                </div>
                {r.comment && <div className="vt-res-text">{r.comment}</div>}
              </li>
            ))}
          </ul>
        )}
        {resolver && <ResolveActions v={v} closed={closed} onChanged={onChanged} />}
      </td>
    </tr>
  );
}

/** "Комментарий" / "Закрыть" buttons; each opens a small popover with a required comment. */
function ResolveActions({ v, closed, onChanged }: { v: Violation; closed: boolean; onChanged: () => void }) {
  const [mode, setMode] = useState<"comment" | "close" | null>(null);
  const [comment, setComment] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const anchorRef = useRef<HTMLDivElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  const areaRef = useRef<HTMLTextAreaElement>(null);
  const openerRef = useRef<HTMLButtonElement | null>(null);
  const open = mode !== null;
  const place = usePlacement(anchorRef, popRef, open);

  function close(restoreFocus = true) {
    setMode(null);
    setError(null);
    if (restoreFocus) openerRef.current?.focus();
  }
  useClickOutside([anchorRef], () => close(false), open);
  useEffect(() => {
    if (open) areaRef.current?.focus();
  }, [open]);

  function start(m: "comment" | "close", btn: HTMLButtonElement) {
    openerRef.current = btn;
    setError(null);
    setMode((cur) => (cur === m ? null : m));
  }

  async function submit() {
    if (!mode || !comment.trim()) return;
    setBusy(true);
    setError(null);
    try {
      await api.resolve(v.violation_id, mode, comment.trim());
      setComment("");
      setMode(null);
      onChanged();
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.stopPropagation();
      close();
    } else if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      void submit();
    }
  }

  const popId = `vt-pop-${v.violation_id}`;
  return (
    <div className="vt-buttons">
      <div className="vt-anchor" ref={anchorRef}>
        <Button
          size="sm"
          aria-expanded={mode === "comment"}
          aria-controls={mode === "comment" ? popId : undefined}
          onClick={(e) => start("comment", e.currentTarget)}
        >
          Комментарий
        </Button>
        {!closed && (
          <Button
            size="sm"
            aria-expanded={mode === "close"}
            aria-controls={mode === "close" ? popId : undefined}
            onClick={(e) => start("close", e.currentTarget)}
          >
            Закрыть
          </Button>
        )}
        {open && (
          <div
            ref={popRef}
            id={popId}
            role="dialog"
            aria-label={mode === "close" ? "Закрыть расхождение" : "Добавить комментарий"}
            className={`popover vt-pop${place.up ? " is-up" : ""}${place.end ? " is-end" : ""}`}
            onKeyDown={onKeyDown}
          >
            <div className="vt-pop-title">
              {mode === "close" ? "Закрыть расхождение" : "Комментарий к расхождению"}
            </div>
            <textarea
              ref={areaRef}
              rows={3}
              value={comment}
              onChange={(e) => setComment(e.target.value)}
              placeholder={mode === "close" ? "Причина закрытия (обязательно)" : "Текст комментария (обязательно)"}
              aria-label="Комментарий"
            />
            <ErrorBox error={error} />
            <div className="vt-pop-actions">
              <Button
                size="sm"
                variant="primary"
                disabled={busy || !comment.trim()}
                onClick={() => void submit()}
              >
                {busy ? "Сохранение…" : mode === "close" ? "Закрыть" : "Добавить"}
              </Button>
              <Button size="sm" variant="ghost" onClick={() => close()}>
                Отмена
              </Button>
              <span className="vt-pop-key">Ctrl+Enter</span>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
