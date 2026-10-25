"""Step-level structural figures from the observation-only pass and the layer R records."""
import collections
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
EV = ROOT / "evidence"
FROZEN = {w["id"]: w for w in json.loads((EV / "WORKFLOWS_FROZEN.json").read_text())["workflows"]}


def jl(name):
    p = EV / name
    return [json.loads(x) for x in p.read_text().splitlines() if x.strip()] if p.exists() else []


VERB = {"commit": "write", "create": "write", "toggle": "write", "destroy": "destructive", "navigate": "navigation", "dismiss": "navigation", "reveal": "navigation", "query": "read", "export": "read"}


def hint(step):
    if step.get("structural"):
        return step["structural"]
    hs = sorted({VERB.get(v, "unknown") for v in (step.get("verbs") or [])})
    rank = {"destructive": 0, "write": 1, "read": 2, "navigation": 3, "unknown": 4}
    return hs[0] if hs else "none"


def norm_effect(h):
    return {"state_change": "write", "destructive_candidate": "destructive", "state_change_candidate": "write", "read_candidate": "read", "navigation_candidate": "navigation"}.get(h, h)


import os
SUF = os.environ.get("AUDIT_SUFFIX", "")
obs = jl(f"observe{SUF}.jsonl")
out = {"workflows": len(obs)}
steps = [(o, s) for o in obs for s in o.get("steps", []) if s["do"] not in ("press", "type")]
out["target_steps"] = len(steps)
out["unit_at_control"] = sum(1 for _, s in steps if s["unitAtControl"])
out["unit_usable"] = sum(1 for _, s in steps if s["unitAtControl"] and s.get("usable"))
out["resolved_correct"] = sum(1 for _, s in steps if s["wouldResolve"] == "correct")
out["resolved_misbound"] = sum(1 for _, s in steps if s["wouldResolve"] == "misbound")
out["resolution_reasons"] = dict(collections.Counter(s["resolution"] for _, s in steps))
by_app = collections.defaultdict(lambda: collections.Counter())
for o, s in steps:
    a = by_app[o["app"]]
    a["steps"] += 1
    a["unit"] += s["unitAtControl"]
    a["correct"] += s["wouldResolve"] == "correct"
out["by_app"] = {k: dict(v) for k, v in by_app.items()}
unit_but_not_resolved = [(o["id"], s["target"], s.get("nameBound"), s["resolution"]) for o, s in steps if s["unitAtControl"] and s["wouldResolve"] != "correct"]
out["unit_but_not_resolved"] = len(unit_but_not_resolved)
out["unit_but_not_resolved_examples"] = unit_but_not_resolved[:40]
no_unit = [(o["id"], s["target"]) for o, s in steps if not s["unitAtControl"]]
out["no_unit_at_control"] = len(no_unit)
out["no_unit_examples"] = no_unit[:40]
commit = []
for o, s in steps:
    if not s["committing"]:
        continue
    w = FROZEN[o["id"]]
    h = norm_effect(hint(s)) if s["unitAtControl"] else "no_unit"
    commit.append({"id": o["id"], "declared": w["effect"], "hint": h, "name": s["target"], "bound": s.get("nameBound"), "usable": s.get("usable")})
out["committing_steps"] = len(commit)
cm = collections.Counter((c["declared"], c["hint"]) for c in commit)
out["committing_declared_vs_hint"] = {f"{a}->{b}": n for (a, b), n in sorted(cm.items())}
out["write_seen_as_read_or_navigation"] = [c for c in commit if c["declared"] in ("write", "destructive") and c["hint"] in ("read", "navigation")]
out["read_seen_as_write"] = [c for c in commit if c["declared"] == "read" and c["hint"] in ("write", "destructive")]
out["destructive_marked_unusable"] = [c["id"] for c in commit if c["declared"] == "destructive" and c["usable"] is False]
lat = [o["recMsTotal"] / o["recCalls"] for o in obs if o.get("recCalls")]
out["recognizer_call_ms_incl_python_spawn"] = {"median": sorted(lat)[len(lat) // 2] if lat else None, "max": max((o["recMsMax"] for o in obs if o.get("recMsMax")), default=None)}
out["recognizer_rss_kb_max"] = max((o.get("recRssKb", 0) for o in obs), default=0)
(EV / f"step-summary{SUF}.json").write_text(json.dumps(out, indent=1) + "\n")
print(json.dumps({k: v for k, v in out.items() if not k.endswith("examples")}, indent=1))
