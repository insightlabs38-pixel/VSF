# Application corpus (independent set, 7 applications)

All applications are unmodified upstream releases, hosted locally on `127.0.0.1`, and started from a sealed seed (database, files, session) before every single workflow so that the reference run and the VSF run begin from equivalent state. Accounts and data are synthetic. Nothing on a public host was contacted by a workflow. Package digests: `evidence/environment.json` and the pip freeze files copied into `evidence/`.

| id | application | version (pinned) | licence | implementation family | what it adds | workflows |
|---|---|---|---|---|---|---:|
| gitea | Gitea | 28.1.0, `gitea-28.1.0-linux-amd64`, sha256 `ea93cfa4a300564ac457d3903469fea49ec5a4da37d0cebaa435f8e329ea64d0` | MIT | server-rendered Go templates with Fomantic UI and jQuery islands | enterprise administration, custom dropdowns without accessible names, modal dialogs, markdown editor, file upload | 8 |
| pocketbase | PocketBase admin UI | 0.40.4, zip sha256 `9042ec818570e79c3628dadcd0a756c1496d9e1173918ec409d133c02f82e5fa` | MIT | custom Svelte single-page application | CRUD side panels, filter expressions, "load more" lists, bulk selection with styled checkboxes, custom select, file fields, collection management | 8 |
| jupyterlab | JupyterLab | 4.6.4 with jupyter_server 2.21.1, ipykernel 7.4.0 | BSD-3-Clause | Lumino widget toolkit, CodeMirror 6, xterm.js | notebook, editor and terminal (the B12 specialist surfaces), nested menus, inline rename, upload toolbar | 8 |
| datasette | Datasette | 0.65.5 | Apache-2.0 | plain server-rendered HTML, CodeMirror 5 editor | filter forms with unnamed native selects, facets, sorting, pagination, custom SQL, CSV export | 6 |
| swagger | Swagger UI (`swagger-ui-dist`) with the official Petstore OpenAPI 3.0 description | 5.32.15; description from `swagger-api/swagger-petstore` @ `53f1fe9259ca7e7830199d99d2f719aef2f5fae3` | Apache-2.0 | React single-page application | developer console: collapsible operations, parameter forms, JSON body editor, authorization dialog; effects are verified in the server's request log | 6 |
| kanboard | Kanboard | v1.2.55 @ `e4fb64e972b0ae255a92cf4b7b03489846ebbffc`, PHP 8.3 built-in server, SQLite | MIT | PHP templates, jQuery, jQuery UI sortable | popup forms loaded on demand, native selects, drag and drop, repeated link names, confirmation dialogs | 6 |
| gradio | Gradio 6.30.0 with five demos from tag `gradio@6.30.0` (`2cdd7727442fa2a8560992e1d8e164e18e1527aa`): calculator, sentence_builder, sales_projections, zip_files | 6.30.0 | Apache-2.0 | Svelte component system, virtual dataframe, plots | machine-learning style forms (slider, radio group, multi-select), editable dataframe, plot output, file upload and generated download | 6 |

Streamlit 1.65.0 was installed into the Python environment while scoping and was **not used**. It is kept as a Stage E candidate only if the verdict is unresolved on canvas and chart controls.

## Families and the B11 corpus

B11 families: W3C APG, jQuery UI, Choices.js, DataTables, AdminLTE (Bootstrap), TodoMVC, File Browser, jQuery File Upload, Tabulator, Django admin, SlickGrid, FullCalendar. The new set shares no application, template, component library or code base with them. PocketBase and Gradio both use Svelte, with separate custom component sets; they are treated as different families but the shared compiler-level style is noted. Gitea and Kanboard both use jQuery in places; their markup and widget sets are unrelated.

## Coverage of the requested categories

| category in the brief | workflows |
|---|---|
| conventional forms and CRUD | GITEA-01, 02, 03, 05; PB-01, 02, 08; KB-01, 02, 03 |
| server-backed enterprise administration | GITEA-06, 07; PB-08 |
| tables, filtering, pagination | GITEA-04; PB-04, 05; DS-01 to 04; KB-06 |
| custom components and unusual DOM | PB-01 (custom select), JUP-06 (menu parents without a role), GR-03 (mixed widgets), SW-01 |
| SPA navigation and conditional controls | GITEA-03 (Close with Comment), PB-02, SW-05 |
| dialogs, menus, multi-step forms | GITEA-05, 07; PB-03; JUP-06; SW-05; KB-04 |
| upload, download, bulk | GITEA-08; PB-06, 07; JUP-04; DS-06; GR-06 |
| virtualized or infinite lists | PB-05 (load more). No true virtualized grid with thousands of rows: a gap |
| canvas, chart, specialized controls | JUP-01, 03, 08 (notebook, terminal), GR-05 (plot and dataframe), DS-05 (editor) |
| developer and research interfaces | JUP-*, DS-*, SW-*, GR-* |

## Safety of the setup

Destructive workflows (repository deletion, record deletion, bulk deletion, task closing, pet deletion) run only against the restored disposable copy. No application had outbound network access requirements; Gitea runs with `OFFLINE_MODE`, JupyterLab with news fetching disabled, Gradio with analytics off. Gitea and JupyterLab ran as root in the sandbox with their root-run guards explicitly acknowledged (`GITEA_I_AM_BEING_UNSAFE_RUNNING_AS_ROOT`, `--allow-root`); this has no bearing on the interface behaviour measured.

## Source of each workflow's ground truth

| application | oracle |
|---|---|
| gitea | REST API as the administrator (`/api/v1`): repository, contents, issues, labels, users |
| pocketbase | REST API as a superuser: records, files by URL, collections; list rows read from the page for read workflows |
| jupyterlab | files on disk under the server's root directory (notebook JSON, text files, directories) |
| datasette | the sealed SQLite file queried with the `sqlite3` command, compared with the rows the page shows; downloaded CSV bytes |
| swagger | request log and store of the mock API that serves the Petstore description (`harness/swagger-mock.mjs`, the mock is the system under test of Swagger UI, not part of it) |
| kanboard | the SQLite database (`tasks`, `comments`) |
| gradio | the pure function behind each demo, recomputed in the oracle (including the demo's integer truncation in the sales projection); downloaded archive opened with Python's `zipfile` |
