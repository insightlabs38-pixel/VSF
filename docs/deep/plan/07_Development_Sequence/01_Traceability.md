# Requirements to implementation and validation traceability

| Critical requirement | Design file | Batches | Verified by |
|---|---|---|---|
| No mandatory LLM, deterministic observation/recognition | `03_Compiler_and_Primitives/01_Discovery_Recognition_and_Optimization.md` | B01–B05 | G5 held-out reproducible classification |
| Broad primitive architecture | `03_Compiler_and_Primitives/02_Primitive_Program_and_Coverage.md` | B03, B11, B12, B20 | Primitive conformance + independent apps |
| Source authority and user permission inheritance | `02_Architecture/02_Contracts_and_IR.md` | B02, B07, B14 | AUTH-001 and OPS fixtures |
| Environment capability negotiation | `04_Runtime_and_Interfaces/01_Execution_Modes_and_Routing.md` | B01, B02, B09, B15, B16, B21 | Cross-mode required-capability matrix |
| Fast cached interactions | `03_Compiler_and_Primitives/01_Discovery_Recognition_and_Optimization.md` | B05, B06, B18 | p50/p95/p99 and incremental work fraction |
| Typed interface parity | `04_Runtime_and_Interfaces/05_API_CLI_MCP_Generation.md` | B07, B10 | Single operation UI/CLI/API/MCP fidelity |
| Flagship versioning | `04_Runtime_and_Interfaces/03_Versioning_and_Compatibility.md` | B02, B08, B13, B17, B19 | VER-001..008 including pin/diff/rollback |
| Low-dependency but efficient frontend | `04_Runtime_and_Interfaces/04_UI_Design_System_and_Accessibility.md` | B06, B12, B18 | Interactive bundle size, a11y and memory |
| Multiple modes and cost policies | `04_Runtime_and_Interfaces/01_Execution_Modes_and_Routing.md` | B09, B15, B16, B23 | Mode matrix, routing policy test |
| Paid localhost, private server and advanced cache | `05_Security_and_Operations/02_Storage_Billing_Deployments.md` | B13, B16, B17, B19, B23 | Entitlement tests and premium upgrade/rollback |
| PAYG metering and non-Stripe choice | `05_Security_and_Operations/02_Storage_Billing_Deployments.md` | B15, B19 | Meter-to-invoice reconciliation |
| User credentials and hosted sandbox safety | `05_Security_and_Operations/01_Identity_Threats_and_Privacy.md` | B09, B14, B15 | AUTH-002..006 and SSRF fixtures |
| ZEROS3 immutable artifacts | `05_Security_and_Operations/02_Storage_Billing_Deployments.md` | B13, B18 | Performance and crash recovery comparison |
| Specialized Benchling/GCP-like capability | `03_Compiler_and_Primitives/03_Capability_Packs_and_Specialized_Systems.md` | B12, B20 | Scientific/terminal/graph negative and round-trip fixtures |
| Accessibility/localization | `04_Runtime_and_Interfaces/04_UI_Design_System_and_Accessibility.md` | B06, B11, B20, B24 | WCAG 2.2 manual + automated, RTL/time tests |
| Enterprise efficiency ROI hypothesis | `01_Product_and_Market/02_Market_and_Unit_Economics.md` | B22, B24 | Task time/error and support cost pilot |
| 99% aspirational workflow compatibility | `06_Quality_and_Validation/01_Test_Strategy_and_Metrics.md` | B11, B20, B24 | Pre-registered held-out workflow denominator |

## Exit criteria by early stage

| Gate | Minimum credible evidence |
|---|---|
| G0 | Clean clone CI passes TS/Rust/Go and fixture toolchain |
| G1 | Browser policy-limited extension plus TS↔WASM benchmark report |
| G2 | Canonical semantic/operation schema, stable hashes and cross-language round trip |
| G3 | Catalog registry classifies entries, tracks dependency DAG and rejects missing conformance |
| G4 | Browser capture handles frames/shadow/AX/mutation gap safely |
| G5 | Two unseen app architectures recognized with no vendor logic and no false safe write |
| G6 | Renderer preserves source semantics, keyboard accessibility and fallback control |
| G7 | State-changing operation verified; crash after commit produces UNKNOWN_COMMIT |
| G8 | Pinned contract source-change semantic diff/compatibility/rollback demonstration |
| G9 | Extension runs source operation while malicious webpage cannot use local gateway |
| G10 | API/CLI/MCP/UI use same contract and authorization, including unknown outcome |

## Explicit non-guarantees

A cache hit does not prove current authorization; an observed DOM does not prove hidden semantics; a typed API method does not imply original source transactionality; a paid version pin does not guarantee unavailable source features; a local-only user does not require hosted computation; zero dependencies is not itself a security property.
