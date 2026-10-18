# Specialist capability packs and expansion strategy

## Pack philosophy

Capability packs are optional versioned modules that reuse semantic and interaction primitives across multiple providers. They do not bypass source controls and must include provenance, supported schemas, tests and uncertainty declarations. If a provider has a documented native API, its adapter may supply stronger semantics than the DOM alone.

## Phase-targeted packs

| Pack | Foundational behavior | Cross-domain examples | Critical fidelity probes |
|---|---|---|---|
| Data grid/spreadsheet | virtual rows, filter/sort, selection, edit, formula, export | CRM, lab registries, finance, cloud resources | virtual DOM and stale view reconciliation |
| Terminal/remote shell | PTY model, ANSI, resize, keyboard, streaming, cancellation | cloud shell, IDE, SSH admin | source-owned process, credential boundary, paste safety |
| Editor/document | rich text, selection, annotations, undo, schema, revision | ELN, documents, CMS | round-trip data and collaborative conflict |
| Code/notebooks | syntax, cells, execution, kernels, diffs, filesystem | IDE, Jupyter, cloud notebooks | execution scope and output trust |
| Graph/diagram | nodes/edges, layout, drag, cycles, SVG/canvas | infrastructure dependencies, workflows, lineage | graph semantic preservation |
| Science | dimensional units, DNA/protein, plates, registry, instrument | ELN, biology, physics | units, sequence orientation, metadata integrity |
| Visualization/CAD | WebGL/canvas, 3D view, gestures, selectors | scientific graphics, engineering, GIS | preserve original when semantic mapping unproven |
| Finance/operations | decimals, ledger, approvals, invoices, procurement | accounting, ERP, sales | precise numeric types and audit trace |
| Maps/spatial | CRS, bounds, layers, geodata, feature editing | GIS, asset management | coordinate reference and routing correctness |
| Live collaboration | WebSocket, SSE, WebRTC, presence, reconnect | conferencing, collaborative editors, support | causal ordering and conflict replay |
| Identity | OAuth/OIDC/SAML integration boundaries, session persistence | GitHub/Google/Microsoft SSO | token audience and user binding |
| File/data formats | JSON, CSV, XML, Parquet, Arrow, PDF, attachments | enterprise exports, labs, analytics | typed round-trip, avoid lossy silent coercion |
| Standard protocols | REST/OpenAPI, GraphQL, Git, S3, SQL, SSH, calendar | hundreds of services | transport permissions and contract provenance |
| Legacy surfaces | iframes, remote desktop, JavaScript-heavy old forms | government, ERP, niche enterprise | preserved surface without unsafe embedding |

## Research benchmarks by category

- Cloud console fixture: nested menus, role-dependent form, IAM policy editor, terminal, embedded assistant, monitoring graph. Existing assistant remains source AI; VSF does not call a model to compile it.
- Scientific fixture: notebook, plate 96/384 cell grid, annotated sequence, metadata registry, inventory and scientific units.
- Enterprise fixture: virtualized CRM opportunity grid, conditional approval wizard, timeouts, change of role, bulk edit with source validation.
- Developer fixture: terminal, code editor, remote file tree, a long-running build and streamed logs.
- Legacy fixture: server-rendered app with partial reload, nonsemantic HTML, nested iframe, printer/export flow.

## Sequence and buy/build decisions

Implement *generic core* first, add specialist packs as batches driven by benchmark blockers. Do not spend quarters rebuilding an entire terminal emulator or code editor when a mature, isolated implementation already passes conformance. For high-volume primitive operations, Rust/TS custom optimization may be justified after profiling.

## Plugin extension API (later)

Signed manifests, declarative registration, isolated execution, capabilities and version constraints, conformance suite and trusted distribution. Tenant customizations should never silently alter shared compiler semantics. Browser extension policies may prohibit arbitrary remote plugin logic: use packaged extensions or separately installed local/hosted workers with explicit permissions.
