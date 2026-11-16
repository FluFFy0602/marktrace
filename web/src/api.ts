// All REST API types and calls (SPEC.md §6). Cookie auth, JSON, prefix /api.
// Where SPEC leaves a response shape open, normalisers below accept the likely variants
// so the UI keeps working regardless of minor naming choices in the backend.

export type Role = "A1" | "A2" | "A3" | "A4" | "admin";
export type CodeStatus = "issued" | "introduced" | "sold" | "returned" | "disposed";
export type OpType = "issue" | "introduce" | "sell" | "return" | "dispose";
export type Decision = "accepted" | "rejected" | "duplicate";
export type Source = "items" | "codes" | "operations";

export const STATUSES: CodeStatus[] = ["issued", "introduced", "sold", "returned", "disposed"];
export const OP_TYPES: OpType[] = ["issue", "introduce", "sell", "return", "dispose"];
export const SOURCES: Source[] = ["items", "codes", "operations"];
export const REASONS = [
  "AUTH_DENIED",
  "REQUIRED_FIELD",
  "VALUE_FORMAT",
  "CODE_FORMAT",
  "GTIN_FORMAT",
  "GTIN_CHECKSUM",
  "GTIN_MISMATCH",
  "ITEM_NOT_FOUND",
  "CODE_NOT_FOUND",
  "DUPLICATE_CODE",
  "TRANSITION_NOT_ALLOWED",
  "TIME_FORMAT",
  "TIME_REVERSED",
  "IDEMPOTENCY_CONFLICT",
  "SNAPSHOT_MISMATCH",
  "MOCK_INVALID",
  "MOCK_UNAVAILABLE",
] as const;

export interface User {
  username: string;
  role: Role;
  display_name: string;
}

export interface Item {
  item_code: string;
  name: string;
  gtin: string;
}

export interface CodeRow {
  code: string;
  item_code: string;
  status: CodeStatus;
  last_ts: string;
  last_actor: string;
}

export type StatusCounters = Record<CodeStatus, number>;

export interface CodesResponse {
  rows: CodeRow[];
  total: number;
  counters: StatusCounters;
  elapsed_ms: number;
}

export interface OperationRequest {
  op_type: OpType;
  code: string;
  item_code?: string;
  ts?: string;
  actor?: string;
  op_id?: string;
}

export interface OperationResult {
  attempt_id?: string;
  op_id?: string;
  decision: Decision;
  reason?: string;
  message?: string;
  code?: CodeRow;
}

export interface Operation {
  op_id: string;
  code: string;
  op_type: OpType;
  ts: string;
  actor: string;
  attempt_id?: string | null;
  created_at?: string;
}

export interface Attempt {
  attempt_id: string;
  source: "form" | "import";
  import_row_id?: string | null;
  op_id?: string;
  code?: string;
  op_type?: string;
  ts_raw?: string;
  actor?: string;
  initiator?: string;
  role?: string;
  decision: Decision;
  reason?: string | null;
  message?: string | null;
  mock_result?: unknown;
  rules_version?: string;
  created_at: string;
}

export interface Resolution {
  id: string;
  violation_id?: string;
  action: "comment" | "close";
  comment: string;
  author: string;
  created_at: string;
}

export interface Violation {
  violation_id: string;
  source: Source;
  ref: string;
  code?: string | null;
  item_code?: string | null;
  reason: string;
  message: string;
  attempt_id?: string | null;
  import_row_id?: string | null;
  rules_version?: string;
  created_at: string;
  op_ts?: string | null;
  actor?: string | null;
  state?: "open" | "closed";
  resolutions: Resolution[];
}

export interface ViolationsResponse {
  rows: Violation[];
  total: number;
  counts: Record<string, number>;
}

export interface Registry {
  code: string;
  item_code: string;
  expected_status?: string | null;
  expected_ts?: string | null;
}

export interface CodeDetail {
  code: string;
  current: CodeRow | null;
  registry: Registry | null;
  item: Item | null;
  operations: Operation[];
  attempts: Attempt[];
  violations: Violation[];
}

export interface EntityCounters {
  accepted: number;
  duplicate: number;
  rejected: number;
}

export interface ImportRow {
  row_id?: string;
  entity: Source;
  line_no: number;
  key?: string;
  raw?: unknown;
  processing_status?: string;
  reason?: string | null;
  message?: string | null;
}

export interface ImportSession {
  session_id: string;
  started_by?: string;
  role?: string;
  started_at?: string;
  finished_at?: string | null;
  status: string;
  failure_reason?: string | null;
  files?: unknown;
  counters: Partial<Record<Source, EntityCounters>>;
  rows_created: number | Record<string, number> | null;
  snapshot_violations?: number;
  rejected_rows: ImportRow[];
}

export interface Kpi {
  k1: { accepted: number; rejected: number; percent: number; target: number };
  k2: { consistent: number; total: number; percent: number };
  k3: { last_session_id: string | null; rows_created: number | null };
  k4: { last_ms: number | null };
  k5: { percent: number | null };
}

export interface CompareSection {
  match: boolean;
  expected_rows: number;
  actual_rows: number;
  missing: unknown[];
  extra: unknown[];
}

export interface GoldenCompare {
  status_summary: CompareSection;
  violations: CompareSection;
  percent: number;
}

export interface VerifyResult {
  is_valid: boolean;
  reason?: string;
  [k: string]: unknown;
}

// ---------------------------------------------------------------------------
// transport

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

let unauthorizedHandler: (() => void) | null = null;

/** Registers a callback invoked on any 401 (except the login call itself). */
export function onUnauthorized(fn: () => void) {
  unauthorizedHandler = fn;
}

type Json = Record<string, unknown>;

async function parseBody(res: Response): Promise<unknown> {
  const text = await res.text();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function toError(status: number, body: unknown): ApiError {
  if (body && typeof body === "object") {
    const b = body as Json;
    const code = typeof b.error === "string" ? b.error : String(status);
    const msg = typeof b.message === "string" ? b.message : `Ошибка ${status}`;
    return new ApiError(status, code, msg);
  }
  const fallback =
    status === 0 ? "Сервер недоступен" : typeof body === "string" && body ? body.slice(0, 300) : `Ошибка ${status}`;
  return new ApiError(status, String(status), fallback);
}

interface RawResult {
  status: number;
  body: unknown;
}

async function send(method: string, path: string, body?: unknown, form?: FormData): Promise<RawResult> {
  const init: RequestInit = { method, credentials: "same-origin", headers: {} };
  if (form) {
    init.body = form;
  } else if (body !== undefined) {
    (init.headers as Record<string, string>)["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  let res: Response;
  try {
    res = await fetch(`/api${path}`, init);
  } catch {
    throw new ApiError(0, "NETWORK", "Сервер недоступен");
  }
  const parsed = await parseBody(res);
  if (res.status === 401 && path !== "/auth/login" && path !== "/auth/me") {
    unauthorizedHandler?.();
  }
  return { status: res.status, body: parsed };
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const r = await send(method, path, body);
  if (r.status < 200 || r.status >= 300) throw toError(r.status, r.body);
  return r.body as T;
}

/** Builds a query string from non-empty values. */
export function qs(params: Record<string, string | number | undefined | null>): string {
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === "") continue;
    sp.set(k, String(v));
  }
  const s = sp.toString();
  return s ? `?${s}` : "";
}

// ---------------------------------------------------------------------------
// normalisers for shapes SPEC leaves open

function asArray<T>(v: unknown, ...keys: string[]): T[] {
  if (Array.isArray(v)) return v as T[];
  if (v && typeof v === "object") {
    for (const k of keys) {
      const x = (v as Json)[k];
      if (Array.isArray(x)) return x as T[];
    }
  }
  return [];
}

function num(v: unknown, dflt = 0): number {
  const n = typeof v === "number" ? v : typeof v === "string" ? Number(v) : NaN;
  return Number.isFinite(n) ? n : dflt;
}

function normViolation(v: Json): Violation {
  const resolutions = asArray<Resolution>(v.resolutions);
  let state = v.state as Violation["state"];
  if (state !== "open" && state !== "closed") {
    const closed = v.closed === true || resolutions.some((r) => r.action === "close");
    state = closed ? "closed" : "open";
  }
  return { ...(v as unknown as Violation), resolutions, state };
}

function normCounts(v: unknown): Record<string, number> {
  const out: Record<string, number> = {};
  if (Array.isArray(v)) {
    for (const e of v as Json[]) {
      if (e && typeof e.reason === "string") out[e.reason] = num(e.count);
    }
  } else if (v && typeof v === "object") {
    for (const [k, x] of Object.entries(v as Json)) out[k] = num(x);
  }
  return out;
}

function normCounters(v: unknown): EntityCounters | undefined {
  if (!v || typeof v !== "object") return undefined;
  const o = v as Json;
  if (!("accepted" in o) && !("duplicate" in o) && !("rejected" in o)) return undefined;
  return { accepted: num(o.accepted), duplicate: num(o.duplicate), rejected: num(o.rejected) };
}

function normSession(raw: unknown): ImportSession {
  const s = (raw && typeof raw === "object" ? raw : {}) as Json;
  const src = (s.session && typeof s.session === "object" ? { ...s, ...(s.session as Json) } : s) as Json;
  const box = (src.counters ?? src.entities ?? src.stats ?? src) as Json;
  const counters: ImportSession["counters"] = {};
  for (const e of SOURCES) {
    const c = normCounters(box?.[e]);
    if (c) counters[e] = c;
  }
  let rejected = asArray<ImportRow>(src.rejected_rows ?? src.rejected ?? src.rows);
  rejected = rejected.filter((r) => !r.processing_status || r.processing_status === "rejected");
  let rowsCreated: ImportSession["rows_created"] = null;
  if (typeof src.rows_created === "number") rowsCreated = src.rows_created;
  else if (src.rows_created && typeof src.rows_created === "object")
    rowsCreated = normCounts(src.rows_created);
  return {
    ...(src as unknown as ImportSession),
    session_id: String(src.session_id ?? src.id ?? ""),
    status: String(src.status ?? ""),
    counters,
    rows_created: rowsCreated,
    rejected_rows: rejected,
  };
}

/** Total of rows_created whether it is a number or a per-table map. */
export function rowsCreatedTotal(v: ImportSession["rows_created"]): number | null {
  if (v === null || v === undefined) return null;
  if (typeof v === "number") return v;
  return Object.values(v).reduce((a, b) => a + b, 0);
}

function pickObj(o: Json, ...keys: string[]): Json | null {
  for (const k of keys) {
    const x = o[k];
    if (x && typeof x === "object" && !Array.isArray(x)) return x as Json;
  }
  return null;
}

function normCodeDetail(code: string, raw: unknown): CodeDetail {
  const d = (raw && typeof raw === "object" ? raw : {}) as Json;
  // Current projection: nested object or flat fields on the root.
  let cur = pickObj(d, "code_row", "current", "row", "codes", "code");
  if (!cur && typeof d.status === "string") cur = d;
  let current: CodeRow | null = null;
  if (cur && typeof cur.status === "string") {
    current = {
      code: String(cur.code ?? code),
      item_code: String(cur.item_code ?? ""),
      status: cur.status as CodeStatus,
      last_ts: String(cur.last_ts ?? cur.ts ?? ""),
      last_actor: String(cur.last_actor ?? cur.actor ?? ""),
    };
  }
  const reg = pickObj(d, "registry", "binding", "code_registry");
  const registry: Registry | null = reg
    ? {
        code: String(reg.code ?? code),
        item_code: String(reg.item_code ?? ""),
        expected_status: (reg.expected_status as string | null) ?? null,
        expected_ts: (reg.expected_ts as string | null) ?? null,
      }
    : null;
  const it = pickObj(d, "item");
  return {
    code,
    current,
    registry,
    item: it ? (it as unknown as Item) : null,
    operations: asArray<Operation>(d.operations),
    attempts: asArray<Attempt>(d.attempts),
    violations: asArray<Json>(d.violations).map(normViolation),
  };
}

// ---------------------------------------------------------------------------
// endpoints

export const api = {
  login: (username: string, password: string) =>
    request<unknown>("POST", "/auth/login", { username, password }).then((b) => {
      const o = (b ?? {}) as Json;
      return (o.user && typeof o.user === "object" ? o.user : o) as User;
    }),
  logout: () => request<unknown>("POST", "/auth/logout"),
  me: () => request<User>("GET", "/auth/me"),

  items: () => request<unknown>("GET", "/items").then((b) => asArray<Item>(b, "rows", "items")),

  /** Rejections come back as a decision body (possibly with a non-2xx status); other errors throw. */
  async operation(req: OperationRequest): Promise<OperationResult> {
    const r = await send("POST", "/operations", req);
    const b = r.body as Json | null;
    if (b && typeof b === "object" && typeof b.decision === "string") return b as unknown as OperationResult;
    if (r.status >= 200 && r.status < 300) {
      throw new ApiError(r.status, "BAD_RESPONSE", "Неожиданный ответ сервера");
    }
    throw toError(r.status, r.body);
  },

  codes: (params: Record<string, string | number | undefined>) =>
    request<CodesResponse>("GET", `/codes${qs(params)}`).then((b) => ({
      rows: asArray<CodeRow>(b?.rows),
      total: num(b?.total),
      counters: Object.fromEntries(STATUSES.map((s) => [s, num(b?.counters?.[s])])) as StatusCounters,
      elapsed_ms: num(b?.elapsed_ms),
    })),
  code: (code: string) =>
    request<unknown>("GET", `/codes/${encodeURIComponent(code)}`).then((b) => normCodeDetail(code, b)),

  violations: (params: Record<string, string | number | undefined>) =>
    request<unknown>("GET", `/violations${qs(params)}`).then((raw) => {
      const b = (raw ?? {}) as Json;
      const rows = asArray<Json>(b, "rows", "violations").map(normViolation);
      return {
        rows,
        total: num(b.total, rows.length),
        counts: normCounts(b.counts ?? b.counts_by_reason ?? b.by_reason ?? b.reasons),
      } as ViolationsResponse;
    }),
  resolve: (id: string, action: "comment" | "close", comment: string) =>
    request<unknown>("POST", `/violations/${encodeURIComponent(id)}/resolutions`, { action, comment }),

  kpi: () => request<Kpi>("GET", "/kpi"),

  async importFiles(form: FormData): Promise<ImportSession> {
    const r = await send("POST", "/imports", undefined, form);
    const b = r.body as Json | null;
    // A structure_rejected session may be reported with a 4xx status but still carry the session.
    if (b && typeof b === "object" && (b.session_id || (b.session as Json | undefined)?.session_id)) {
      return normSession(b);
    }
    if (r.status >= 200 && r.status < 300) return normSession(b);
    throw toError(r.status, r.body);
  },
  imports: () =>
    request<unknown>("GET", "/imports").then((b) => asArray<unknown>(b, "rows", "sessions", "imports").map(normSession)),
  importDetail: (id: string) =>
    request<unknown>("GET", `/imports/${encodeURIComponent(id)}`).then(normSession),

  golden: () => request<GoldenCompare>("GET", "/golden/compare"),

  verify: (code: string, gtin?: string) =>
    request<VerifyResult>("POST", "/verify", gtin ? { code, gtin } : { code }),
};
