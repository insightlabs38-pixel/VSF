"""Failure taxonomy: earliest cause per failed workflow, root-cause clusters and the leverage of each candidate fix.

The cluster of every failing step was assigned by hand after reading the unit, the DOM and the recognizer output for it (harness/inspect.ts); this file is that assignment.
"""
import collections
import json
from pathlib import Path

EV = Path(__file__).resolve().parent.parent / "evidence"
ORDER = [w["id"] for w in json.loads((EV / "WORKFLOWS_FROZEN.json").read_text())["workflows"]]
R = {json.loads(l)["id"]: json.loads(l) for l in (EV / "vsf-layerR-A1.jsonl").read_text().splitlines()}
O = {json.loads(l)["id"]: json.loads(l) for l in (EV / "observe-A1.jsonl").read_text().splitlines()}

# cluster id: (taxonomy class from the audit brief, generality category 1-4, one-line mechanism)
CLUSTERS = {
    "C-HANDLER": ("Observation / inaccessible state", 2, "clickable element with no role, tabindex or name evidence in the observation (table cell, list item, example cell); the handler is invisible to a DOM observer"),
    "C-NAME": ("Incorrect structural matching", 1, "a unit exists at the control but its bound name is the title, placeholder, description or a column header, not the caption or text a person reads; exact name equality fails"),
    "C-UNNAMED": ("Genuine information limitation (naming)", 4, "control has neither accessible name nor visible caption; any name is authored or positional"),
    "C-SCOPE": ("Missing composite relationships", 2, "same name repeated across columns, dialogs or panels; needs scoping by the owning composite, otherwise ambiguity is correct"),
    "C-WIDGET": ("Incorrect operation semantics", 2, "custom select, dropdown or file-chooser button is recognized as an action, a duplicated wrapper or an unbound field, not as a choice or upload"),
    "C-SPECIAL": ("Rendering or specialist adapter gap", 2, "editor, terminal or notebook surface appears as a generic text field with a wrong label; no recognition maps it to the B12 adapters"),
    "C-CRED": ("Authorization or session limitation", 4, "credential or secret-like field withheld by policy; the workflow needs a person-supplied secret"),
    "C-DND": ("Missing dependency / state-transition model", 2, "drag and drop has no operation model"),
    "C-MENU": ("Incorrect structural matching", 1, "submenu parent exposed as a list item with aria-haspopup but no actionable unit"),
    "C-ROWCTX": ("Missing composite relationships", 2, "row checkboxes carry no name; the row they belong to is the only identity"),
}
# (workflow, step index) -> cluster
STEP = {
    ("GITEA-04", 0): "C-WIDGET", ("GITEA-05", 3): "C-NAME", ("GITEA-06", 2): "C-CRED", ("GITEA-06", 3): "C-CRED", ("GITEA-07", 1): "C-SCOPE", ("GITEA-08", 0): "C-WIDGET",
    ("PB-01", 4): "C-WIDGET", ("PB-02", 0): "C-HANDLER", ("PB-03", 0): "C-HANDLER", ("PB-03", 2): "C-CRED", ("PB-04", 0): "C-NAME", ("PB-06", 0): "C-ROWCTX", ("PB-06", 1): "C-ROWCTX", ("PB-06", 2): "C-ROWCTX",
    ("PB-07", 0): "C-HANDLER", ("PB-07", 1): "C-WIDGET", ("PB-08", 2): "C-SCOPE",
    ("JUP-01", 0): "C-SCOPE", ("JUP-01", 1): "C-SPECIAL", ("JUP-02", 0): "C-NAME", ("JUP-02", 1): "C-SPECIAL", ("JUP-03", 0): "C-NAME", ("JUP-03", 1): "C-SPECIAL",
    ("JUP-04", 0): "C-WIDGET", ("JUP-06", 1): "C-MENU", ("JUP-07", 0): "C-HANDLER",
    ("DS-01", 0): "C-UNNAMED", ("DS-01", 1): "C-UNNAMED", ("DS-05", 0): "C-SPECIAL",
    ("SW-01", 2): "C-NAME", ("SW-03", 2): "C-NAME", ("SW-05", 1): "C-CRED", ("SW-05", 3): "C-SCOPE",
    ("KB-01", 0): "C-SCOPE", ("KB-02", 0): "C-SCOPE", ("KB-05", 0): "C-DND",
    ("GR-04", 0): "C-HANDLER", ("GR-06", 0): "C-SCOPE", ("GR-06", 1): "C-WIDGET", ("GR-06", 2): "C-WIDGET",
}
# PB-03 step 2 is the destroy verb making 'Delete' unusable by design, not a credential: relabel
STEP[("PB-03", 2)] = "C-TEMPORAL"
CLUSTERS["C-TEMPORAL"] = ("Missing dependency / state-transition model", 2, "a menu item that opened a moment earlier was still recorded as not visible when the observation was taken; the visibility hint lags the DOM, and usable becomes true after a longer wait")

fails = []
for wid in ORDER:
    r = R[wid]
    if r["outcome"] == "native_path":
        continue
    bad = [s["i"] for s in O[wid]["steps"] if s["do"] not in ("press", "type") and s["wouldResolve"] != "correct"]
    first = r["stoppedAt"]
    cl = [STEP[(wid, i)] for i in bad]
    fails.append({"workflow": wid, "layerR": r["outcome"], "earliest_step": first, "earliest_cluster": STEP[(wid, first)], "earliest_class": CLUSTERS[STEP[(wid, first)]][0],
                  "failing_steps": [{"step": i, "target": O[wid]["steps"][i]["target"], "cluster": STEP[(wid, i)]} for i in bad],
                  "clusters_all": sorted(set(cl)), "generality_category": min(CLUSTERS[c][1] for c in set(cl)) if len(set(cl)) == 1 else max(CLUSTERS[c][1] for c in set(cl)),
                  "mechanism": CLUSTERS[STEP[(wid, first)]][2]})
(EV / "FAILURE_TAXONOMY.jsonl").write_text("".join(json.dumps(f) + "\n" for f in fails))

by_first = collections.Counter(f["earliest_cluster"] for f in fails)
apps = collections.defaultdict(set)
for f in fails:
    for s in f["failing_steps"]:
        apps[s["cluster"]].add(f["workflow"].split("-")[0])
step_counts = collections.Counter(s["cluster"] for f in fails for s in f["failing_steps"])
leverage = {}
cats = {"C-HANDLER": ["C-HANDLER"], "C-NAME": ["C-NAME"], "C-NAME+C-MENU": ["C-NAME", "C-MENU"], "C-SCOPE": ["C-SCOPE"], "C-WIDGET": ["C-WIDGET"], "C-SPECIAL": ["C-SPECIAL"], "C-UNNAMED": ["C-UNNAMED"], "C-ROWCTX": ["C-ROWCTX"]}
for name, cs in cats.items():
    n = sum(1 for f in fails if f["clusters_all"] and set(f["clusters_all"]) <= set(cs))
    leverage[name] = n
combos = {"all category-1 clusters (C-NAME, C-MENU)": ["C-NAME", "C-MENU"], "category 1 + scope + widget": ["C-NAME", "C-MENU", "C-SCOPE", "C-WIDGET"], "all except category 4 (C-CRED, C-UNNAMED)": [c for c in CLUSTERS if c not in ("C-CRED", "C-UNNAMED")], "everything": list(CLUSTERS)}
comb = {k: sum(1 for f in fails if set(f["clusters_all"]) <= set(v)) for k, v in combos.items()}
out = {"failed_workflows": len(fails), "earliest_cluster": dict(by_first), "failing_steps_by_cluster": dict(step_counts), "applications_by_cluster": {k: sorted(v) for k, v in apps.items()},
       "workflows_fully_unblocked_by_single_cluster": leverage, "workflows_fully_unblocked_by_combination": comb,
       "clusters": {k: {"class": v[0], "category": v[1], "mechanism": v[2]} for k, v in CLUSTERS.items()}}
(EV / "taxonomy-summary.json").write_text(json.dumps(out, indent=1) + "\n")
print(json.dumps({k: v for k, v in out.items() if k != "clusters"}, indent=1))
