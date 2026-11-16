"""Structural BPMN 2.0 checks (no XSD available offline). Usage: bpmncheck.py file.bpmn..."""
import sys
import xml.etree.ElementTree as ET

M = "http://www.omg.org/spec/BPMN/20100524/MODEL"
DI = "http://www.omg.org/spec/BPMN/20100524/DI"
B = "{%s}" % M
FLOW_NODES = {"startEvent", "endEvent", "boundaryEvent", "userTask", "serviceTask", "task",
              "exclusiveGateway", "parallelGateway", "intermediateThrowEvent", "intermediateCatchEvent"}

total_err = 0
for path in sys.argv[1:]:
    errs, notes = [], []
    root = ET.parse(path).getroot()
    ids = {}
    for el in root.iter():
        i = el.get("id")
        if i:
            if i in ids:
                errs.append(f"duplicate id {i}")
            ids[i] = el
    kind = lambda el: el.tag[len(B):] if el.tag.startswith(B) else el.tag
    nodes = {i: el for i, el in ids.items() if kind(el) in FLOW_NODES}
    flows = {i: el for i, el in ids.items() if kind(el) == "sequenceFlow"}
    msgs = {i: el for i, el in ids.items() if kind(el) == "messageFlow"}
    inc = {i: [] for i in nodes}
    out = {i: [] for i in nodes}
    for fid, f in flows.items():
        s, t = f.get("sourceRef"), f.get("targetRef")
        if s not in nodes or t not in nodes:
            errs.append(f"sequenceFlow {fid}: unresolved source/target {s} -> {t}")
            continue
        out[s].append(fid)
        inc[t].append(fid)
    for nid, n in nodes.items():
        k = kind(n)
        decl_in = {x.text for x in n.findall(B + "incoming")}
        decl_out = {x.text for x in n.findall(B + "outgoing")}
        if decl_in != set(inc[nid]):
            errs.append(f"{nid}: <incoming> {sorted(decl_in)} != flows {sorted(inc[nid])}")
        if decl_out != set(out[nid]):
            errs.append(f"{nid}: <outgoing> {sorted(decl_out)} != flows {sorted(out[nid])}")
        if k == "startEvent" and inc[nid]:
            errs.append(f"{nid}: start event has incoming flow")
        if k in ("startEvent", "boundaryEvent") and not out[nid]:
            errs.append(f"{nid}: no outgoing flow")
        if k == "endEvent" and out[nid]:
            errs.append(f"{nid}: end event has outgoing flow")
        if k == "endEvent" and not inc[nid]:
            errs.append(f"{nid}: end event unreachable")
        if k.endswith("Task") and (not inc[nid] or not out[nid]):
            errs.append(f"{nid}: task without incoming or outgoing flow")
        if k == "boundaryEvent" and n.get("attachedToRef") not in nodes:
            errs.append(f"{nid}: attachedToRef unresolved")
        if k == "exclusiveGateway":
            if len(out[nid]) >= 2:
                d = n.get("default")
                if d and d not in out[nid]:
                    errs.append(f"{nid}: default flow {d} is not outgoing")
                cond = [f for f in out[nid] if f != d]
                for f in cond:
                    if flows[f].find(B + "conditionExpression") is None and not flows[f].get("name"):
                        notes.append(f"{nid}: flow {f} has neither condition nor label")
            elif len(inc[nid]) < 2:
                errs.append(f"{nid}: gateway neither splits nor joins")
        for ref in (x.get("errorRef") for x in n.iter(B + "errorEventDefinition")):
            if ref and ref not in ids:
                errs.append(f"{nid}: errorRef {ref} unresolved")
    # reachability from start events (boundary events start from their host)
    seen, stack = set(), [i for i, n in nodes.items() if kind(n) == "startEvent"]
    host = {}
    for i, n in nodes.items():
        if kind(n) == "boundaryEvent":
            host.setdefault(n.get("attachedToRef"), []).append(i)
    while stack:
        x = stack.pop()
        if x in seen:
            continue
        seen.add(x)
        stack += [flows[f].get("targetRef") for f in out[x]] + host.get(x, [])
    for i in nodes:
        if i not in seen:
            errs.append(f"{i}: unreachable from start")
    # lanes
    lane_refs = [r.text for r in root.iter(B + "flowNodeRef")]
    for i in nodes:
        if lane_refs.count(i) != 1:
            errs.append(f"{i}: listed in {lane_refs.count(i)} lanes")
    for r in lane_refs:
        if r not in nodes:
            errs.append(f"lane refers to missing node {r}")
    # message flows
    for mid, m in msgs.items():
        for r in (m.get("sourceRef"), m.get("targetRef")):
            if r not in ids:
                errs.append(f"messageFlow {mid}: unresolved {r}")
    # DI coverage
    di = {e.get("bpmnElement") for e in root.iter() if e.tag in ("{%s}BPMNShape" % DI, "{%s}BPMNEdge" % DI)}
    for i in list(nodes) + list(flows) + list(msgs):
        if i not in di:
            errs.append(f"{i}: no DI shape/edge")
    for e in di:
        if e not in ids:
            errs.append(f"DI refers to missing element {e}")
    print(f"== {path.split('/')[-1]}")
    print(f"   flow nodes: {len(nodes)}, sequence flows: {len(flows)}, message flows: {len(msgs)}, "
          f"lanes: {sum(1 for e in ids.values() if kind(e) == 'lane')}")
    for e in errs:
        print("   ERROR", e)
    for n in notes:
        print("   NOTE ", n)
    print(f"   result: {'OK' if not errs else 'FAILED'} ({len(errs)} errors, {len(notes)} notes)")
    total_err += len(errs)
sys.exit(1 if total_err else 0)
