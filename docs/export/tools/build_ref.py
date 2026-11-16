"""Derive a pandoc reference doc from the MIREA example deck.

Usage: python3 build_ref.py <MIREA example .pptx> <out reference .pptx>

Pandoc looks layouts up by name ("Title Slide", "Title and Content", ...). The MIREA
example uses Keynote layout names and draws its look with per-slide shapes, so the
layouts pandoc needs are rewritten here and carry the look themselves: a blue top bar
with the MIREA logo on content slides and the logo panel on the title slide.
The example deck itself is only read.
"""
import re
import sys
import tempfile
import zipfile
from pathlib import Path

SRC = Path(sys.argv[1])
OUT = Path(sys.argv[2])

NS = ('xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" '
      'xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" '
      'xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"')
W, H = 24384000, 13716000  # slide size of the MIREA example (EMU)
BLUE, NAVY, ACCENT = "5B9BD5", "0B2D50", "2F74B9"
FONT = "Arial"
BAR = 1600000
LOGO_H = 1300000
LOGO_W = int(LOGO_H * 608 / 426)  # media/image2.png is 608 x 426


def rpr(sz, color, bold=False):
    b = ' b="1"' if bold else ' b="0"'
    return (f'<a:defRPr sz="{sz}"{b}><a:solidFill><a:srgbClr val="{color}"/></a:solidFill>'
            f'<a:latin typeface="{FONT}"/><a:ea typeface="{FONT}"/><a:cs typeface="{FONT}"/></a:defRPr>')


def rect(id_, name, x, y, cx, cy, color):
    return (f'<p:sp><p:nvSpPr><p:cNvPr id="{id_}" name="{name}"/><p:cNvSpPr/><p:nvPr userDrawn="1"/></p:nvSpPr>'
            f'<p:spPr><a:xfrm><a:off x="{x}" y="{y}"/><a:ext cx="{cx}" cy="{cy}"/></a:xfrm>'
            f'<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="{color}"/></a:solidFill>'
            f'<a:ln><a:noFill/></a:ln></p:spPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="ru-RU"/></a:p></p:txBody></p:sp>')


def pic(id_, name, rid, x, y, cx, cy):
    return (f'<p:pic><p:nvPicPr><p:cNvPr id="{id_}" name="{name}"/><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr>'
            f'<p:nvPr userDrawn="1"/></p:nvPicPr><p:blipFill><a:blip r:embed="{rid}"/><a:stretch><a:fillRect/></a:stretch></p:blipFill>'
            f'<p:spPr><a:xfrm><a:off x="{x}" y="{y}"/><a:ext cx="{cx}" cy="{cy}"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>')


def text(id_, name, x, y, cx, cy, lines, sz, color):
    paras = "".join(
        f'<a:p><a:pPr algn="ctr"/><a:r><a:rPr lang="ru-RU" sz="{sz}" b="1"><a:solidFill><a:srgbClr val="{color}"/></a:solidFill>'
        f'<a:latin typeface="{FONT}"/><a:cs typeface="{FONT}"/></a:rPr><a:t>{t}</a:t></a:r></a:p>' for t in lines)
    return (f'<p:sp><p:nvSpPr><p:cNvPr id="{id_}" name="{name}"/><p:cNvSpPr txBox="1"/><p:nvPr userDrawn="1"/></p:nvSpPr>'
            f'<p:spPr><a:xfrm><a:off x="{x}" y="{y}"/><a:ext cx="{cx}" cy="{cy}"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>'
            f'<p:txBody><a:bodyPr wrap="square" anchor="ctr"/><a:lstStyle/>{paras}</p:txBody></p:sp>')


def ph(id_, name, phattrs, x, y, cx, cy, lst, anchor="t"):
    return (f'<p:sp><p:nvSpPr><p:cNvPr id="{id_}" name="{name}"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr>'
            f'<p:nvPr><p:ph {phattrs}/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="{x}" y="{y}"/><a:ext cx="{cx}" cy="{cy}"/></a:xfrm>'
            f'<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr anchor="{anchor}"><a:normAutofit/></a:bodyPr>'
            f'<a:lstStyle>{lst}</a:lstStyle><a:p><a:endParaRPr lang="ru-RU"/></a:p></p:txBody></p:sp>')


SLDNUM = ('<p:sp><p:nvSpPr><p:cNvPr id="9" name="Slide Number"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr>'
          '<p:nvPr><p:ph type="sldNum" sz="quarter" idx="2"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/>'
          '<a:p><a:fld id="{86CB4B4D-7CA3-9044-876B-883B54F8677D}" type="slidenum"><a:t>‹#›</a:t></a:fld></a:p></p:txBody></p:sp>')
GRP = ('<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/>'
       '<a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>')
NOTES_NUM = ('<p:sp><p:nvSpPr><p:cNvPr id="90" name="Slide Number Placeholder"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr>'
             '<p:nvPr><p:ph type="sldNum" sz="quarter" idx="5"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="3884613" y="8685213"/>'
             '<a:ext cx="2971800" cy="458787"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody>'
             '<a:bodyPr anchor="b"/><a:lstStyle><a:lvl1pPr algn="r"><a:defRPr sz="1200"/></a:lvl1pPr></a:lstStyle><a:p>'
             '<a:fld id="{B5B9A3B4-2D6E-4C2B-9E7D-3F1A2B3C4D5E}" type="slidenum"><a:rPr lang="ru-RU"/><a:t>‹#›</a:t></a:fld>'
             '<a:endParaRPr lang="ru-RU"/></a:p></p:txBody></p:sp>')


def layout(name, typ, shapes):
    return (f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n<p:sldLayout {NS} type="{typ}" preserve="1">'
            f'<p:cSld name="{name}"><p:spTree>{GRP}{shapes}</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>')


def body_lst():
    out = []
    for lvl, sz in ((1, 3400), (2, 3000), (3, 2600)):
        out.append(f'<a:lvl{lvl}pPr marL="{457200 * lvl}" indent="-457200"><a:lnSpc><a:spcPct val="100000"/></a:lnSpc>'
                   f'<a:spcBef><a:spcPts val="1200"/></a:spcBef><a:buFont typeface="Arial"/><a:buChar char="•"/>'
                   f'{rpr(sz, "1F2D3D")}</a:lvl{lvl}pPr>')
    return "".join(out)


def build(work: Path):
    lay = work / "ppt" / "slideLayouts"

    def put(n, name, typ, shapes, images):
        (lay / f"slideLayout{n}.xml").write_text(layout(name, typ, shapes), encoding="utf-8")
        rels = lay / "_rels" / f"slideLayout{n}.xml.rels"
        s = rels.read_text(encoding="utf-8")
        add = "".join(f'<Relationship Id="{rid}" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" '
                      f'Target="../media/{img}"/>' for rid, img in images)
        rels.write_text(s.replace("</Relationships>", add + "</Relationships>"), encoding="utf-8")

    logo = [("rIdLogo", "image2.png")]
    top = (rect(2, "Bar", 0, 0, W, BAR, BLUE)
           + pic(3, "Logo", "rIdLogo", W - LOGO_W - 250000, (BAR - LOGO_H) // 2, LOGO_W, LOGO_H)
           + ph(4, "Title", 'type="title"', 700000, 100000, W - LOGO_W - 1300000, BAR - 200000,
                f'<a:lvl1pPr algn="l">{rpr(4000, "FFFFFF", True)}</a:lvl1pPr>', anchor="ctr"))
    body_h = H - BAR - 900000
    put(1, "Title and Content", "obj",
        top + ph(5, "Content", 'idx="1"', 700000, BAR + 300000, W - 1400000, body_h, body_lst()) + SLDNUM, logo)

    half = (W - 1400000 - 400000) // 2
    two = (top + ph(5, "Left", 'idx="1"', 700000, BAR + 300000, half, body_h, body_lst())
           + ph(6, "Right", 'idx="2"', 700000 + half + 400000, BAR + 300000, half, body_h, body_lst()) + SLDNUM)
    # Pandoc falls back to its own layouts for missing names and then overwrites files
    # of the same number, so every layout name pandoc asks for must exist here.
    put(2, "Two Content", "twoObj", two, logo)
    put(3, "Comparison", "twoObj", two, logo)

    put(4, "Section Header", "secHead",
        rect(2, "Bar", 0, 0, W, BAR, BLUE)
        + pic(3, "Logo", "rIdLogo", W - LOGO_W - 250000, (BAR - LOGO_H) // 2, LOGO_W, LOGO_H)
        + ph(4, "Title", 'type="title"', 1219200, 4500000, 21945600, 3000000,
             f'<a:lvl1pPr algn="l">{rpr(6000, NAVY, True)}</a:lvl1pPr>', anchor="ctr")
        + ph(5, "Text", 'type="body" idx="1"', 1219200, 7600000, 21945600, 2400000,
             f'<a:lvl1pPr marL="0" indent="0"><a:buNone/>{rpr(3000, ACCENT)}</a:lvl1pPr>')
        + SLDNUM, logo)

    panel_x = 12888613
    put(5, "Title Slide", "title",
        pic(2, "Half Logo", "rIdHalf", -140636, 0, 9478210, 13911265)
        + rect(3, "Panel", panel_x, 0, W - panel_x, H, BLUE)
        + pic(4, "Logo", "rIdLogo", 14800000, 2600000, 7729137, 5415481)
        + text(5, "Course", panel_x + 300000, 8800000, W - panel_x - 600000, 2600000,
               ["ОСНОВЫ ПРОЕКТНОЙ", "ДЕЯТЕЛЬНОСТИ"], 4800, "FFFFFF")
        + ph(6, "Title", 'type="ctrTitle"', 988727, 2400000, 11400000, 4100000,
             f'<a:lvl1pPr algn="l"><a:lnSpc><a:spcPct val="100000"/></a:lnSpc>{rpr(5000, NAVY, True)}</a:lvl1pPr>', anchor="b")
        + ph(7, "Subtitle", 'type="subTitle" idx="1"', 988727, 6800000, 11400000, 6300000,
             f'<a:lvl1pPr marL="0" indent="0" algn="l"><a:spcBef><a:spcPts val="1000"/></a:spcBef><a:buNone/>'
             f'{rpr(2800, ACCENT, True)}</a:lvl1pPr>'),
        [("rIdHalf", "image1.png")] + logo)

    put(8, "Content with Caption", "objTx",
        top + ph(5, "Content", 'idx="1"', 700000, BAR + 300000, W - 1400000, H - BAR - 2600000, body_lst())
        + ph(6, "Caption", 'type="body" idx="2"', 700000, H - 2100000, W - 1400000, 1500000,
             f'<a:lvl1pPr marL="0" indent="0"><a:buNone/>{rpr(2200, "1F2D3D")}</a:lvl1pPr>')
        + SLDNUM, logo)

    # Master text styles: the Keynote defaults (84 pt titles, 44 pt body) are too large.
    master = work / "ppt" / "slideMasters" / "slideMaster1.xml"
    s = master.read_text(encoding="utf-8")
    master.write_text(s.replace('sz="8400"', 'sz="4000"').replace('sz="4400"', 'sz="2600"'), encoding="utf-8")

    # Pandoc needs a slide-number field in the notes master to write speaker notes.
    notes = work / "ppt" / "notesMasters" / "notesMaster1.xml"
    s = notes.read_text(encoding="utf-8")
    if 'type="slidenum"' not in s:
        i = s.rfind("</p:spTree>")
        notes.write_text(s[:i] + NOTES_NUM + s[i:], encoding="utf-8")


def main():
    with tempfile.TemporaryDirectory() as tmp:
        work = Path(tmp)
        with zipfile.ZipFile(SRC) as z:
            z.extractall(work)
        build(work)
        OUT.unlink(missing_ok=True)
        files = sorted((p for p in work.rglob("*") if p.is_file()), key=lambda p: (p.name != "[Content_Types].xml", str(p)))
        with zipfile.ZipFile(OUT, "w", zipfile.ZIP_DEFLATED) as z:
            for p in files:
                z.write(p, p.relative_to(work).as_posix())
    print(f"{OUT}: reference doc")


if __name__ == "__main__":
    main()
