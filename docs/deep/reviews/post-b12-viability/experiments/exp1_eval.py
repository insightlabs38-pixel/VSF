"""EXPERIMENTAL. Offline evaluation of evidence-reconciled binding on the saved observer snapshots (see EXPERIMENT_RESULTS.md)."""
import collections
import json
import re
import subprocess
import sys
import unicodedata
from pathlib import Path

REPO = Path(__file__).resolve().parents[5]
EV = Path(__file__).resolve().parent.parent / "evidence"
SNAPS = Path(sys.argv[1])
REC = REPO / "compiler/target/release/vsf-recognizer"


def norm(s):
    s = unicodedata.normalize("NFKC", s or "").lower()
    s = re.sub(r"\(required\)|\(optional\)", " ", re.sub(r"[*:]+", " ", s))
    return " ".join(re.sub(r"[^\w]+", " ", s).split())


def toks(s):
    return norm(s).split()


def recognize(path):
    r = subprocess.run([str(REC), "recognize", str(path), "--rules", "3"], capture_output=True, text=True)
    return json.loads(r.stdout)


def build(snap):
    nodes = {n["id"]: n for n in snap["nodes"]}
    kids = collections.defaultdict(list)
    for n in snap["nodes"]:
        kids[n["parent"]].append(n["id"])
    return nodes, kids


def content(nodes, kids, i, limit=140):
    out, stack = [], [i]
    while stack and sum(len(x) for x in out) < limit:
        j = stack.pop(0)
        n = nodes.get(j)
        if not n:
            continue
        if n.get("text"):
            out.append(n["text"])
        stack = kids.get(j, []) + stack
    return " ".join(" ".join(out).split())[:limit]


def caption(nodes, kids, i):
    for _ in range(3):
        n = nodes.get(i)
        if not n:
            break
        sibs = kids.get(n["parent"], [])
        k = sibs.index(i) if i in sibs else 0
        for j in reversed(sibs[:k]):
            c = content(nodes, kids, j, 60)
            if c and len(c.split()) <= 6:
                return c
        i = n["parent"]
    return ""


def evidence(nodes, kids, u):
    n = nodes[u["node"]]
    ev = {}
    if u["type"] == "field":
        ev["binding"] = (u.get("binding") or {}).get("text") or ""
    else:
        ev["name"] = u.get("name") or ""
        ev["shown"] = u.get("shown") or ""
    ev["own_name"] = n.get("name") or ""
    ev["content"] = content(nodes, kids, u["node"])
    for k, rid in n.get("rels", []):
        if k == "label":
            ev["label_rel"] = content(nodes, kids, rid)
    ev["caption"] = caption(nodes, kids, u["node"])
    return {k: v for k, v in ev.items() if v}


FIELD_KINDS = {"fill": {"text", "date", "other", "range"}, "choose": {"choice"}, "toggle": {"toggle"}, "upload": {"file"}}


def compat(step_do, u):
    if step_do == "click":
        return u["type"] == "action" or (u["type"] == "field" and u.get("kind") in ("toggle", "choice"))
    if step_do == "drag":
        return u["type"] == "action"
    return u["type"] == "field" and u.get("kind") in FIELD_KINDS.get(step_do, set())


TIER_A = ("binding", "name", "shown", "label_rel", "own_name")


def resolve(target, step_do, units, evs, mode):
    t = norm(target)
    tt = set(toks(target))
    cand = [u for u in units if u["type"] != "collection" and u.get("usable") and compat(step_do, u)]

    def exact(keys):
        return [u for u in cand if t in {norm(evs[u["node"]].get(k, "")) for k in keys} - {""}]

    if mode == "V0":
        hits = exact(("binding", "name", "shown"))
    else:
        tiers = [exact(TIER_A), exact(("content",))]
        if mode == "V2":
            scored = []
            for u in cand:
                best = 0.0
                for k in (*TIER_A, "content"):
                    et = set(toks(evs[u["node"]].get(k, "")))
                    if not et or not tt:
                        continue
                    if tt <= et:
                        best = max(best, len(tt) / len(et))
                    elif len(et) >= 2 and et <= tt:
                        best = max(best, len(et) / len(tt))
                if best >= 0.5:
                    scored.append((best, u))
            top = max((s for s, _ in scored), default=0)
            tiers.append([u for s, u in scored if s == top])
            tiers.append(exact(("caption",)))
        else:
            tiers.append(exact(("caption",)))
        hits = next((h for h in tiers if h), [])
    if len(hits) == 1:
        return "bound", hits[0]
    return ("ambiguous" if hits else "none"), None


def main():
    files = sorted(SNAPS.glob("*.json"))
    obs = {}
    for l in (EV / "observe-A1.jsonl").read_text().splitlines():
        o = json.loads(l)
        for s in o["steps"]:
            obs[(o["id"], s["i"])] = s
    rows, selft = [], collections.Counter()
    probes = collections.Counter()
    for f in files:
        d = json.loads(f.read_text())
        tmp = Path("/tmp/claude-0") / f"exp1-{f.stem}.snap.json"
        tmp.write_text(json.dumps(d["snapshot"]))
        rec = recognize(tmp)
        tmp.unlink()
        nodes, kids = build(d["snapshot"])
        units = [u for u in rec["units"] if u["type"] != "collection"]
        evs = {u["node"]: evidence(nodes, kids, u) for u in units}
        ref = d["refNode"]
        anc = set()
        i = ref
        while i in nodes:
            anc.add(i)
            i = nodes[i]["parent"]
        desc = set()
        stack = [ref] if ref is not None else []
        while stack:
            j = stack.pop()
            desc.add(j)
            stack += kids.get(j, [])
        good = {u["node"] for u in units if u["node"] in anc or u["node"] in desc} if ref is not None else set()
        row = {"wf": d["wf"], "step": d["step"], "target": d["target"], "do": d["do"], "unit_at_control": bool(good)}
        for mode in ("V0", "V1", "V2"):
            r, u = resolve(d["target"], d["do"], units, evs, mode)
            row[mode] = ("correct" if u["node"] in good else "wrong") if r == "bound" else r
        # adversarial probes
        r, u = resolve("Frobnicate quux", d["do"], units, evs, "V2")
        probes["absent_name_bound"] += r == "bound"
        probes["absent_name_total"] += 1
        tk = toks(d["target"])
        near = tk[0] if len(tk) > 1 else (d["target"] + " extra")
        r, u = resolve(near, d["do"], units, evs, "V2")
        probes["near_miss_total"] += 1
        if r == "bound":
            probes["near_miss_bound"] += 1
            probes["near_miss_bound_to_other_control"] += u["node"] not in good
        # self-resolution over every named unit on the page
        for v in units:
            if not v.get("usable"):
                continue
            nm = evs[v["node"]].get("binding") or evs[v["node"]].get("name") or evs[v["node"]].get("own_name")
            if not nm or not toks(nm):
                continue
            for mode in ("V0", "V2"):
                rr, uu = resolve(nm, "click" if v["type"] == "action" else {"text": "fill", "choice": "choose", "toggle": "toggle", "file": "upload"}.get(v.get("kind"), "fill"), units, evs, mode)
                selft[(mode, "bound_right" if rr == "bound" and uu["node"] == v["node"] else "bound_wrong" if rr == "bound" else rr)] += 1
        rows.append(row)
    with_unit = [r for r in rows if r["unit_at_control"]]
    v0_fail = [r for r in with_unit if r["V0"] != "correct"]
    summary = {"steps": len(rows), "steps_with_unit_at_control": len(with_unit), "v0_failures_with_unit": len(v0_fail)}
    for mode in ("V0", "V1", "V2"):
        c = collections.Counter(r[mode] for r in rows)
        summary[mode] = dict(c)
        summary[f"{mode}_recovered_of_v0_failures"] = sum(1 for r in v0_fail if r[mode] == "correct")
    summary["v2_wrong_steps"] = [(r["wf"], r["step"], r["target"]) for r in rows if r["V2"] == "wrong"]
    summary["v2_still_unresolved_with_unit"] = [(r["wf"], r["step"], r["target"], r["V2"]) for r in v0_fail if r["V2"] != "correct"]
    summary["v0_regressions_under_v2"] = [(r["wf"], r["step"]) for r in rows if r["V0"] == "correct" and r["V2"] != "correct"]
    by_app = collections.defaultdict(lambda: collections.Counter())
    for r in rows:
        a = r["wf"].split("-")[0]
        by_app[a]["steps"] += 1
        by_app[a]["V0_correct"] += r["V0"] == "correct"
        by_app[a]["V2_correct"] += r["V2"] == "correct"
    summary["by_app"] = {k: dict(v) for k, v in by_app.items()}
    summary["probes"] = dict(probes)
    summary["self_resolution"] = {f"{m}:{k}": v for (m, k), v in sorted(selft.items())}
    (EV / "exp1-results.json").write_text(json.dumps({"summary": summary, "rows": rows}, indent=1) + "\n")
    print(json.dumps(summary, indent=1))


main()
