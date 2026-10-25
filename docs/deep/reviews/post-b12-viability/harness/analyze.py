"""Combines reference, product and VSF-path results into WORKFLOW_RESULTS.jsonl and the summary tables."""
import collections
import json
import math
import random
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
EV = ROOT / "evidence"
FROZEN = json.loads((EV / "WORKFLOWS_FROZEN.json").read_text())["workflows"]
FAMILY = {"gitea": "fomantic-server", "pocketbase": "svelte-admin", "jupyterlab": "lumino", "datasette": "server-html",
          "swagger": "react-swagger", "kanboard": "php-jquery", "gradio": "svelte-gradio"}


def jl(name):
    p = EV / name
    return {r["id"]: r for r in (json.loads(l) for l in p.read_text().splitlines() if l.strip())} if p.exists() else {}


ref1, ref2 = jl("reference-pass1.jsonl"), jl("reference-pass2.jsonl")
prod, vsf = jl("product.jsonl"), jl("vsf-layerR.jsonl")


def wilson(k, n, z=1.96):
    if n == 0:
        return (None, None)
    p = k / n
    d = 1 + z * z / n
    c = p + z * z / (2 * n)
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n))
    return (round((c - h) / d, 3), round((c + h) / d, 3))


def boot(by_app, pick, n=10000, seed=1):
    random.seed(seed)
    apps = list(by_app)
    vals = []
    for _ in range(n):
        s = [random.choice(apps) for _ in apps]
        num = sum(pick(by_app[a])[0] for a in s)
        den = sum(pick(by_app[a])[1] for a in s)
        vals.append(num / den if den else 0)
    vals.sort()
    return (round(vals[int(0.025 * n)], 3), round(vals[int(0.975 * n)], 3))


def classify(w):
    wid = w["id"]
    r1, r2 = ref1.get(wid, {}), ref2.get(wid, {})
    status = r1.get("status", "missing") if r1.get("status") == r2.get("status", r1.get("status")) else "unstable"
    v, p = vsf.get(wid, {}), prod.get(wid, {})
    emitted = (p.get("transform", {}).get("actions", {}) or {}).get("available", 0) if p else 0
    row = {"id": wid, "app": w["app"], "family": FAMILY[w["app"]], "title": w["title"], "effect": w["effect"], "difficulty": w["difficulty"], "categories": w["categories"],
           "reference": status, "reference_detail": r1.get("detail"),
           "layerP": {"transform_ok": p.get("transform", {}).get("ok"), "overlay": p.get("overlay"), "actions": p.get("transform", {}).get("actions"), "native_feasible": p.get("nativeFeasible"),
                      "targets_present": sum(1 for t in p.get("targets", []) if t["present"]), "targets_enabled": sum(1 for t in p.get("targets", []) if t["enabled"]), "targets_total": len(p.get("targets", []))},
           "layerG": {"emitted_available_actions": emitted, "programmatic": emitted > 0 and False},
           "layerR": {"outcome": v.get("outcome"), "stopped_at": v.get("stoppedAt"), "cause": v.get("cause"), "steps": w["steps"]},
           "timing": {"reference_steps_ms": r1.get("stepsMs"), "vsf_steps_ms": v.get("stepsMs"), "recognizer_ms": v.get("recMsTotal"), "recognizer_calls": v.get("recCalls")}}
    if status != "confirmed":
        row["class"] = "reference_limitation" if status != "vacuous" else "annotation_defect"
    elif p.get("nativeFeasible"):
        row["class"] = "native_candidate"
    else:
        row["class"] = {"native_path": "recognition_path"}.get(v.get("outcome"), v.get("outcome") or "not_run")
    return row


rows = [classify(w) for w in FROZEN]
(EV / "WORKFLOW_RESULTS.jsonl").write_text("".join(json.dumps(r) + "\n" for r in rows))


def table(group_key):
    g = collections.defaultdict(list)
    for r in rows:
        g[group_key(r)].append(r)
    return g


def rate(rs, cls):
    return sum(1 for r in rs if r["class"] in cls), len(rs)


out = {"n": len(rows), "classes": dict(collections.Counter(r["class"] for r in rows))}
by_app = {a: rs for a, rs in table(lambda r: r["app"]).items()}
for name, cls in [("recognition_path", {"recognition_path"}), ("recognition_or_hybrid", {"recognition_path", "hybrid"}), ("any_path_incl_preserved", {"recognition_path", "hybrid", "preserved_source"})]:
    k, n = rate(rows, cls)
    per = {a: round(rate(rs, cls)[0] / len(rs), 3) for a, rs in by_app.items()}
    vals = sorted(per.values())
    out[name] = {"k": k, "n": n, "workflow_weighted": round(k / n, 3), "wilson95": wilson(k, n), "app_weighted": round(sum(vals) / len(vals), 3),
                 "app_median": vals[len(vals) // 2], "app_min": vals[0], "bootstrap95_over_apps": boot(by_app, lambda rs: rate(rs, cls)), "per_app": per}
conf = [r for r in rows if r["reference"] == "confirmed"]
out["reference_confirmed"] = len(conf)
out["recognition_path_over_confirmed"] = [sum(1 for r in conf if r["class"] == "recognition_path"), len(conf)]
for key in ("effect", "difficulty"):
    out[f"by_{key}"] = {str(k): {"n": len(v), "recognition_path": rate(v, {"recognition_path"})[0]} for k, v in table(lambda r: r[key]).items()}
(EV / "summary.json").write_text(json.dumps(out, indent=1) + "\n")
print(json.dumps(out, indent=1))
