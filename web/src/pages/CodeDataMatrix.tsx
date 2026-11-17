import { useState } from "react";
import { DataMatrix } from "../ui";
import { checkCode } from "./codeMask";

/** Saves SVG markup as a file. */
export function downloadSvg(svg: string, name: string) {
  const url = URL.createObjectURL(new Blob([svg], { type: "image/svg+xml" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

/**
 * Data Matrix ECC 200 of a marking code on the code card. The symbol holds exactly the
 * 24-character code (no GS1 FNC1, no crypto tail), so a scanner reads the same string.
 */
export function CodeDataMatrix({ code }: { code: string }) {
  const [svg, setSvg] = useState<string | null>(null);
  const valid = checkCode(code).state === "ok";

  return (
    <figure className="code-dm">
      {valid ? (
        <DataMatrix value={code} size={128} className="code-dm-img" onRendered={setSvg} />
      ) : (
        <div className="code-dm-img code-dm-none">Data Matrix не строится: код не соответствует маске</div>
      )}
      <figcaption>
        <span className="code-dm-title">Data Matrix ECC 200</span>
        <span className="code-dm-sub">строка кода, без криптохвоста</span>
        {valid && svg && (
          <button
            type="button"
            className="btn btn-link code-dm-dl"
            onClick={() => downloadSvg(svg, `datamatrix-${code}.svg`)}
          >
            Скачать SVG
          </button>
        )}
      </figcaption>
    </figure>
  );
}
