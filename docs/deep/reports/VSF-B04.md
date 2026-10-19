Batch: B04 | Gate: G4 | Decision: PROCEED
Branch: vsf/b04-browser-observation | Base: aed1c55 | Commit: 5c8eb32
PR: https://github.com/insightlabs38-pixel/VSF/pull/5
Closed tasks: VSF-B04-T01 VSF-B04-T02 VSF-B04-T03 VSF-B04-T04 VSF-B04-T05 VSF-B04-T06 VSF-B04-T07 VSF-B04-T08
Contracts/primitive IDs: IR 1.0.0 unchanged; vsf-protocol-evidence-v1 new (standalone); no primitive changes
Changed modules: frontend/packages/observer tests/observation apps/extension schemas/protocol-evidence ci
Tests: 29/29 evidence runs PASS; observation 53 browser + 29 unit, extension 2 browser + 3 unit; 9 CI jobs incl. new observation PASS
Negative/adversarial: nested shadow roots, opaque cross-origin frames, lost and corrupted batches, recreated and moved nodes, secret-shaped DOM, 20 protocol-evidence rejections, unreadable flags all PASS
Performance: cold capture p50 33/133/505 ms at 1k/10k/50k nodes (12.7 s at 50k before the label index); steady flush p95 0.6/0.6/1.8 ms; heap about 370 B per node
Security/session scope: ungranted origins refused; credential regions yield no text; account text withheld from shared evidence; no request leaves the page origin
Source compatibility: synthetic fixtures only; extension sees closed shadow roots, page context does not; both advertise limits
Version/pin/retention: ADR-030; epochs are per tab and origin and never below the clock; protocol evidence needs documentation to be supported
Known limitations: no browser accessibility tree, network or canvas; late attachShadow needs rescan; visibility is a hint between probes
Evidence: docs/deep/evidence/VSF-B04/ (manifest.json, logs/, overhead.json, collect.sh)
Next eligible: VSF-B05-T01
