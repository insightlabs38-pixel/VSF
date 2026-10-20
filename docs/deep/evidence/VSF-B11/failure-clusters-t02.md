# B11-T02: row model, semantic states and name-from-content

Changes: the observer now records allow-listed, value-free `states` (aria expanded, pressed, selected, checked, current, sort, haspopup, modal, required, readonly, multiselectable, input `type`, `multiple`, details/dialog open) and an `aria-controls` relation; the recognizer reads them. Collections find rows through row groups and plain wrapper elements, ignore footer and hidden rows, and take columns from header cells only. Radios, checkboxes, switches, tabs and options without a label name themselves from their content. File and date inputs get their own kind. Actions with no lexical verb get a toggle or reveal hint from `pressed` or `expanded`/`haspopup`.

## Same 41 development and validation states, before and after (capture digests unchanged: 41/41)
| measure | T08 baseline | after T02 |
|---|---:|---:|
| fields found | 93/109 | 103/109 |
| actions found / agreeing effect | 98/100 / 46.9% | 98/100 / 54.1% |
| collections found / agreeing shape | 12/20 / 41.7% | 19/20 / 84.2% |
| workflows usable | 13/71 | 22/71 |
| false claims (all kinds) | 16 | 12 (8 are wrong effect hints, 4 are structural) |
| composites | 0/71 | 0/71 |

Recognizer p50 25.9 ms, max 41.1 ms, 2716 nodes max, 11 MB peak RSS.

## What remains, by families affected
`action-effect-unknown` (4 families), `composite:sortable_columns` (3), `composite:bulk_selection` (3), `composite:pagination`, `composite:filter_bar`, `composite:dialog|menu|tabs|tree|editable_cell` (2 each), `composite:combobox_popup` (22 instances, 2 families).

## Known wrong readings
- Three Tabulator tables (selection, tree data, full demo) are now found but the shape is wrong: nested or partial rows count as data rows and a checkbox column has no header text. They were absent before, so these are newly visible false claims, not new regressions.
- A date-picker combobox reads as `choice`, not `date`.
- Effect hints from names still misread dialog openers ("Add ...", "Create ...") as create and "Cancel upload" as dismiss; they stay candidates until a witness exists.
- Property state that scripts change without an attribute (a native checkbox's `checked`) is not observable and is not claimed.

The held-out partition was not read or evaluated for this task.
