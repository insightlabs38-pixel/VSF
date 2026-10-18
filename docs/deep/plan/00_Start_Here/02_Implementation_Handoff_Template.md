# Agent / engineering handoff template for each batch

Use this as a template when the implementation begins. The **executable** tasks and accepted gate are in `07_Development_Sequence/03_Executable_Task_Queue/`; follow its `00_Execution_Policy.md` including strict report, comment, and Git message limits. Replace bracketed material. Avoid sending a single agent the entire product as one coding task.

## Batch charter

- **Batch:** [Bxx]
- **Repository base commit / branch:** [SHA, branch]
- **Scope and excluded scope:** [explicit]
- **Relevant plan:** `07_Development_Sequence/batches/Bxx.md`
- **Schema/compiler/runtime versions pinned:** [manifest]
- **Required artifact outputs:** [code, docs, test results, benchmark]
- **Stop conditions:** [unknown effects, security/ABI failures, no evidence]

## Instructions

1. Inspect the current repository and existing implementation before changing files. This plan is a design baseline, not proof that subsystems exist.
2. Implement generic behavior according to the referenced IR/primitive specs; avoid vendor-specific hacks.
3. Use explicit source/user authorization and effect semantics; do not bypass application permissions.
4. Keep code efficient and maintainable; no arbitrary source file-size or line-count constraint. Add dependencies only with benefit, license and upgrade review.
5. Preserve reproducibility: pin versions, add positive/negative fixtures, run relevant tests and report failures honestly.
6. Benchmarks report native source latency separately from VSF overhead, and include CPU/memory/disk when relevant.
7. Maintain version compatibility and migration contracts; never silently change a generated API/MCP meaning.
8. Write a succinct completed-batch report. Include exact test commands, commit SHA, performance numbers, new known limitations, next recommended batch and the decision PROCEED/HOLD/REVISE/REVERT.

## Required completion report

```text
Batch, branch and commit:
Files/modules changed:
Primitive IDs implemented or changed:
IR/contract/version changes:
Features demonstrated:
Tests performed and exact results:
Performance/memory/storage delta:
Security/privacy/permissions review:
Unsupported/unknown behavior:
Open issues and next batch recommendation:
```
