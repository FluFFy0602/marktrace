"""Minimal BPMN 2.0 DI -> SVG renderer (bpmn-js-like look). Usage: bpmn2svg.py in.bpmn out.svg"""
import sys
import xml.etree.ElementTree as ET
from html import escape

NS = {
    "bpmn": "http://www.omg.org/spec/BPMN/20100524/MODEL",
    "bpmndi": "http://www.omg.org/spec/BPMN/20100524/DI",
    "dc": "http://www.omg.org/spec/DD/20100524/DC",
    "di": "http://www.omg.org/spec/DD/20100524/DI",
}
B = "{%s}" % NS["bpmn"]
STROKE = "#22242a"
FONT = "Arial, Helvetica, sans-serif"
FS = 12

src, dst = sys.argv[1], sys.argv[2]
root = ET.parse(src).getroot()

elems = {}
for el in root.iter():
    if el.get("id") and el.tag.startswith(B):
        elems[el.get("id")] = el
defaults = {el.get("default") for el in elems.values() if el.get("default")}


def kind(el):
    return el.tag[len(B):]


def text_w(s, fs=FS):
    w = 0.0
    for ch in s:
        if ch in "il.,:;|!'()[]":
            w += 0.28
        elif ch == " ":
            w += 0.28
        elif ch.isupper() or ch in "мшщжюМШЩЖЮmwMW":
            w += 0.70
        elif ch.isdigit():
            w += 0.56
        else:
            w += 0.54
    return w * fs


def wrap(name, width, fs=FS):
    lines = []
    for para in name.split("\n"):
        cur = ""
        for word in para.split(" "):
            cand = (cur + " " + word).strip()
            if cur and text_w(cand, fs) > width:
                lines.append(cur)
                cur = word
            else:
                cur = cand
        lines.append(cur)
    return lines


def text_block(lines, cx, y_top, fs=FS, anchor="middle"):
    out = []
    lh = fs * 1.2
    for i, ln in enumerate(lines):
        out.append(
            f'<text x="{cx:.1f}" y="{y_top + fs + i * lh:.1f}" font-family="{FONT}" '
            f'font-size="{fs}" text-anchor="{anchor}" fill="{STROKE}">{escape(ln)}</text>'
        )
    return "\n".join(out)


def bounds(node):
    b = node.find("dc:Bounds", NS)
    return tuple(float(b.get(k)) for k in ("x", "y", "width", "height"))


def gear(x, y):
    # simple cog: circle with 8 teeth
    import math
    cx, cy, r = x + 12, y + 12, 5.5
    teeth = []
    for i in range(8):
        a = i * math.pi / 4
        teeth.append(
            f'<line x1="{cx + r * math.cos(a):.1f}" y1="{cy + r * math.sin(a):.1f}" '
            f'x2="{cx + (r + 3) * math.cos(a):.1f}" y2="{cy + (r + 3) * math.sin(a):.1f}" '
            f'stroke="{STROKE}" stroke-width="2.2"/>'
        )
    return (
        "".join(teeth)
        + f'<circle cx="{cx}" cy="{cy}" r="{r}" fill="white" stroke="{STROKE}" stroke-width="1.3"/>'
        + f'<circle cx="{cx}" cy="{cy}" r="2" fill="white" stroke="{STROKE}" stroke-width="1"/>'
    )


def person(x, y):
    cx = x + 13
    return (
        f'<circle cx="{cx}" cy="{y + 9}" r="4.5" fill="white" stroke="{STROKE}" stroke-width="1.2"/>'
        f'<path d="M{cx - 8},{y + 22} q0,-8 8,-8 q8,0 8,8 z" fill="white" stroke="{STROKE}" stroke-width="1.2"/>'
    )


shapes, edges, labels = [], [], []
min_x = min_y = 1e9
max_x = max_y = -1e9


def grow(x, y, w, h):
    global min_x, min_y, max_x, max_y
    min_x, min_y = min(min_x, x), min(min_y, y)
    max_x, max_y = max(max_x, x + w), max(max_y, y + h)


pools, lanes, flow_shapes = [], [], []
for sh in root.iter("{%s}BPMNShape" % NS["bpmndi"]):
    el = elems[sh.get("bpmnElement")]
    k = kind(el)
    if k == "participant":
        pools.append((sh, el))
    elif k == "lane":
        lanes.append((sh, el))
    else:
        flow_shapes.append((sh, el))

for sh, el in pools:
    x, y, w, h = bounds(sh)
    grow(x, y, w, h)
    name = el.get("name", "")
    shapes.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" fill="white" stroke="{STROKE}" stroke-width="1.5"/>')
    if el.get("processRef"):
        shapes.append(f'<line x1="{x + 30}" y1="{y}" x2="{x + 30}" y2="{y + h}" stroke="{STROKE}" stroke-width="1.5"/>')
        cx, cy = x + 15, y + h / 2
        shapes.append(
            f'<text x="{cx}" y="{cy}" transform="rotate(-90 {cx} {cy})" font-family="{FONT}" font-size="{FS + 1}" '
            f'text-anchor="middle" dominant-baseline="central" fill="{STROKE}">{escape(name)}</text>'
        )
    else:
        lines = wrap(name, w - 40, FS + 1)
        lh = (FS + 1) * 1.2
        shapes.append(text_block(lines, x + w / 2, y + h / 2 - len(lines) * lh / 2 - 2, FS + 1))

for sh, el in lanes:
    x, y, w, h = bounds(sh)
    shapes.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" fill="none" stroke="{STROKE}" stroke-width="1.5"/>')
    cx, cy = x + 15, y + h / 2
    shapes.append(
        f'<text x="{cx}" y="{cy}" transform="rotate(-90 {cx} {cy})" font-family="{FONT}" font-size="{FS}" '
        f'text-anchor="middle" dominant-baseline="central" fill="{STROKE}">{escape(el.get("name", ""))}</text>'
    )


def label_for(node, name, fallback):
    lb = node.find("bpmndi:BPMNLabel", NS)
    if lb is not None and lb.find("dc:Bounds", NS) is not None:
        x, y, w, h = bounds(lb)
    else:
        x, y, w, h = fallback
    lines = wrap(name, max(w, 40))
    grow(x, y, w, len(lines) * FS * 1.2 + 2)
    return text_block(lines, x + w / 2, y)


# boundary events drawn after tasks
ordered = sorted(flow_shapes, key=lambda p: kind(p[1]) == "boundaryEvent")
for sh, el in ordered:
    x, y, w, h = bounds(sh)
    grow(x, y, w, h)
    k = kind(el)
    name = el.get("name", "") or ""
    cx, cy = x + w / 2, y + h / 2
    if k.endswith("Task") or k == "task":
        shapes.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="10" ry="10" fill="white" stroke="{STROKE}" stroke-width="2"/>')
        if k == "serviceTask":
            shapes.append(gear(x + 2, y + 2))
        elif k == "userTask":
            shapes.append(person(x + 2, y + 2))
        lines = wrap(name, w - 16)
        lh = FS * 1.2
        shapes.append(text_block(lines, cx, cy - len(lines) * lh / 2 - 2))
    elif k in ("startEvent", "endEvent", "boundaryEvent", "intermediateThrowEvent", "intermediateCatchEvent"):
        r = w / 2
        sw = {"startEvent": 2, "endEvent": 4}.get(k, 1.5)
        shapes.append(f'<circle cx="{cx}" cy="{cy}" r="{r if k != "endEvent" else r - 1}" fill="white" stroke="{STROKE}" stroke-width="{sw}"/>')
        if k == "boundaryEvent" or k.startswith("intermediate"):
            shapes.append(f'<circle cx="{cx}" cy="{cy}" r="{r - 3}" fill="white" stroke="{STROKE}" stroke-width="1.5"/>')
        if el.find("bpmn:timerEventDefinition", NS) is not None:
            shapes.append(f'<circle cx="{cx}" cy="{cy}" r="{r - 7}" fill="white" stroke="{STROKE}" stroke-width="1.5"/>')
            for i in range(12):
                import math
                a = i * math.pi / 6
                shapes.append(
                    f'<line x1="{cx + (r - 9) * math.cos(a):.1f}" y1="{cy + (r - 9) * math.sin(a):.1f}" '
                    f'x2="{cx + (r - 7.5) * math.cos(a):.1f}" y2="{cy + (r - 7.5) * math.sin(a):.1f}" stroke="{STROKE}" stroke-width="1"/>'
                )
            shapes.append(f'<path d="M{cx},{cy - 7} L{cx},{cy} L{cx + 5},{cy + 3}" fill="none" stroke="{STROKE}" stroke-width="1.5"/>')
        if el.find("bpmn:errorEventDefinition", NS) is not None:
            shapes.append(
                f'<path d="M{cx - 7},{cy + 8} L{cx - 3},{cy - 7} L{cx + 2},{cy + 2} L{cx + 7},{cy - 8} '
                f'L{cx + 3},{cy + 7} L{cx - 2},{cy - 2} Z" fill="white" stroke="{STROKE}" stroke-width="1.3"/>'
            )
        if name:
            labels.append(label_for(sh, name, (cx - 60, y + h + 5, 120, 18)))
    elif k.endswith("Gateway"):
        pts = f"{cx},{y} {x + w},{cy} {cx},{y + h} {x},{cy}"
        shapes.append(f'<polygon points="{pts}" fill="white" stroke="{STROKE}" stroke-width="2"/>')
        if k == "exclusiveGateway":
            d = w * 0.18
            shapes.append(f'<path d="M{cx - d},{cy - d} L{cx + d},{cy + d} M{cx + d},{cy - d} L{cx - d},{cy + d}" stroke="{STROKE}" stroke-width="3.5" stroke-linecap="square"/>')
        if name:
            labels.append(label_for(sh, name, (cx - 60, y + h + 5, 120, 18)))


def arrow(x1, y1, x2, y2, filled=True):
    import math
    a = math.atan2(y2 - y1, x2 - x1)
    L, Wd = 11, 4.5
    bx, by = x2 - L * math.cos(a), y2 - L * math.sin(a)
    p1 = (bx + Wd * math.sin(a), by - Wd * math.cos(a))
    p2 = (bx - Wd * math.sin(a), by + Wd * math.cos(a))
    fill = STROKE if filled else "white"
    return f'<path d="M{x2},{y2} L{p1[0]:.1f},{p1[1]:.1f} L{p2[0]:.1f},{p2[1]:.1f} Z" fill="{fill}" stroke="{STROKE}" stroke-width="1.2"/>'


for ed in root.iter("{%s}BPMNEdge" % NS["bpmndi"]):
    el = elems[ed.get("bpmnElement")]
    k = kind(el)
    pts = [(float(w.get("x")), float(w.get("y"))) for w in ed.findall("di:waypoint", NS)]
    for px, py in pts:
        grow(px, py, 0, 0)
    d = "M" + " L".join(f"{px},{py}" for px, py in pts)
    if k == "messageFlow":
        edges.append(f'<path d="{d}" fill="none" stroke="{STROKE}" stroke-width="1.5" stroke-dasharray="10,6"/>')
        edges.append(f'<circle cx="{pts[0][0]}" cy="{pts[0][1]}" r="4" fill="white" stroke="{STROKE}" stroke-width="1.5"/>')
        edges.append(arrow(*pts[-2], *pts[-1], filled=False))
    else:
        edges.append(f'<path d="{d}" fill="none" stroke="{STROKE}" stroke-width="2" stroke-linejoin="round"/>')
        edges.append(arrow(*pts[-2], *pts[-1]))
        if el.get("id") in defaults:
            (x1, y1), (x2, y2) = pts[0], pts[1]
            import math
            a = math.atan2(y2 - y1, x2 - x1)
            mx, my = x1 + 12 * math.cos(a), y1 + 12 * math.sin(a)
            # slash across the flow
            s = 6
            ax, ay = math.cos(a + math.pi / 4) * s, math.sin(a + math.pi / 4) * s
            edges.append(f'<line x1="{mx - ay:.1f}" y1="{my + ax:.1f}" x2="{mx + ay:.1f}" y2="{my - ax:.1f}" stroke="{STROKE}" stroke-width="2"/>')
    name = el.get("name", "") or ""
    if name:
        mx = (pts[0][0] + pts[1][0]) / 2
        labels.append(label_for(ed, name, (mx - 50, pts[0][1] - 22, 100, 18)))

pad = 15
vx, vy = min_x - pad, min_y - pad
vw, vh = max_x - min_x + 2 * pad, max_y - min_y + 2 * pad
svg = [
    '<?xml version="1.0" encoding="utf-8"?>',
    f'<!-- rendered from {src.split("/")[-1]} (BPMN DI) -->',
    f'<svg xmlns="http://www.w3.org/2000/svg" width="{vw:.0f}" height="{vh:.0f}" viewBox="{vx:.0f} {vy:.0f} {vw:.0f} {vh:.0f}">',
    f'<rect x="{vx:.0f}" y="{vy:.0f}" width="{vw:.0f}" height="{vh:.0f}" fill="white"/>',
    *shapes,
    *edges,
    *labels,
    "</svg>",
]
open(dst, "w", encoding="utf-8").write("\n".join(svg))
