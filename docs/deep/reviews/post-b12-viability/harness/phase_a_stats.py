"""Recomputes the B11 reproduction and harness-quality figures from the frozen B11 files and the reproduced runs."""
import collections
import glob
import json
import re
import statistics
from pathlib import Path

REPO = Path(__file__).resolve().parents[5]
A = REPO / "docs/deep/reviews/post-b12-viability/evidence/phase-a"
man = {s["id"]: s for s in json.loads((REPO / "tests/corpus/corpus.json").read_text())["sources"]}
rows = json.loads((A / "ops-devval.json").read_text())["rows"] + json.loads((A / "ops-heldout.json").read_text())["rows"]
rows = [r for r in rows if r["origin"] == "frozen"]
ops = {}
for f in glob.glob(str(REPO / "fixtures/corpus/operations/*/*.json")):
    d = json.loads(Path(f).read_text())
    for o in d["operations"]:
        ops[(d["source"], d["page"], o["name"])] = o
ver = [r for r in rows if r.get("outcome") == "verified"]
key = lambda r: (r["source"], r["page"], r["operation"])
commit_rx = re.compile(r"\b(save|submit|create|add|delete|remove|upload|send|post|confirm|ok|yes|apply|update|register|sign|log ?in|clear completed|go|publish|commit)\b", re.I)


def commits(o):
    return any(s["do"] == "click" and commit_rx.search(s.get("target") or "") for s in o["steps"]) or any(s["do"] == "press" and s.get("key") == "Enter" for s in o["steps"])


by_fam = collections.defaultdict(lambda: [0, 0])
for r in rows:
    f = man[r["source"]]["family"]
    by_fam[f][0] += 1
    by_fam[f][1] += r.get("outcome") == "verified"
rates = [v[1] / v[0] for v in by_fam.values()]
ctl = {}
for name in ("null", "shuffle"):
    s = json.loads((A / f"ctl-{name}-devval.json").read_text())["summary"]
    ctl[name] = {k: s[k] for k in ("intended", "referenceConfirmed", "verified", "fallback", "abandoned", "wrongEffect", "misbound")}
ctl["null_verified_ops"] = [f"{r['source']}/{r['page']}: {r['operation']}{' (negative)' if r['negative'] else ''}" for r in json.loads((A / "ctl-null-devval.json").read_text())["rows"] if r.get("outcome") == "verified" and r["origin"] == "frozen"]
out = {
    "intended": len(rows), "verified": len(ver),
    "verified_single_step": sum(1 for r in ver if len(ops[key(r)]["steps"]) == 1),
    "verified_negative_controls_of_the_set": sum(1 for r in ver if r["negative"]),
    "ops_with_commit_like_click_or_enter": sum(1 for o in ops.values() if not o.get("negative") and commits(o)),
    "ops_non_negative": sum(1 for o in ops.values() if not o.get("negative")),
    "ops_destructive_effect": sum(1 for o in ops.values() if o["effect"] == "destructive"),
    "expectation_kinds": dict(collections.Counter(e["kind"] for o in ops.values() for e in o["expect"])),
    "families": len(by_fam), "family_weighted_mean": round(statistics.mean(rates), 3), "family_median": round(statistics.median(rates), 3), "family_min": round(min(rates), 3),
    "per_family": {k: f"{v[1]}/{v[0]}" for k, v in sorted(by_fam.items())},
    "todomvc_verified_share": round(sum(1 for r in ver if r["source"].startswith("todomvc")) / len(ver), 3),
    "controls": ctl,
}
(A / "b11-harness-audit.json").write_text(json.dumps(out, indent=1) + "\n")
print(json.dumps(out, indent=1))
