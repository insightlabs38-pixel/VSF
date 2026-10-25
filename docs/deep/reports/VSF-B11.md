Batch: B11 | Gate: G11 | Decision: PROCEED
Branch: vsf/b11-independent-corpus | Base: 851e31d | Commit: 148498a (code and receipts; CI run on the PR head)
PR: https://github.com/insightlabs38-pixel/VSF/pull/14 ; merge commit 3594e8f
Closed tasks: VSF-B11-T07 VSF-B11-T01 VSF-B11-T08 VSF-B11-T02 VSF-B11-T09 VSF-B11-T10 VSF-B11-T03 VSF-B11-T04 VSF-B11-T05 VSF-B11-T06
Contracts/primitive IDs: no IR or primitive change; recognizer rules 3 (composites, list and repeated-block collections, structural effects); Go FileExecutor, FilePolicy, FileSandbox, BulkExecutor; observer scrollProbe
Changed modules: compiler/crates/vsf-recognizer, frontend/packages/observer, backend/execution, tests/corpus, fixtures/corpus, ADR-040 and ADR-041
Tests: 82 evidence runs PASS (recognizer, frozen goldens, corpus harness in Chromium, Go with -race and integration, typecheck, biome, comments, secrets, capture reproduction 81 of 81)
Negative/adversarial: planted wrong-effect and misbound targets, unconfirmed operations never counted as verified, stale pages and filters, vanished ids, lost upload responses, hostile file names, sandbox escape, navigation chrome as collections
Performance: recognizer p50 about 45 ms, max about 80 ms, about 11 MB RSS on 58 pages, unchanged from T02
Coverage (frozen denominator, verified/intended; pre-B11 > current): dev+validation 93/142 > 118/142; held-out AdminLTE+TodoMVC 34/42 > 38/42; held-out File Browser 8/15 > 8/15; wrong effect 0, misbound 0
Reference-confirmed only (not coverage): dev+validation 118/124; AdminLTE+TodoMVC 38/41; File Browser 8/10
Structural: workflows supported 18 > 65 of 115 (dev+validation), 20 > 30 of 47 (open held-out), 2 of 14 unchanged (File Browser)
Security/session scope: hints stay candidates; write seen as read-only did not rise (7, 1, 0); credential fields withheld; zero file policy allows nothing; unknown outcomes never retried
Source compatibility: 15 public applications, pinned and hash-verified, disposable state; not a measure of production compatibility
Version/pin/retention: rules 1 and 2 outputs byte-identical to frozen goldens; no artifact version or retention change
Known limitations: improvement concentrated in diagnosed applications; File Browser unchanged; open held-out hint false claims rose 4 to 17 (read versus navigation); 98 unannotated composites; 15 annotation defects and 4 information limits; held-out independence consumed
Follow-ups queued: new held-out set for later claims; drag and drop, on-demand popups and duplicated enhanced inputs unrecognized
Evidence: docs/deep/evidence/VSF-B11/ (manifest.json, g11/, failure-clusters-t06.md, logs/)
Next eligible: VSF-B12-T01
