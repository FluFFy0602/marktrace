import { useEffect, useState } from "react";

type Encoder = Pick<typeof import("bwip-js/browser"), "datamatrix" | "drawingSVG">;

// The encoder is loaded once, on the first symbol shown. Destructuring inside then() lets the
// bundler keep only the Data Matrix encoder instead of every barcode type (~230 KB vs ~970 KB).
let encoder: Promise<Encoder> | null = null;
const loadEncoder = () =>
  (encoder ??= import("bwip-js/browser").then(({ datamatrix, drawingSVG }) => ({ datamatrix, drawingSVG })));

/**
 * Renders a Data Matrix ECC 200 symbol to SVG markup. The value is encoded byte for byte
 * (no GS1 FNC1, no escapes), so a scanner reads back exactly the same string.
 */
export async function dataMatrixSvg(value: string): Promise<string> {
  const { datamatrix, drawingSVG } = await loadEncoder();
  return datamatrix({ bcid: "datamatrix", text: value, scale: 4, paddingwidth: 0, paddingheight: 0 }, drawingSVG());
}

export interface DataMatrixProps {
  value: string;
  /** Edge of the square in px; the symbol scales to fit. */
  size?: number;
  className?: string;
  /** Called with the SVG markup once rendered (e.g. for a download button). */
  onRendered?: (svg: string) => void;
}

/** Data Matrix ECC 200 of `value`, drawn as inline SVG. */
export function DataMatrix({ value, size = 128, className = "", onRendered }: DataMatrixProps) {
  const [svg, setSvg] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let alive = true;
    setSvg(null);
    setFailed(false);
    dataMatrixSvg(value)
      .then((s) => {
        if (!alive) return;
        setSvg(s);
        onRendered?.(s);
      })
      .catch(() => alive && setFailed(true));
    return () => {
      alive = false;
    };
    // onRendered is a notification only; re-rendering the symbol for a new callback is pointless.
  }, [value]);

  return (
    <div
      className={`datamatrix${svg ? "" : " is-loading"}${failed ? " is-failed" : ""} ${className}`.trim()}
      style={{ width: size, height: size }}
      role="img"
      aria-label={`Data Matrix: ${value}`}
      title={value}
      // The markup comes from the local encoder; the value never reaches it as markup.
      dangerouslySetInnerHTML={svg ? { __html: svg } : undefined}
    >
      {failed ? "Не удалось построить Data Matrix" : undefined}
    </div>
  );
}
