# Annotation guide

Annotations are the expected structure of a page state, written by someone who has not seen what VSF recognizes. The annotator works only from a dossier: the browser's own accessibility tree of the page state, the application's name, and a short description of what people do there. They must not read VSF source, tests, fixtures, recognizer output or other annotations of the same page.

Output: one JSON file per page, `fixtures/corpus/annotations/<source>/<page>.json`, shaped as `src/annotation.ts` (`vsf-corpus-annotation-v1`). `dossierSha256` is the sha256 of the dossier file exactly as given.

## Scope
Cover the application or widget under test wherever it sits in the page: inside the first `main` landmark, and also outside it when the controls belong to the application (a header input, footer filters). Site chrome, skip links, cookie banners, navigation to other parts of a site, and documentation or reference links (specification, issue tracker, "open in CodePen", source file links) are out of scope. Content that exists only because the page is a demo wrapper is out of scope. Scoring counts every recognized unit on the page and reports the ones nobody annotated separately, so omitting chrome costs nothing.

## Fields: things a person fills in or chooses
One entry per control: textbox, searchbox, combobox, listbox (as a chooser), checkbox, radio (each radio is one field), switch, slider, spinbutton, file chooser, date entry. `name` is the accessible name in the tree. `kind`: `text` free text or number; `choice` pick from a set (select, combobox, listbox, menu-like chooser); `toggle` on/off or one-of-group (checkbox, radio, switch); `range` slider or spinner; `file` file chooser; `date` date entry or picker; `other`. `required` only when the tree or description says so.

## Actions: things a person presses
Buttons, links and menu items that do something in the application workflow. `effect`: `read` shows or finds information without changing it (search, filter, sort, view, export, download); `state_change` creates or updates something (add, save, submit, create, rename, upload, move, copy, share); `destructive` removes or clears (delete, remove, clear, reset); `navigation` only changes what is shown (open a page or folder, close, cancel, expand, collapse, switch tab or view); `unknown` when the name does not say. Judge by what the control does in this application, not by the word alone.

## Collections: records shown as rows
Tables, grids, tree grids and lists of records. `columns` are the visible header texts in order; `rows` the number of data rows present in the tree, or null when the content is virtualized, endless or unknown.

## Composites: structures made of several controls
One entry per instance present in this state, `name` as shown. Kinds: `dialog` a dialog or modal; `menu` a menu or menubar with items; `tabs` a tab list; `pagination` page controls; `bulk_selection` a select-all or multi-select mode with selection checkboxes; `file_input` a file or folder chooser or drop target; `date_picker` a calendar chooser; `tree` an expandable hierarchy; `combobox_popup` a suggestion or option list belonging to a combobox or autocomplete; `virtual_list` a list that renders only part of its items; `sortable_columns` headers that sort; `filter_bar` search or filter inputs that narrow a collection; `editable_cell` cells edited in place; `drag_drop` interaction that needs dragging; `other`.

## Workflows
One to four per page, in the application's terms: `name`, `needs` (names of the fields, actions, collections and composites above that the workflow uses, spelled as in those entries) and `effect` of the workflow as a whole (same values as actions).

## Unsupported
Things that cannot be driven or read through the accessibility tree (canvas content, image-only controls, interactions only possible by dragging, rich editors) with the reason. Empty when none.

## Rules
Annotate only what is in the dossier. Do not guess hidden or closed content. Prefer omission to invention. Record `annotator.kind` as `separate-agent`, `annotator.id` as given in the task, `sawRecognizerOutput` false, and `basis` listing what you used.
