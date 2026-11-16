// Russian labels and small formatting helpers shared by pages.
import type { CodeStatus, OpType, Role } from "./api";

export const STATUS_LABEL: Record<CodeStatus, string> = {
  issued: "Эмитирован",
  introduced: "В обороте",
  sold: "Продан",
  returned: "Возвращён",
  disposed: "Выбыл",
};

export const OP_LABEL: Record<OpType, string> = {
  issue: "Эмиссия (issue)",
  introduce: "Ввод в оборот (introduce)",
  sell: "Продажа (sell)",
  return: "Возврат (return)",
  dispose: "Выбытие (dispose)",
};

export const ROLE_LABEL: Record<Role, string> = {
  A1: "A1 · Производство",
  A2: "A2 · Розница",
  A3: "A3 · Контроль качества",
  A4: "A4 · Аудитор",
  admin: "Администратор",
};

export const REASON_LABEL: Record<string, string> = {
  AUTH_DENIED: "Нет прав",
  REQUIRED_FIELD: "Пустое поле",
  VALUE_FORMAT: "Неверное значение",
  CODE_FORMAT: "Формат кода",
  GTIN_FORMAT: "Формат GTIN",
  GTIN_CHECKSUM: "Контрольная цифра GTIN",
  GTIN_MISMATCH: "GTIN не совпадает",
  ITEM_NOT_FOUND: "Товар не найден",
  CODE_NOT_FOUND: "Код не найден",
  DUPLICATE_CODE: "Повтор кода",
  TRANSITION_NOT_ALLOWED: "Недопустимый переход",
  TIME_FORMAT: "Формат времени",
  TIME_REVERSED: "Нарушен порядок времени",
  IDEMPOTENCY_CONFLICT: "Конфликт идемпотентности",
  SNAPSHOT_MISMATCH: "Расхождение со снимком",
  MOCK_INVALID: "ЦРПТ: код недействителен",
  MOCK_UNAVAILABLE: "ЦРПТ недоступен",
};

export const SOURCE_LABEL: Record<string, string> = {
  items: "Товары",
  codes: "Коды",
  operations: "Операции",
  form: "Форма",
  import: "Импорт",
};

export const DECISION_LABEL: Record<string, string> = {
  accepted: "Принято",
  rejected: "Отклонено",
  duplicate: "Повтор",
};

export const SESSION_STATUS_LABEL: Record<string, string> = {
  running: "Выполняется",
  completed: "Завершён",
  structure_rejected: "Отклонён (структура)",
  failed: "Ошибка",
};

/** Formats an API timestamp (UTC) in the browser's local time; falls back to the raw value. */
export function fmtTs(v: string | null | undefined): string {
  if (!v) return "—";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  return d.toLocaleString("ru-RU", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

/**
 * Converts a datetime-local input value ("YYYY-MM-DDTHH:mm[:ss]") into RFC 3339 with the
 * browser offset, seconds precision (SPEC §1.3). Empty input → empty string.
 */
export function localToRfc3339(v: string): string {
  if (!v) return "";
  const base = v.length === 16 ? `${v}:00` : v.slice(0, 19);
  const d = new Date(base);
  if (Number.isNaN(d.getTime())) return "";
  const offMin = -d.getTimezoneOffset();
  const sign = offMin >= 0 ? "+" : "-";
  const abs = Math.abs(offMin);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${base}${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`;
}

/** Current local time in datetime-local format with seconds. */
export function nowLocal(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(
    d.getMinutes(),
  )}:${pad(d.getSeconds())}`;
}

export function fmtPct(v: number | null | undefined): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return "—";
  return `${v.toFixed(2)}%`;
}

export function shortId(id: string | null | undefined): string {
  if (!id) return "—";
  return id.length > 8 ? id.slice(0, 8) : id;
}
