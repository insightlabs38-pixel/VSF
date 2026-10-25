# Decision scorecard

Verdict applied: **C, STOP / SHELVE**. Status key: met, not met, not measured. Evidence is in `VIABILITY_REPORT.md` section numbers and `evidence/`.

## Verdict A (continue): every condition must hold

| condition | status | evidence |
|---|---|---|
| independent complete-workflow correctness about 90 percent or more on a varied set | not met | product path 0 of 48; recognition path 41.7 percent (60.4 with hybrid) (4.2) |
| generalization transfers across distinct families | not met | 12.5 to 66.7 percent by family; rules 3 transfer +1 of 48 (5) |
| safety demonstrated in an adversarial suite, no unauthorized or false-verified write | partly met | none observed; nothing can write; B14 and B07 suites pass; counterexamples of effect classes found (7, 9) |
| native and programmatic coverage beyond preserving the source UI | not met | 0 and 0; only preserved source (4.2) |
| gaps concentrated in a manageable number of generalizable classes | partly met | 24 of 28 first failures in 6 recurring clusters; but 5 further clusters occur in one application each and most fixes are Category 2 (6) |
| credible path to 95 to 99 percent | not met | recognition-path upper bound about 94 percent; product path has no evidence mechanism (7, 10) |
| no extensive per-application annotation | not met for writes | evidence of effect needs read-back, schema or assertion per application (7) |
| performance and operations maintainable | partly met | recognition and assembly cost 2 to 14 ms; the maintenance cost of modelling per widget family is open-ended and unmeasured (9) |

## Verdict B (research only): criteria

| criterion | status |
|---|---|
| current system insufficient for broad development | met |
| small number of root causes explain a substantial part of the failures | met for recognition (6 clusters, 24 of 28); the product failure has two causes: a trivial defect and the evidence problem |
| causes appear solvable by generalizable algorithms | not shown: the cheapest hypothesis recovered 9 percent; the evidence problem failed its registered test |
| solutions have testable predictions | met |
| one bounded campaign could produce a decisive answer | not met: the unknown that decides viability is a human-in-the-loop evidence route whose acceptability is an owner decision, and a decisive answer needs a fresh multi-family set |

## Verdict C (stop): criteria

| criterion | status | evidence |
|---|---|---|
| broad compatibility unlikely without extensive application-specific work | met for writes | Experiment 2 and section 7 |
| structural recognition substantially outperforms behavioural correctness without a credible general solution | met | 95.9 percent unit at control against 72.8 percent named and 41.7 percent complete |
| new applications repeatedly defeat the same architecture | met | 28 of 48 incomplete; the same product defects on all 7 families |
| information or authorization limits obstruct a substantial fraction of ordinary workflows | met | every write workflow (60 percent of the set) lacks an offered action; credentials in 2 more |
| high compatibility depends on manually constructed operation models or expensive inference | partly | not demonstrated either way for recognition; true for write evidence |
| generalization cost grows unmanageably with diversity | unclear | 11 clusters from 7 applications, 5 single-application; no cost data |
| security constraints and necessary observation cannot coexist for important families | partly | probing is unsafe on unknown writes; opaque transports hide effects |
| no credible path to the required reliability and coverage | met for the stated thesis | sections 7 and 10 |

## Verdict D (insufficient evidence)

Not applicable: reference confirmation, oracles, controls and reproduction were all available. The open items (second reviewer, drift, a larger family set, users and economics) do not alter the verdict's basis.

## Confidence

| item | level |
|---|---|
| current compatibility measurement | medium-high for the numbers, medium for their generalization |
| architectural generalization assessment | medium |
| safety assessment | medium |
| estimated future coverage ceiling | low |
| development effort and cost projection | low |
| final viability verdict | medium |
