# ADR-026: Browser, WASM, native and bridge boundaries

Status: accepted (B01, gate G1). Evidence: `docs/deep/evidence/VSF-B01/` (bench numbers: `bench/capture.json`, `bench/boundary.json`; host 4 vCPU Xeon 2.8 GHz, Chromium 141, Node 22.22).

## Context
B01 had to fix interface shapes only after measuring serialization, lifecycle and boundary costs, and decide how the extension satisfies MV3 policy without an in-extension plan interpreter.

## Decisions
1. **Extension holds no plan interpreter.** Packaged code and WASM only; remote content is restricted declarative data; operation plans run in a companion or hosted worker. Classification table and measured browser facts: `docs/deep/policy/mv3-design-review.md`.
2. **Authorization is VSF's per-origin grant list.** Browser host permissions ignore the port (measured), so (scheme, host, port) grants kept by the worker are the real boundary. Commands come only from the packaged panel in the top frame; the worker re-checks the tab origin before and after injection.
3. **Observation pack `VSFS` v1** is the page-to-worker and worker-to-compiler format: canonical string table in first-use order, 24 bytes per node, bounds-checked strict decode in Rust and TypeScript, SHA-256 over the exact bytes. Native, WASM and TypeScript produce identical bytes, digests and summaries for seeds 1 and 42 at 0/1/2/100/1000/10000 nodes, and reject empty, truncated, trailing, bad-magic and corrupted inputs with identical codes.
4. **Transport inside the extension is a base64 string.** At 4 MB a number array over extension messaging costs 3205 ms p50 versus 273 ms for base64; at 1 MB 721 ms versus 72 ms.
5. **Language boundaries** (`bench/boundary.json`, p50 ms, `analyze` on 1k / 10k / 50k / 200k nodes):

   | Path | 1k | 10k | 50k | 200k |
   |---|---:|---:|---:|---:|
   | Native in-process (lower bound) | 0.32 | 3.70 | 17.6 | 94.5 |
   | Node in-process WASM | 0.49 | 4.96 | 28.7 | 117.0 |
   | Browser WASM | 0.50 | 4.70 | 29.8 | 102.7 |
   | Node spawn per call | 5.82 | 11.5 | 29.7 | 101.6 |
   | Node persistent worker (stdio frames) | 0.49 | 4.42 | 21.3 | 92.3 |
   | Go spawn per call | 3.09 | 7.42 | 33.2 | 96.5 |
   | Go persistent worker (stdio frames) | 0.51 | 4.77 | 24.3 | 85.3 |

   - Browser and extension: in-process WASM (1.2-1.6x native compute).
   - Go services: a **persistent native worker over length-prefixed stdio frames**. Spawn adds about 2.8 ms (Go) to 5.5 ms (Node) per call; the persistent worker adds about 0.2 ms and isolates crashes. After SIGKILL the next call fails immediately and a restart plus first call costs 3.1 ms at 1k nodes. Per-call spawn is acceptable only for rare, long operations.
   - Worker peak RSS 2.9 MB at 1k up to 22.9 MB at 200k; WASM linear memory never shrinks (Node RSS grew about 10 MB at 200k nodes).
   - Not measured, deferred to B15: in-process WASM inside Go (a runtime dependency) and cgo.
6. **Capture budget.** The extension spends its time in the page: at 50k nodes p50 capture 319 ms, pack+base64 177 ms, transfer 44 ms, base64 decode 10 ms, recognition stub 22 ms, fixed worker overhead about 9-11 ms (storage checkpoints, messaging). At 10k nodes end-to-end is 128 ms p50, 164 ms p95. Capture and pack encoding are the optimization targets (visibility checks, string interning); that belongs to B04 and B18.
7. **Loopback bridge** (`backend/bridge`): binds `127.0.0.1` on an ephemeral port; Host must be the bound address (rebinding), Origin must be an exact allowlisted origin (null, wildcards and unknown origins refused, no CORS grant), bearer token compared in constant time and never accepted from URL or cookie, state-changing calls need a non-simple content type plus `X-VSF-Bridge: 1`, 1 MiB body cap, 64 MiB chunked upload with per-upload digest, bounded concurrent uploads with expiry, header and read timeouts. Real-browser tests confirm the paired extension passes CORS preflight and a hostile origin holding the token cannot read or write.
8. **Native messaging limits** (Chromium 141): browser to host succeeded at 32 MB; host to browser succeeded at 999,900 bytes and failed at 1,048,576. Anything larger than about 1 MB from a companion goes through the bridge or chunking.

## Alternatives rejected
Number-array messaging; spawn-per-call as the default Go path; a remote operation DSL interpreted inside the extension; trusting browser host permissions as the only per-origin control.

## Consequences
- ABI of `vsf_wasm` is 2 (adds `vsf_pack_analyze`). The pack format and the bridge API are v1 and gain compatibility rules in B02.
- Pairing, token distribution and Windows pipe equivalents are B09 work; the bridge here is a validated prototype.
- A WebDriver BiDi probe is not part of any B01 task and moves to B21 (transport evolution).

## Rollback
Revert the B01 merge; no stored data or external system depends on it.
