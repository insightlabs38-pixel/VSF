# Dispatch budget

What VSF itself may spend to dispatch one verified write, with the source application answering instantly. Source and network time are never part of it. Enforced by `TestDispatchOverheadPerLayerExcludesTheSourceAndStaysInBudget` (`backend/interfaces/overhead_test.go`); numbers from the last run are in `docs/deep/evidence/VSF-B10/overhead.txt`.

## What is measured
400 sequential calls after 30 warm-up calls, each a distinct request id and record, a real Rust authorizer, an in-memory journal and a source stub with no latency. Layers, each wrapping the one before it:

| Layer | What it adds |
|---|---|
| `http-floor` | one loopback HTTP round trip, for reference |
| `runtime` | decode (memoized), admission, two Rust authorizations, journal, verification |
| `service` | contract resolution, schema validation, scope and session checks |
| `rest`, `ui-client`, `mcp` | one HTTP round trip, JSON, authentication, translation |
| `cli` | two round trips (describe, then run), argument typing, output |

## Budgets (ceilings, not targets)
| Layer | p50 | p99 | Over the layer below (p50) |
|---|---|---|---|
| http-floor | 2 ms | 10 ms | |
| runtime | 4 ms | 20 ms | |
| service | 5 ms | 20 ms | 2 ms |
| rest, ui-client, mcp | 6 ms | 25 ms | 3 ms over service |
| cli | 8 ms | 30 ms | 6 ms over service |

The ceilings are several times the measured costs so a shared CI runner does not fail a correct change; a change that doubles a layer's cost is still visible in the report. Under the race detector every ceiling is multiplied by ten.

## Rules
- A new layer or a new hop needs its own row and ceiling before it ships.
- A budget is raised only with a measured reason in an ADR; it is never raised to make a failing run pass.
- Memory and storage are not part of this budget; the discovery cost of a large catalog is covered by `BenchmarkCatalogIndex` and `BenchmarkFirstPageOfAHugeCatalog` in `backend/api`.
