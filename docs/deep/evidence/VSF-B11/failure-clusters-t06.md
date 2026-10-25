# B11-T06: failure clusters that remain (rules 3)

Counts are instances over the annotated items of the partition; the sealed File Browser is shown only as aggregate clusters.

## Development + validation (58 pages)

| cluster | instances | sources | workflows blocked |
|---|---:|---:|---:|
| action-effect-unknown:state_change | 18 | 5 | 6 |
| action-effect-unknown:read | 10 | 4 | 6 |
| collection-contradict | 7 | 3 | 8 |
| action-effect-wrong:navigation-as-create | 12 | 3 | 5 |
| action-effect-unknown:navigation | 11 | 3 | 4 |
| composite:editable_cell | 4 | 3 | 3 |
| action-missing:navigation | 7 | 2 | 5 |
| composite:other | 3 | 2 | 3 |
| action-missing:state_change | 5 | 2 | 2 |
| action-effect-wrong:read-as-navigate | 9 | 2 | 1 |
| field-kind:date-as-text | 3 | 2 | 0 |
| field-kind:choice-as-text | 2 | 2 | 0 |

## Held-out AdminLTE + TodoMVC (17 pages, unsealed)

| cluster | instances | sources | workflows blocked |
|---|---:|---:|---:|
| action-effect-wrong:read-as-navigate | 14 | 5 | 5 |
| collection-missing | 2 | 2 | 2 |
| action-effect-unknown:state_change | 6 | 1 | 4 |
| composite:filter_bar | 4 | 4 | 4 |
| action-effect-unknown:read | 5 | 1 | 3 |
| field-missing:toggle | 2 | 1 | 2 |
| action-effect-unknown:navigation | 9 | 1 | 1 |
| action-effect-unknown:unknown | 2 | 1 | 1 |
| action-effect-wrong:read-as-commit | 1 | 1 | 1 |
| action-effect-wrong:state_change-as-navigate | 1 | 1 | 1 |
| composite:drag_drop | 1 | 1 | 1 |
| action-effect-wrong:navigation-as-commit | 1 | 1 | 0 |

## Held-out File Browser (6 pages, aggregate)

| cluster | instances | sources | workflows blocked |
|---|---:|---:|---:|
| collection-missing | 4 | 1 | 6 |
| composite:dialog | 2 | 1 | 3 |
| action-effect-unknown:read | 32 | 1 | 1 |
| action-missing:state_change | 2 | 1 | 1 |
| field-kind:range-as-text | 2 | 1 | 1 |
| action-effect-unknown:state_change | 1 | 1 | 1 |
| action-effect-wrong:navigation-as-commit | 1 | 1 | 1 |
| composite:tabs | 1 | 1 | 1 |
| action-effect-unknown:navigation | 16 | 1 | 0 |
| action-effect-wrong:navigation-as-destroy | 4 | 1 | 0 |
| composite:bulk_selection | 4 | 1 | 0 |
| composite:sortable_columns | 4 | 1 | 0 |

## Reading the clusters
- `action-effect-unknown:*` are controls whose wording and structure give no effect class (icon buttons named by their text, "Copy", "Paste"); they stay candidates.
- `action-effect-wrong:read-as-navigate` are links and filters the annotators call `read` and the structure calls `navigation`; both are on the read side of the write line.
- `collection-missing` on the sealed File Browser is a listing of `div role=button` items split by headings into two groups; no rule here joins them, and no File Browser-specific rule was added.
- `composite:drag_drop`, lazily built popups (jQuery UI autocomplete suggestions) and Choices.js text inputs (a hidden original input and its visible clone are both exposed) are not recognized.
- The held-out File Browser was examined page by page once, after the final measurement, to write this note; nothing was changed afterwards.
