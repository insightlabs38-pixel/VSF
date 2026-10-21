# B11 checkpoint (interrupted by usage limit)

Branch `vsf/b11-independent-corpus`. Done: T07, T01, T08, T02, T09 (receipts in `docs/deep/queue/receipts/`). Active: VSF-B11-T10.

## T10 state
- Built and tested: `tests/corpus/src/{operations,reader,execute}.ts`, CLI `reference` and `operations`, browser tests (`pnpm --filter @vsf/corpus-tests run test:harness`), CI job `corpus`, observer `Session.elementOf`.
- Operations annotations (separate sessions) are in `fixtures/corpus/operations/` and are partial: apg, fullcalendar, jquery-file-upload, slickgrid are final; jquery-ui/choices, datatables/tabulator, django-admin and the held-out sources (adminlte, todomvc-*, filebrowser) were still being written when work stopped. The reference reader's text checks were tightened to visible text after those sessions began, so every file must be re-run with `reference` and unconfirmed or vacuous operations dropped.
- Held-out operations were not read or run by the implementer. Run held-out only with `--aggregate-only` until the T06 unseal.

## Next steps
1. `node --conditions=source tests/corpus/src/cli.ts validate`, then `reference --partition dev,validation --only <source>` for each source and fix or drop.
2. Run `operations` with `VSF_LEGACY_WIRE=1 VSF_RECOGNIZER=~/vsf-baseline/vsf-recognizer-pre-b11` (build that binary from the commit before B11-T02) and with the current build, dev and validation, then held-out with `--aggregate-only`; store results under this directory; re-rank clusters on the 58-state set (FullCalendar day links and Django actions dominate false claims).
3. Close T10 (receipt, state), then T03 composites, T04 files, T05 bulk and pagination, T06 held-out unseal, gate G11.
