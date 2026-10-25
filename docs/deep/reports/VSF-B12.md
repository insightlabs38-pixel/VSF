Batch: B12 | Gate: G12 | Decision: PROCEED
Branch: vsf/b12-specialized-interfaces | Base: 3594e8f | Commit: b944e5a (code and receipts; CI run on the PR head)
PR: pending
Closed tasks: VSF-B12-T01 VSF-B12-T02 VSF-B12-T03 VSF-B12-T04 VSF-B12-T05 VSF-B12-T06
Contracts/primitive IDs: no IR or primitive change; new package @vsf/specialized (TerminalAdapter, EditorAdapter, Notebook, ChartModel, TransportManifest), Go Hub/Process in backend/execution
Changed modules: frontend/packages/specialized, tests/specialized, backend/execution/streams.go, CI job specialized, ADR-042
Tests: 32 evidence runs PASS: 46 Playwright scenarios in Chromium (27 adapters and lazy load, 4 chart, 10 transport, 5 large and hostile); 34 package unit tests; 14 Go stream tests with -race; full Go suite, typecheck, biome, comments, secrets
Negative/adversarial: bad sizes and ranges, use after dispose, hostile escape sequences, failed chunk load, restart and stale kernel messages, unsafe MIME, lost connection, unknown input never resent, replay gap, foreign source session, unbounded manifests, canvas charts, 2 MB lines, invalid bytes
Performance: entry 9.5 KB; terminal chunk 437 KB and editor chunk 581 KB load only when used; 200000 output lines keep 1016 rows (+0.6 to 1.0 MB heap); 40 open/dispose cycles +0.9 MB; 100000-line document set in 30 to 90 ms, 67 lines drawn, +23.5 MB
Security/session scope: processes bind to exact tenant, caller and session; input at-most-once with unknown fate never retried; terminal output cannot retitle, navigate or open windows; script-capable notebook output blocked; chart text never becomes markup
Source compatibility: synthetic fixtures only; no third-party application involved; nothing here is recognized from a page yet
Version/pin/retention: xterm 6.0.0, addon-fit 0.11.0, addon-unicode11 0.9.0, CodeMirror state 6.7.6, view 6.43.13, commands 6.11.1 (MIT, exact, over two weeks old); no retention change
Known limitations: synthetic IME events only; Chromium 141 on Linux only; Hub not exposed through API or MCP and tested with in-memory dialers; no live Jupyter or shell; reconnect client is test code; WebRTC loopback only; ZWJ emoji measured per code point
Follow-ups queued: map recognized terminal, editor, chart and notebook surfaces to these models; expose Hub through the interfaces; cross-browser and platform-IME runs
Evidence: docs/deep/evidence/VSF-B12/ (manifest.json, measurements.jsonl, logs/)
Next eligible: none in scope (B15 to B24 not started by instruction)
