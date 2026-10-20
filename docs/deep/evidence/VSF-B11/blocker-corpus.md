# B11 blocker: no independent corpus of unseen applications

Decision needed from the owner. Raised at the start of B11 (task VSF-B11-T01), not at a failure.

## What B11 asks for
T01 ranks failure clusters by independent held-out frequency and functional impact; T06 re-runs an independent held-out corpus and reports the coverage denominator; G11 requires improved workflow coverage across unseen applications. The queue and the primitive notes both say registry maturity advances only with held-out fixtures from a source other than the authors, adversarial coverage on real captures and a review record.

## What exists
- Six recognition pages in `fixtures/recognition/apps` (two named held-out) and the fixture source applications in `fixtures/source-apps`, all written inside this project, most by the same author as the recognizer.
- No capture of a third-party application, no ground-truth annotations by anyone but the implementer, no coverage denominator.
- `docs/deep/primitives/collection-extract.md` states that conformance is blocked on exactly this.

## Why implementation cannot proceed honestly
Ranking clusters on pages the implementer wrote ranks the implementer's assumptions. Building the combobox, virtualization, upload and bulk primitives first and measuring them on pages written for them would produce a passing G11 that proves nothing about unseen applications, which the plan forbids ("do not claim ... proven"). The order of T02 to T05 depends on T01's ranking, so none of them can be sequenced without the data.

## Options
1. **Supply a corpus.** Name applications that may be run locally and observed (open-source admin panels, form builders, data-grid demos, calendars with permissive licences, or accounts the owner controls), who annotates ground truth (someone other than the implementer), and the privacy rules for captures. B11 resumes at T01 against that corpus.
2. **Accept a synthetic-only B11.** Rescope B11 as a hypothesis batch that implements the named primitives against authored fixtures, keeps every registry entry below `conformant`, and closes G11 as HOLD or REVISE rather than PROCEED. Nothing downstream may then claim coverage.
3. **Defer B11.** Leave it blocked and continue the batches the dependency graph allows without it (B13, B14, B15, B16, B17). B12, B18, B20 and B21 wait.

Recommended: option 1 with option 3 meanwhile. Work continues on B13 under option 3.

## State left behind
B11-T01 is BLOCKED on this branch; nothing else of B11 is started; no registry maturity changed.
