# Composition: collection.extract

Status: implemented for synthetic fixtures only (B03); no real source application was observed.

Serves TABLE-001 (static HTML table) and TABLE-002 (ARIA data grid). Both recognizers produce `collection` and one shared extraction turns the matched subtree into `{columns, rows}`.

- Recognition looks only at structure: `tag:table` or `role:grid`. Style, class and position are never evidence, so a CSS grid of the same text is not a collection.
- `role=presentation|none` opts a subtree out. `<table role=grid>` belongs to TABLE-002 by exclusion, so no input matches both.
- Rows are `tr` or `role=row`; cells are `td|th` or `gridcell|cell|columnheader|rowheader`. The first row made only of header cells names the columns.
- Hidden subtrees (`hidden`, `aria-hidden=true`) are omitted. A nested recognized collection is extracted on its own and excluded from the outer cell text.
- Text keeps its Unicode exactly (NFC and NFD stay distinct); only runs of ASCII whitespace collapse to one space.

Advancing to `conformant` needs held-out fixtures from a source other than the authors of these, adversarial coverage on real captures, and a review record. Neither exists yet.
