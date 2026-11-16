// Live hint for a marking-code input: SPEC §1.2 mask ^01(\d{14})21([A-Za-z0-9]{6})$.
// Purely informational: the form still sends whatever was typed, the backend decides.

export const CODE_LEN = 24;
const MASK = /^01(\d{14})21([A-Za-z0-9]{6})$/;

export interface CodeCheck {
  state: "empty" | "partial" | "ok" | "bad";
  text: string;
  /** GTIN-14 embedded in the code (only when the mask matches). */
  gtin?: string;
}

export function checkCode(code: string): CodeCheck {
  if (!code) return { state: "empty", text: "01 + GTIN-14 + 21 + серийный номер из 6 символов" };
  const m = MASK.exec(code);
  if (m) return { state: "ok", text: `Формат верный · GTIN ${m[1]}`, gtin: m[1] };

  if (/\s/.test(code)) return { state: "bad", text: "Код содержит пробел" };
  const head = code.slice(0, 2);
  if (head !== "01".slice(0, head.length)) return { state: "bad", text: "Код должен начинаться с 01" };
  const gtin = code.slice(2, 16);
  if (!/^\d*$/.test(gtin)) return { state: "bad", text: "Символы 3–16 — GTIN-14, только цифры" };
  const ai = code.slice(16, 18);
  if (ai && ai !== "21".slice(0, ai.length)) return { state: "bad", text: "Символы 17–18 должны быть 21" };
  const serial = code.slice(18);
  if (!/^[A-Za-z0-9]*$/.test(serial)) return { state: "bad", text: "Серийный номер — только латиница и цифры" };
  if (code.length > CODE_LEN) return { state: "bad", text: `Лишних символов: ${code.length - CODE_LEN}` };
  return { state: "partial", text: `Осталось символов: ${CODE_LEN - code.length}` };
}

/** Hint row under a code input: check text on the left, "N / 24" counter on the right. */
export function CodeHint({ code, id }: { code: string; id?: string }) {
  const c = checkCode(code);
  const cls = c.state === "ok" ? "ok" : c.state === "bad" ? "bad" : undefined;
  return (
    <div className="ops-code-hint" id={id} aria-live="polite">
      <span className={cls}>{c.text}</span>
      <span className={`ops-counter${code.length > CODE_LEN ? " bad" : ""}`}>
        {code.length} / {CODE_LEN}
      </span>
    </div>
  );
}

/** GTIN-13 → "0" + gtin13 for comparison (SPEC §1.1). */
export function normGtin(g: string): string {
  return g.length === 13 ? `0${g}` : g;
}
