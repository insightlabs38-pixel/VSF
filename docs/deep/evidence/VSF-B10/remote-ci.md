# Remote CI record (B10)

Local logs in this bundle are not evidence of a remote run. This file names the remote run per head.

| Head | Run | Result |
|---|---|---|
| a466437052c5 | 39383419850 | FAIL: commit-policy (an attribution footer had been appended to the PR text; removed) and the gate that depends on it |
| 2bf5ff0c09ca | 39383546818 | PASS: all 13 jobs including go, execution, ui, browser, wasm-browser, cross-language and gate |

Merged as aa45177 (merge commit, PR 11). The first run's only failure was the PR text; no code check failed.
