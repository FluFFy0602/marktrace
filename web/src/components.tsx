import { Link, useLocation } from "react-router-dom";
import { ApiError, type CodeStatus } from "./api";
import { DECISION_LABEL, REASON_LABEL, SESSION_STATUS_LABEL, STATUS_LABEL } from "./labels";

/** Code status as a coloured dot + Russian label. */
export function StatusBadge({ status }: { status: string | null | undefined }) {
  if (!status) return <span className="faint">—</span>;
  const label = STATUS_LABEL[status as CodeStatus] ?? status;
  return (
    <span className={`status st-${status}`} title={status}>
      {label}
    </span>
  );
}

/** Reason code: monospace quiet tag, Russian meaning in the tooltip; the marker colour shows the group. */
export function ReasonBadge({ reason }: { reason: string | null | undefined }) {
  if (!reason) return <span className="faint">—</span>;
  return (
    <span className={`badge reason r-${reasonGroup(reason)}`} title={REASON_LABEL[reason] ?? reason}>
      {reason}
    </span>
  );
}

export function reasonGroup(r: string): "mock" | "format" | "snapshot" | "auth" | "logic" {
  if (r.startsWith("MOCK")) return "mock";
  if (r.startsWith("GTIN") || r === "CODE_FORMAT" || r === "VALUE_FORMAT" || r === "TIME_FORMAT" || r === "REQUIRED_FIELD")
    return "format";
  if (r === "SNAPSHOT_MISMATCH") return "snapshot";
  if (r === "AUTH_DENIED") return "auth";
  return "logic";
}

/** Operation decision: quiet tinted badge (accepted / rejected / duplicate). */
export function DecisionBadge({ decision }: { decision: string | null | undefined }) {
  if (!decision) return <span className="faint">—</span>;
  return <span className={`badge d-${decision}`}>{DECISION_LABEL[decision] ?? decision}</span>;
}

/** Import session status as dot + label. */
export function SessionStatusBadge({ status }: { status: string }) {
  return <span className={`status ss-${status}`}>{SESSION_STATUS_LABEL[status] ?? status}</span>;
}

/** Violation state as dot + label. */
export function StateBadge({ state }: { state: string | null | undefined }) {
  const closed = state === "closed";
  return <span className={`status ${closed ? "state-closed" : "state-open"}`}>{closed ? "Закрыто" : "Открыто"}</span>;
}

/** Where a detail page's "back" link should return to (list URL with its filters). */
export interface BackState {
  from?: string;
}

export function CodeLink({ code }: { code: string | null | undefined }) {
  const loc = useLocation();
  if (!code) return <span className="faint">—</span>;
  // Remember the list URL (with filters and offset) so the code page can link back to it.
  const state: BackState = { from: loc.pathname + loc.search };
  return (
    <Link className="mono" to={`/codes/${encodeURIComponent(code)}`} state={state}>
      {code}
    </Link>
  );
}

/** Back target from navigation state if it points under `prefix`, else `fallback`. */
export function useBackTo(prefix: string, fallback: string): string {
  const st = useLocation().state as BackState | null;
  const from = st?.from;
  return typeof from === "string" && (from === prefix || from.startsWith(prefix + "?")) ? from : fallback;
}

export function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null;
  const msg = error instanceof ApiError ? error.message : error instanceof Error ? error.message : String(error);
  const code = error instanceof ApiError ? error.code : null;
  return (
    <div className="alert alert-error" role="alert">
      {code && <span className="mono">{code}</span>}
      {code && " · "}
      {msg}
    </div>
  );
}

export function Loading({ text = "Загрузка…" }: { text?: string }) {
  return <div className="loading">{text}</div>;
}

export function Pager({
  total,
  limit,
  offset,
  onChange,
}: {
  total: number;
  limit: number;
  offset: number;
  onChange: (offset: number) => void;
}) {
  const page = Math.floor(offset / limit) + 1;
  const pages = Math.max(1, Math.ceil(total / limit));
  const first = total === 0 ? 0 : offset + 1;
  const last = Math.min(total, offset + limit);
  return (
    <div className="pager">
      <span>
        {first}–{last} из {total}
      </span>
      <div className="pager-nav">
        <span>
          Стр. {page} из {pages}
        </span>
        <button
          type="button"
          className="btn btn-sm"
          disabled={offset <= 0}
          onClick={() => onChange(Math.max(0, offset - limit))}
        >
          Назад
        </button>
        <button
          type="button"
          className="btn btn-sm"
          disabled={offset + limit >= total}
          onClick={() => onChange(offset + limit)}
        >
          Далее
        </button>
      </div>
    </div>
  );
}
