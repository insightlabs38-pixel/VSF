# B11-T08: development corpus extended for uploads, pagination and bulk selection

Added to the development partition: jQuery File Upload (family `blueimp-upload`, 2 states: empty, two files queued) and Tabulator (family `tabulator`, 7 states: basic, sorting and filtering, editing, pagination, selection, tree data, full demo). A third candidate, w2ui, was dropped: its grid cells reach the accessibility tree empty in this setup, so the captured state would be degraded. Both new sources are pinned, licensed, hermetic (third-party scripts served from pinned npm files, Tabulator's unshipped `dist` from its published package of the same version) and annotated by separate sessions from frozen dossiers.

Dossiers are now committed under `fixtures/corpus/dossiers` and capture no longer overwrites them. Four File Browser annotations were redone against the frozen dossiers because the first dossiers had been regenerated; the seal records this (`revisions`) and no recognition change happened in between.

## Unmodified recognizer on 41 development and validation states (6 families)
- Fields 93/109 found; actions 98/100 found, 46.9% with an agreeing effect; collections 12/20 found.
- Composites 0/71; workflows 13/71 usable; false claims 16.

## What the new data adds to the ranking
| cluster | instances | families | pages | workflows blocked |
|---|---:|---:|---:|---:|
| `composite:sortable_columns` | 11 | 3 | 11 | 7 |
| `composite:bulk_selection` | 5 | 3 | 5 | 3 |
| `collection-contradict` | 7 | 2 | 7 | 15 |
| `collection-missing` | 8 | 2 | 8 | 13 |
| `composite:filter_bar` | 9 | 2 | 8 | 8 |
| `composite:pagination` | 6 | 2 | 6 | 7 |
| `composite:editable_cell` | 3 | 2 | 3 | 2 |
| `composite:tree` | 2 | 2 | 2 | 1 |
| `composite:file_input` | 2 | 1 | 2 | 2 |
| `field-missing:file` | 2 | 1 | 2 | 2 |

Annotated composite kinds in these 41 states: combobox_popup 22, sortable_columns 11, filter_bar 9, pagination 6, bulk_selection 5, dialog 3, editable_cell 3, other 3, menu 2, tree 2, tabs 2, file_input 2, date_picker 1.

## Consequences
- `collection-missing` is new: all 7 Tabulator states (an ARIA grid whose rows sit inside `rowgroup` wrappers) and the jQuery File Upload queued-files list (rows that are not table rows) yield no collection at all, 8 states in 2 families blocking 13 workflows. It belongs to T02 with the row model.
- `bulk_selection` (3 families) and `file_input` with `field-missing:file` (the recognizer cannot see input type) now have development evidence, so T04 and T05 can proceed on it.
- Virtualized or windowed lists: none of the 41 states is annotated `virtual_list` and none of the independent applications found renders rows on demand in the captured state. Virtualization therefore has no independent evidence; T03 must not claim it, and G11 will report it as not demonstrated unless an application showing it is added.
- Validation has one family (DataTables) and nothing for the new areas; fixes for uploads, bulk selection and pagination can be checked only on the sealed held-out partition at T06.
