# B11-T01: failure clusters of the unmodified recognizer

Input: `corpus-baseline-dev-validation.json` (32 development and validation page states from 4 applications in 4 template families, annotated independently). The sealed held-out partition was measured once as an aggregate (`corpus-baseline-heldout.json`) and was not used to choose anything below.

## Baseline, with denominators
- Fields: 84/97 found; 96.4% of those with the right kind.
- Actions: 66/67 found; only 48.5% with an effect class that agrees (the rest have no verb or the wrong one).
- Collections: 12/12 found; 41.7% with exact columns and row count.
- Composite structures: 0/56 recognized (the recognizer has no such unit).
- Workflows: 12/54 usable (22.2%).
- False claims (usable units contradicted by the annotation): 12.

## Ranking
Order: independent template families affected, then workflows blocked, then instances. A cluster that makes a wrong executable claim (a contradicted unit) outranks a gap of equal reach. Vendor prominence and ease of making a demo pass are not inputs.

| # | cluster | instances | apps | families | pages | workflows blocked |
|---|---|---:|---:|---:|---:|---:|
| 1 | `action-effect-unknown:read` | 9 | 3 | 3 | 5 | 5 |
| 2 | `collection-contradict` | 7 | 2 | 2 | 7 | 15 |
| 3 | `composite:sortable_columns` | 8 | 2 | 2 | 8 | 5 |
| 4 | `action-effect-unknown:state_change` | 7 | 2 | 2 | 2 | 3 |
| 5 | `composite:other` | 3 | 2 | 2 | 3 | 3 |
| 6 | `action-effect-wrong:navigation-as-create` | 2 | 2 | 2 | 2 | 3 |
| 7 | `composite:dialog` | 3 | 2 | 2 | 3 | 2 |
| 8 | `composite:tabs` | 2 | 2 | 2 | 2 | 2 |
| 9 | `composite:combobox_popup` | 22 | 2 | 2 | 3 | 1 |
| 10 | `composite:menu` | 2 | 2 | 2 | 2 | 1 |
| 11 | `composite:filter_bar` | 7 | 1 | 1 | 6 | 6 |
| 12 | `action-effect-unknown:navigation` | 14 | 1 | 1 | 6 | 5 |
| 13 | `composite:pagination` | 5 | 1 | 1 | 5 | 5 |
| 14 | `field-missing:toggle` | 10 | 1 | 1 | 3 | 4 |
| 15 | `field-missing:choice` | 3 | 1 | 1 | 1 | 1 |

## Decisions: which general primitive, and why not a patch
Each group below fixes a class of pages through structure the observer or recognizer does not yet use. A vendor patch (a selector or a library name) would fix one page and leave the same cluster open on the next application; none of these groups names a library.

1. **Collection row model** (`collection-contradict`, 7 instances in 2 families, 15 workflows blocked). Footer, header and group rows are counted as data rows (DataTables `tfoot` gives 11 for 10; APG tree grids count hidden child rows). This is a wrong executable claim, not a gap, and row counts feed bulk and pagination operations, so it is first. Primitive: rows classified by section (`thead`, `tbody`, `tfoot`, `columnheader` rows) and by visibility; collection reports body data rows only.
2. **Semantic states in the observation** (underlies `action-effect-unknown:*` 30 instances in 3 families, `action-effect-wrong:*` 4, and most composite clusters). The recognizer sees tag, role, name, text and 9 flags; it cannot see `type`, `aria-expanded`, `aria-haspopup`, `aria-controls`, `aria-selected`, `aria-checked`, `aria-sort`, `aria-current`, `aria-modal`. Buttons that open a dialog are read as `create` from the word "Add", and menu, disclosure and toolbar buttons have no effect class. Primitive: an allow-listed, value-free state set carried through the observer, wire format and recognizer, versioned, with effect classes derived from state before wording.
3. **Name from content** (`field-missing:toggle`, 10 instances, 3 pages). `radio`, `checkbox`, `switch`, `tab`, `menuitem` and `option` take their name from their content; the recognizer reads only an explicit name, so radio groups are unbound and unusable. Primitive: accessible-name computation for roles that allow name from content.
4. **Composite units** (`composite:*`, 0 of the annotated structures recognized; `sortable_columns`, `dialog`, `tabs`, `menu`, `combobox_popup` each in 2 families). Primitive: a `composite` unit kind built from roles plus group 2's states, starting with dialog, menu, tabs, combobox popup and sortable columns, each with its operations and an explicit unknown when the structure is only partly visible.
5. **Filter bars and pagination** (`composite:filter_bar` 7, `composite:pagination` 5, one family each in development data). Ranked below the groups above by family count, but each blocks 5 to 6 workflows and both touch the source-rate and stale-pagination risks of the batch; they stay in T05 with an added corpus requirement (below).
6. **Uploads, virtualization, bulk selection**. The development and validation data contain no file input, no virtualized list and one bulk-selection composite, so this evidence cannot rank them or test fixes for them. The held-out data was not consulted. Per the batch's rule to follow evidence, T04 and T05 are held until the corpus has development applications that exercise them (VSF-B11-T08).

## Plan consequences
- T02: groups 1 to 3 (row model, semantic states, name from content) with registry maturity advancing only on independent evidence.
- T03: group 4 for dialog, menu, tabs, combobox popup and sortable columns; virtualization waits for T08 evidence.
- T08 (appended): add development and validation applications with uploads, virtualized grids, pagination and bulk selection; T04 and T05 depend on it.
- T06: re-run the sealed held-out partition and compare against the stored aggregate.

## Limits
Four development and validation families and 32 page states rank only what they exhibit. Ranks 11 and below have one instance in one family and are noise at this size. Annotation judgements (for example that opening a dialog is navigation, not creation) are the annotator's reading of the application, not an oracle.
