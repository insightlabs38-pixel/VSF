Remote CI for the B14 implementation head.

- Head: 69ef2a8dc9c2b154174e004b75af130c080840ed on vsf/b14-auth-managed-sessions (PR 13).
- Run: https://github.com/insightlabs38-pixel/VSF/actions/runs/39495095761
- Result: all 12 jobs succeeded (hygiene, native, go, ts, wasm-browser, cross-language, browser, observation, execution, ui, commit-policy, failure-canary).
- The gate and report commit that follows changes only queue state, the report and this note; its own run is recorded after merge.

Final head and merge.

- Head: 56e85ba (gate, report, this note). Run https://github.com/insightlabs38-pixel/VSF/actions/runs/39495682822: all 13 checks succeeded, including gate.
- Merged as 91d6fe945ae6fbed0eccef9ab651ce5d5d3e026e (merge commit) on main.
