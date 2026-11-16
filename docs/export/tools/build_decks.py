"""Build the checkpoint decks from docs/32-presentations.md with pandoc.

Usage: python3 build_decks.py <32-presentations.md> <reference.pptx> <out_dir>

Rules (see section 0 of 32-presentations.md):
- a deck is an H2 section with a "**Файл:**" line; "**Тема:**" becomes the deck title;
- a slide is an H3 "### Слайд N. Title";
- a slide shows its illustration (png, else table, else code) large and alone,
  otherwise its bullets; whatever is not shown goes to the speaker notes;
- slide 1 is the title slide, its bullets are the authors.
"""
import json
import re
import subprocess
import sys
import tempfile
import zipfile
from pathlib import Path

SRC = Path(sys.argv[1]).resolve()
REF = Path(sys.argv[2]).resolve()
OUT = Path(sys.argv[3]).resolve()
DOCS = SRC.parent

SUBTITLE = "MarkTrace · кейс 16 «Маркировка „Честный знак“ (макет)» · команда [номер команды]"

# Table geometry in EMU for the 24384000 x 13716000 MIREA slide.
ROW_H = 860000
TABLE_FONT = 2600
CODE_FONT = 2400
INLINE_CODE_FONT = 3000
# A monospace run without an explicit size (runs that already have sz= are skipped).
MONO_RPR = r'<a:rPr((?: (?!sz=)[\w:]+="[^"]*")*) ?>(<a:latin typeface="Courier)'


def parse(md: str):
    decks = []
    for sec in re.split(r"(?m)^(?=## )", md):
        m = re.search(r"(?m)^\*\*Файл:\*\* `export/([^`]+)`", sec)
        if not m:
            continue
        theme = re.search(r"(?m)^\*\*Тема:\*\* (.+)$", sec).group(1).strip()
        slides = []
        for part in re.split(r"(?m)^(?=### )", sec):
            h = re.match(r"### Слайд (\d+)\. (.+)", part)
            if not h:
                continue
            slides.append(parse_slide(int(h.group(1)), h.group(2).strip(), part))
        decks.append({"file": m.group(1), "theme": theme, "slides": slides})
    return decks


def parse_slide(num, title, text):
    lines = text.splitlines()[1:]
    s = {"num": num, "title": title, "bullets": [], "table": [], "code": [], "image": None, "speech": ""}
    in_code = False
    for ln in lines:
        if ln.startswith("```"):
            if in_code:
                s["code"].append(ln)
                in_code = False
            else:
                in_code = True
                s["code"].append(ln)
            continue
        if in_code:
            s["code"].append(ln)
        elif ln.startswith("- "):
            s["bullets"].append(ln[2:].strip())
        elif ln.startswith("|"):
            s["table"].append(ln)
        elif ln.startswith("**Иллюстрация:**"):
            m = re.search(r"`([^`]+\.png)`", ln)
            if m:
                s["image"] = m.group(1)
        elif ln.startswith("**Говорит:**"):
            s["speech"] = ln[len("**Говорит:**"):].strip()
    return s


def slide_md(s):
    out = [f"# {s['title']}", ""]
    shown_bullets = False
    if s["image"]:
        path = DOCS / s["image"]
        if not path.exists():
            raise SystemExit(f"missing illustration {path}")
        out += [f"![]({path})", ""]
    elif s["table"]:
        out += s["table"] + [""]
    elif s["code"]:
        out += s["code"] + [""]
    else:
        out += [f"- {b}" for b in s["bullets"]] + [""]
        shown_bullets = True
    notes = []
    if not shown_bullets and s["bullets"]:
        notes += ["Тезисы:", ""] + [f"- {b}" for b in s["bullets"]] + [""]
    if s["speech"] and not s["speech"].startswith("—"):
        notes += [f"Говорит: {s['speech']}", ""]
    if notes:
        out += ["::: notes"] + notes + [":::", ""]
    return "\n".join(out)


def deck_md(deck):
    first, rest = deck["slides"][0], deck["slides"][1:]
    meta = ["---", f"title: {json.dumps(deck['theme'], ensure_ascii=False)}",
            f"subtitle: {json.dumps(SUBTITLE, ensure_ascii=False)}", "author:"]
    meta += [f"  - {json.dumps(a, ensure_ascii=False)}" for a in first["bullets"]]
    meta += ["---", ""]
    return "\n".join(meta) + "\n".join(slide_md(s) for s in rest)


def cell_text(tc):
    return "".join(re.findall(r"<a:t>([^<]*)</a:t>", tc))


def fix_table(m):
    """Proportional column widths, compact rows and a readable font size."""
    frame = m.group(0)
    total = sum(int(w) for w in re.findall(r'<a:gridCol w="(\d+)"', frame))
    rows = re.findall(r"<a:tr .*?</a:tr>", frame, flags=re.S)
    cols = len(re.findall(r"<a:gridCol ", frame))
    lens = [6] * cols
    for r in rows:
        for i, tc in enumerate(re.findall(r"<a:tc>.*?</a:tc>", r, flags=re.S)[:cols]):
            lens[i] = max(lens[i], min(len(cell_text(tc)), 70))
    widths = [int(total * ln / sum(lens)) for ln in lens]
    it = iter(widths)
    frame = re.sub(r'<a:gridCol w="\d+"', lambda _: f'<a:gridCol w="{next(it)}"', frame)
    frame = re.sub(r'<a:tr h="\d+">', f'<a:tr h="{ROW_H}">', frame)
    frame = frame.replace("<a:rPr />", f'<a:rPr lang="ru-RU" sz="{TABLE_FONT}"><a:latin typeface="Arial" />'
                                       f'<a:cs typeface="Arial" /></a:rPr>')
    frame = frame.replace('<a:pPr lvl="0" indent="0" marL="0">', '<a:pPr lvl="0" indent="0" marL="0" algn="l">')
    frame = re.sub(MONO_RPR, lambda mm: f'<a:rPr sz="{TABLE_FONT}"{mm.group(1)}>{mm.group(2)}', frame)
    frame = re.sub(r'(<p:xfrm><a:off x="\d+" y="\d+" /><a:ext cx="\d+" cy=")\d+(")',
                   lambda mm: f"{mm.group(1)}{ROW_H * len(rows)}{mm.group(2)}", frame)
    return frame


def postprocess(pptx: Path, code_slides=frozenset()):
    """code_slides: 1-based slide numbers whose content is a code block."""
    tmp = pptx.with_suffix(".tmp")
    with zipfile.ZipFile(pptx) as zin, zipfile.ZipFile(tmp, "w", zipfile.ZIP_DEFLATED) as zout:
        for item in zin.infolist():
            data = zin.read(item.filename)
            m = re.match(r"ppt/slides/slide(\d+)\.xml$", item.filename)
            if m:
                s = data.decode("utf-8")
                s = re.sub(r"<p:graphicFrame>.*?</p:graphicFrame>", fix_table, s, flags=re.S)
                # Monospace runs get no size from pandoc: JSON blocks would be too big,
                # inline code in bullets too small.
                size = CODE_FONT if int(m.group(1)) in code_slides else INLINE_CODE_FONT
                s = re.sub(MONO_RPR, lambda mm: f'<a:rPr sz="{size}"{mm.group(1)}>{mm.group(2)}', s)
                data = s.encode("utf-8")
            zout.writestr(item, data)
    tmp.replace(pptx)


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    for deck in parse(SRC.read_text(encoding="utf-8")):
        with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False, encoding="utf-8") as f:
            f.write(deck_md(deck))
            md_path = f.name
        target = OUT / deck["file"]
        subprocess.run(["pandoc", md_path, "-o", str(target), f"--reference-doc={REF}",
                        f"--resource-path={DOCS}"], check=True)
        code_slides = {s["num"] for s in deck["slides"] if s["code"] and not s["image"] and not s["table"]}
        postprocess(target, code_slides)
        print(f"{target.name}: {len(deck['slides'])} slides")


if __name__ == "__main__":
    main()
