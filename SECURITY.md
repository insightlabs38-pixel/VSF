# Security policy

Report suspected vulnerabilities privately through GitHub private vulnerability reporting on this repository. Do not open public issues for them.

Baseline invariants (see `docs/deep/plan/00_Start_Here/01_Decisions_and_Exit_Criteria.md`, D4):

- Source-application permissions are never bypassed or widened.
- Secrets never enter logs, goldens or evidence; `schemas/redaction/patterns.v1.json` is enforced in Rust and TypeScript.
- Unknown or conflicting evidence fails closed.
- Lockfiles and the advisory baseline are checked by `dep-review` on every CI run.
