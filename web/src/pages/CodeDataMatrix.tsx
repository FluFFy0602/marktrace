import { useEffect, useState } from "react";

// Marking code mask (SPEC §1.2): 01 + GTIN-14 + 21 + 6-char serial.
const MASK = /^01(\d{14})21([A-Za-z0-9]{6})$/;

/** GS1 element string with AIs in brackets, or null when the code does not match the mask. */
export function gs1Text(code: string): string | null {
  const m = MASK.exec(code);
  return m ? `(01)${m[1]}(21)${m[2]}` : null;
}

type State = { svg: string } | { error: string } | null;

function reasonOf(err: unknown): string {
  const msg = err instanceof Error ? err.message : String(err);
  if (/check ?(digit|sum)/i.test(msg)) return "неверная контрольная цифра GTIN";
  return "код не проходит проверку GS1";
}

/**
 * GS1 DataMatrix of a marking code, as printed on a «Честный знак» label but without the
 * crypto tail (AI 91/92): the training codes have none. The encoder is loaded on demand.
 */
export function CodeDataMatrix({ code }: { code: string }) {
  const text = gs1Text(code);
  const [state, setState] = useState<State>(null);

  useEffect(() => {
    setState(null);
    if (!text) return;
    let alive = true;
    import("bwip-js/browser")
      .then(({ gs1datamatrix, drawingSVG }) => {
        if (!alive) return;
        try {
          setState({
            svg: gs1datamatrix(
              { bcid: "gs1datamatrix", text, scale: 4, paddingwidth: 0, paddingheight: 0 },
              drawingSVG(),
            ),
          });
        } catch (e) {
          setState({ error: reasonOf(e) });
        }
      })
      .catch(() => alive && setState({ error: "не удалось загрузить генератор" }));
    return () => {
      alive = false;
    };
  }, [text]);

  function download() {
    if (!state || !("svg" in state)) return;
    const url = URL.createObjectURL(new Blob([state.svg], { type: "image/svg+xml" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `datamatrix-${code}.svg`;
    a.click();
    URL.revokeObjectURL(url);
  }

  const error = !text ? "код не соответствует маске" : state && "error" in state ? state.error : null;

  return (
    <figure className="code-dm">
      {error ? (
        <div className="code-dm-img code-dm-none">DataMatrix не строится: {error}</div>
      ) : (
        <div
          className={`code-dm-img${state ? "" : " is-loading"}`}
          role="img"
          aria-label={`GS1 DataMatrix кода ${code}`}
          title={text ?? undefined}
          // The SVG is produced locally by the encoder from a mask-checked string.
          dangerouslySetInnerHTML={state && "svg" in state ? { __html: state.svg } : undefined}
        />
      )}
      <figcaption>
        <span className="code-dm-title">GS1 DataMatrix</span>
        <span className="code-dm-sub">без криптохвоста</span>
        {state && "svg" in state && (
          <button type="button" className="btn btn-link code-dm-dl" onClick={download}>
            Скачать SVG
          </button>
        )}
      </figcaption>
    </figure>
  );
}
