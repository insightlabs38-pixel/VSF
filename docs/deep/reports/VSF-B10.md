Batch: B10 | Gate: G10 | Decision: PROCEED
Branch: vsf/b10-api-cli-mcp | Base: bf7e879 | Commit: a466437 (evidence regenerated on the final PR head)
PR: https://github.com/insightlabs38-pixel/VSF/pull/11
Closed tasks: VSF-B10-T01 VSF-B10-T02 VSF-B10-T03 VSF-B10-T04 VSF-B10-T05 VSF-B10-T06 VSF-B10-T07 VSF-B10-T08
Contracts/primitive IDs: REST v1, MCP 2026-07-28, 37-code error taxonomy; confirmation digest now binds caller and session; no IR or primitive change
Changed modules: backend/api, cli, mcp, interfaces, internal/stacktest (new); backend/execution (confirm digest, decode cache), cmd/vsfctl; ci/jobs.json; ADR-037
Tests: 50/50 evidence runs PASS locally; 224 Go tests in the new packages (api 35, cli 11, mcp 16, interfaces 39 test functions plus subtests), full Go suites, cargo, clippy and all 10 CI jobs pass
Negative/adversarial: MCP name collisions and version-pinned tools, older and revoked pins, unauthorized and cross-surface callers, unknown commit and unverified effect on all four surfaces, unemitted operations, roles revoked while a write waits
Performance: p50 without source, runtime 1.41 ms, service 1.49, REST 2.24, MCP 2.45, CLI 3.31 (two round trips), loopback floor 0.34; 10000-entry catalog indexes in 34 ms and lists a cold page in 15 ms; decode memoization cut runtime p50 about half
Security/session scope: credentials per caller and surface stored as digests; session ids give no oracle; confirmations belong to caller, session and a person; no output carries tokens or source identity (scanned)
Source compatibility: fixture source only; no third-party application validated
Version/pin/retention: exact pins honoured, revoked never revived, deprecated named with the served list; no retention change
Known limitations: no gateway process composes the surfaces; shell does not call the UI surface; no delegated confirmation; MCP Tasks, stdio and progress deferred; no client or conformance suite exists for MCP 2026-07-28
Follow-ups queued: VSF-B14-T06, VSF-B16-T05, VSF-B16-T06, VSF-B16-T07
CI findings fixed: an attribution footer was appended to the PR text (removed); remote CI 13/13 on 2bf5ff0, merge aa45177 (evidence/remote-ci.md)
Evidence: docs/deep/evidence/VSF-B10/ (manifest.json, logs/, overhead.txt, remote-ci.md, collect.sh)
Next eligible: VSF-B11-T01 (B11 is provisional: review failure clusters and record an ADR first)
