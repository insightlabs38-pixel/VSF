# Varifold Systems Foundry (VSF)
## Master product, architecture and implementation program

**Working name:** Varifold Systems Foundry (VSF). **Platform:** Varifold. **Core:** Varifold Fabric. **Other product lines:** Cloud, Connect, Local, Studio. **Plan baseline:** 2026-10-18. **Status:** design and validation plan, not implemented functionality. All named prices, service-level targets and milestone estimates are hypotheses until tested.

### Non-negotiable product principles

1. **Generalized and deterministic by default.** No LLM required for compilation or ordinary execution. Optional AI is an *explicitly separate* assistive capability, not a fallback that silently fabricates semantics. AI assistants already embedded in a source application remain usable where the source permits it.
2. **Primitive-first, not per-vendor special cases.** Large coverage catalog from the start; classify and test atomic primitives, compositions, protocol adapters and infrastructure separately. Domain packs are justified when they generalize.
3. **Correctness and source authority.** VSF inherits source account permissions; it never bypasses or expands them. Additional VSF client authorization protects generated API/CLI/MCP access. Never claim a write succeeded without sufficient evidence.
4. **Performance is a product property.** Cheap cached ordinary actions; expensive discovery is incremental and bounded. Optimize latency, CPU, memory, storage and hosting costs, not *line count*. A large codebase with clear abstractions is acceptable.
5. **Multiple execution environments.** Extension, desktop, localhost companion, hosted sandbox, headless/on-demand worker, and private/self-hosted/VPC. Explicit capabilities differ; portable contracts and artifacts do not imply portable credentials.
6. **Versioning is a flagship feature.** Compiled app, source compatibility, primitive, runtime, API/CLI/MCP contracts, presentation, cache and data schemas have separately governed versions. Paid plans expand retained history, pins, migration, snapshots and managed rollback. Basic safe updates and a limited history are universal.
7. **Accessible individual economics.** Affordable extension/local UI use, with paid localhost hosting, advanced caches, cloud execution, managed persistence and private deployments. PAYG applies to genuine metered services. No billing-provider decision yet; **do not assume Stripe**.
8. **Visual restraint and customization.** Black/white light/dark theme, optional mint/blue accents, semantic red/orange/yellow/green statuses; configurable layout and density within correctness/accessibility constraints.
9. **Operational isolation.** Source app, browser, identity provider, VSF user, machine, org, tenant, execution worker, artifact origin and MCP/API caller are distinct trust domains.
10. **Test breadth and adversarial cases.** 99% is an aspiration that requires a carefully specified corpus and denominator; no claim is valid without held-out workflow-level results, failure disclosure and confidence bounds.

### Reading order

1. `00_Start_Here/01_Decisions_and_Exit_Criteria.md`: locked choices, hypotheses, gates.
2. `01_Product_and_Market/01_Product_Requirements.md` and `02_Market_and_Unit_Economics.md`.
3. `02_Architecture/01_System_Architecture.md`, `02_Contracts_and_IR.md`, `03_ADRs.md`.
4. `03_Compiler_and_Primitives/` for algorithms, inventory governance, special domains and benchmark sources.
5. `04_Runtime_and_Interfaces/` for modes, execution, versioning, UI and programmable interfaces.
6. `05_Security_and_Operations/` for identity, threats, deployments, release ops and pricing.
7. `06_Quality_and_Validation/` for objective acceptance criteria and performance workloads.
8. `07_Development_Sequence/` for the **actual build order**, including `03_Executable_Task_Queue/` with 152 actionable tasks and maintained states; later batches retain outcome-adaptive scope.
9. `08_Research_and_Reviews/` for primary-source references, first/second artifact audits and decisions after research.
10. `09_Reference_Catalog/` includes the prior **924-entry proposed catalog** and **41-gap audit**, deliberately unmodified baselines. These are requirements candidates, **not implemented code**.
11. `10_Agent_Prompts/` contains the first B00 execution prompt and the reusable one-batch continuation prompt.

### Navigation and diagrams

- Mermaid architecture diagrams live in Markdown and SVG-free `.mmd` text under `diagrams/`.
- `07_Development_Sequence/03_Executable_Task_Queue/README.md` is the actionable task ledger; `00_Master_Sequence.md` remains the authoritative batch sequence. Batch IDs and dependencies are stable planning identifiers, not promises of fixed future work.
- `07_Development_Sequence/01_Traceability.md` maps high-risk requirements to batches and verification gates.
- `08_Research_and_Reviews/02_Review_Pass_1.md` and `03_Review_Pass_2.md` record audits around an intermediate research verification pass.

### Boundary of this deliverable

No code, deployment, browser store approval, customer contract, trademark clearance, penetration test, or validated market estimate has been produced. This is the detailed *implementation plan* from which an agent or engineering team can implement and evaluate the project.
