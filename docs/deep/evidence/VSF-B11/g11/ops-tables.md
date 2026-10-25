# G11 measurements (B11-T06)

Same harness, same frozen denominator (operations written in the first annotation round, `fixtures/corpus/operations.frozen.json`), same Chromium 141. Recognizers: pre-B11 (d543def, legacy wire), T02 (71e7b7c), current (rules 3).

## Executable operations through recognized units

| partition | recognizer | intended | reference-confirmed | verified | fallback | abandoned | wrong effect | misbound | verified/intended | verified/confirmed (not coverage) |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| development + validation (58 pages) | pre | 142 | 124 | 93 | 26 | 5 | 0 | 0 | 0.655 | 0.75 |
| development + validation (58 pages) | t02 | 142 | 124 | 99 | 20 | 5 | 0 | 0 | 0.697 | 0.798 |
| development + validation (58 pages) | cur | 142 | 124 | 118 | 2 | 4 | 0 | 0 | 0.831 | 0.952 |
| held-out AdminLTE + TodoMVC (unsealed for diagnosis, 17 pages) | pre | 42 | 41 | 34 | 7 | 0 | 0 | 0 | 0.81 | 0.829 |
| held-out AdminLTE + TodoMVC (unsealed for diagnosis, 17 pages) | t02 | 42 | 41 | 34 | 7 | 0 | 0 | 0 | 0.81 | 0.829 |
| held-out AdminLTE + TodoMVC (unsealed for diagnosis, 17 pages) | cur | 42 | 41 | 38 | 3 | 0 | 0 | 0 | 0.905 | 0.927 |
| held-out File Browser (kept sealed, 6 pages) | pre | 15 | 10 | 8 | 1 | 1 | 0 | 0 | 0.533 | 0.8 |
| held-out File Browser (kept sealed, 6 pages) | t02 | 15 | 10 | 8 | 1 | 1 | 0 | 0 | 0.533 | 0.8 |
| held-out File Browser (kept sealed, 6 pages) | cur | 15 | 10 | 8 | 1 | 1 | 0 | 0 | 0.533 | 0.8 |

Diagnostic classes of the intended operations (verified / recognition_limit / incorrect_effect / reference_limit / annotation_defect / information_limit):

- devval pre: 93 / 26 / 0 / 4 / 15 / 4
- devval cur: 118 / 1 / 0 / 4 / 15 / 4
- open pre: 34 / 7 / 0 / 1 / 0 / 0
- open cur: 38 / 3 / 0 / 1 / 0 / 0
- sealed pre: 8 / 2 / 0 / 3 / 2 / 0
- sealed cur: 8 / 2 / 0 / 3 / 2 / 0
