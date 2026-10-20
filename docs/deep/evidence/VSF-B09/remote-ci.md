# Remote CI record (B09)

Local logs in this bundle are not evidence of a remote run. This file names the remote run per head.

| Head | Run | Result |
|---|---|---|
| 6e7149c75ade | 39369265172 | FAIL: ui (102 screenshot baselines differed by 1-3% of pixels on the runner, structural checks passed); first run also failed commit-policy (an attribution footer had been appended to the PR text, removed) |
| 0dd3cb62f7a1 | 39370215969 | PASS: all 13 jobs including ui, browser, execution, wasm-browser, cross-language and gate |
| 85837aff994e | 39373847707 | PASS: all 13 jobs on the merged head (merge commit bf7e879) |

Cause of the ui failure: text rasterization depended on the host's font settings. Baselines are now rendered with fixed Chromium font flags (policy doc section 2); a local run and the runner then agree on all 102 images. The runner's failure images are uploaded with the job logs.
