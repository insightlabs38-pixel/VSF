# VSF executable development queue

This augments, rather than replaces, the 25-batch development sequence and source architecture. It converts **152 original execution steps into 152 individually testable task contracts** across **25 batches**. The number of tasks is not a measure of shipped implementation or capability maturity.

## Entry points

1. [Execution policy](00_Execution_Policy.md): non-negotiable scope, status, test, Git and reporting rules.
2. [Queue operations and receipt schema](01_Queue_Operations.md): transitions, review/gate closure and the 24-line report template.
3. [Human task ledger](TASKS.md): stable IDs, initial TODO state, linked detail.
4. [Machine-readable tasks](tasks.json): exact dependencies, intent, acceptance, planned paths and test risk matrix.
5. [Bootstrap state](state.bootstrap.json): the initial unexecuted cursor and per-task/gate states.
6. [Detailed batch task contracts](batches/B00.md): start B00; each batch has task-specific verification and a gate.
7. [Batch index](batch_index.csv): quickly filter outcomes and dependencies.
8. [Agent prompts](../../10_Agent_Prompts/README.md): first invocation and later keep-going.

## Do not misinterpret the queue

- The original source B00–B24 files remain the authoritative program gates and prerequisites.
- Work is **fixed-order one batch per agent invocation**, with readiness and gates enforced; later provisional tasks can be decomposed once experiments justify their concrete shape.
- A queue item is not complete because code exists; it needs focused tests, one edge/adversarial assertion, recorded evidence, coherent commits and a closed gate.
- During B00, promote these planning records into the implementation repository under `docs/deep/queue/`; keep a concise root `TASKS.md` pointer and checklist. Report files live under `docs/deep/reports/` and may have at most **24 physical lines**.
- User-specified Git identity and attribution constraints are binding, regardless of agent/provider.

## Sequence

`B00` foundation → `B01` browser/WASM feasibility → `B02` schemas → `B03` registry → `B04` observation → `B05` semantic inference → `B06` presentation → `B07` execution → `B08` version lifecycle → `B09` extension/local bridge → `B10` interface parity → `B11` common coverage → `B12` specialized I → `B13` storage → `B14` auth → `B15` hosted → `B16` desktop/localhost → `B17` version UX → `B18` optimization → `B19` commercialization → `B20` specialized II → `B21` browser parity → `B22` labs/team → `B23` private → `B24` release.
