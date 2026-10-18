# Universal Interface Compiler: Initial Primitive and Capability Catalog

**Status:** Proposed engineering inventory, not an implemented or validated feature list.
**Scope:** 924 named, separately testable capabilities across 29 families.

## Interpretation
An entry represents one testable capability. Some are low-level atomic operations, some are higher-level composite capabilities, some are runtime services or reusable protocol adapters. They intentionally share a catalog for coverage planning but **must not** all be implemented as one abstraction or one giant switch statement.

Every entry should acquire an explicit detector/precondition, normalized IR mapping, authorized execution or preserved-surface fallback, verification tests and compatibility provenance. Unknown semantics must stay unknown.

## Priority definitions
- **Critical:** needed before the first broad public compatibility release.
- **Core:** substantially implemented before broad public release.
- **Extended:** specialized coverage targets; select by measured cross-application impact, not vanity counts.

## Index
- [Browser and document observation](#browser-and-document-observation) — 30 (Foundation, Critical)
- [Semantic structure and relationships](#semantic-structure-and-relationships) — 30 (Core, Critical)
- [Geometry and visual interpretation](#geometry-and-visual-interpretation) — 30 (Core, Core)
- [Interaction and event mechanics](#interaction-and-event-mechanics) — 31 (Foundation, Critical)
- [Forms and standard widgets](#forms-and-standard-widgets) — 40 (Capability, Critical)
- [Navigation, menus and selection](#navigation-menus-and-selection) — 30 (Capability, Critical)
- [Tables, grids and collections](#tables-grids-and-collections) — 39 (Capability, Critical)
- [Data model and entity semantics](#data-model-and-entity-semantics) — 34 (Core, Critical)
- [Documents and rich content](#documents-and-rich-content) — 31 (Capability, Core)
- [Files, directories and transfer](#files-directories-and-transfer) — 31 (Capability, Core)
- [Charts and numerical visualization](#charts-and-numerical-visualization) — 33 (Capability, Core)
- [Graphs, canvases and diagram editors](#graphs-canvases-and-diagram-editors) — 30 (Capability, Extended)
- [Terminals, code and computational notebooks](#terminals-code-and-computational-notebooks) — 33 (Capability, Core)
- [Scientific and laboratory capabilities](#scientific-and-laboratory-capabilities) — 36 (Capability, Extended)
- [Media, graphics and 3D](#media-graphics-and-3d) — 28 (Capability, Extended)
- [Maps, geospatial and spatial data](#maps-geospatial-and-spatial-data) — 24 (Capability, Extended)
- [Business, finance and operational constructs](#business-finance-and-operational-constructs) — 30 (Capability, Extended)
- [Date, calendar and scheduling](#date-calendar-and-scheduling) — 28 (Capability, Core)
- [Communication and multiuser collaboration](#communication-and-multiuser-collaboration) — 28 (Capability, Core)
- [Network and protocol integrations](#network-and-protocol-integrations) — 34 (Adapter, Core)
- [Identity, permissions and secrets](#identity-permissions-and-secrets) — 30 (Runtime, Critical)
- [Asynchrony, state and workflows](#asynchrony-state-and-workflows) — 34 (Core, Critical)
- [Caching, versioning and consistency](#caching-versioning-and-consistency) — 34 (Runtime, Core)
- [Generated UI and ergonomic transformations](#generated-ui-and-ergonomic-transformations) — 32 (Compiler, Critical)
- [CLI, API, MCP and SDK generation](#cli-api-mcp-and-sdk-generation) — 34 (Compiler, Critical)
- [Execution control and safe equivalence](#execution-control-and-safe-equivalence) — 32 (Runtime, Critical)
- [Validation, testing and observability](#validation-testing-and-observability) — 36 (Foundation, Critical)
- [Legacy, embedded and difficult UI interop](#legacy-embedded-and-difficult-ui-interop) — 30 (Adapter, Extended)
- [Reusable protocol and application-family adapters](#reusable-protocol-and-application-family-adapters) — 32 (Adapter, Extended)

## Catalog

### Browser and document observation

**Layer:** Foundation · **Release priority:** Critical · **Purpose:** Acquire complete authorized evidence from live applications.

- `OBS-001` Frame discovery
- `OBS-002` Nested iframe enumeration
- `OBS-003` Cross-origin frame boundary detection
- `OBS-004` Open shadow root traversal
- `OBS-005` Closed shadow root boundary detection
- `OBS-006` DOM subtree snapshot
- `OBS-007` Mutation stream subscription
- `OBS-008` Accessibility-tree snapshot
- `OBS-009` Accessible-name computation
- `OBS-010` ARIA role extraction
- `OBS-011` Computed-style snapshot
- `OBS-012` Layout rectangle capture
- `OBS-013` Paint order capture
- `OBS-014` Scroll container discovery
- `OBS-015` Focus ownership tracking
- `OBS-016` Visible text extraction
- `OBS-017` Document metadata reading
- `OBS-018` HTML template inspection
- `OBS-019` Custom-element registration detection
- `OBS-020` DOM slot projection mapping
- `OBS-021` Route and URL observation
- `OBS-022` History transition observation
- `OBS-023` Navigation timing observation
- `OBS-024` Resource loading observation
- `OBS-025` Console error capture
- `OBS-026` Document lifecycle tracking
- `OBS-027` Viewport and DPR inspection
- `OBS-028` Media-query state observation
- `OBS-029` Content security policy observation
- `OBS-030` Sandbox attribute inspection

### Semantic structure and relationships

**Layer:** Core · **Release priority:** Critical · **Purpose:** Construct stable structural meaning independently of CSS selectors.

- `STRUCT-001` Stable element identity
- `STRUCT-002` Accessible-label association
- `STRUCT-003` Heading outline reconstruction
- `STRUCT-004` Landmark identification
- `STRUCT-005` Section hierarchy recognition
- `STRUCT-006` Parent-child relationship inference
- `STRUCT-007` Sibling group recognition
- `STRUCT-008` Repeated component detection
- `STRUCT-009` List and list-item recognition
- `STRUCT-010` Definition-list recognition
- `STRUCT-011` Breadcrumb recognition
- `STRUCT-012` Toolbar recognition
- `STRUCT-013` Status and notification recognition
- `STRUCT-014` Container role normalization
- `STRUCT-015` Tooltip-owner association
- `STRUCT-016` Dialog-owner association
- `STRUCT-017` Trigger-target association
- `STRUCT-018` Form-field grouping
- `STRUCT-019` Fieldset and legend binding
- `STRUCT-020` Required-field identification
- `STRUCT-021` Error-message association
- `STRUCT-022` Description association
- `STRUCT-023` Active-descendant resolution
- `STRUCT-024` Selected-item relation
- `STRUCT-025` Expanded-content relation
- `STRUCT-026` Tree-parent relation
- `STRUCT-027` Tab-panel relation
- `STRUCT-028` Table-header association
- `STRUCT-029` Semantic confidence provenance
- `STRUCT-030` Structural fingerprinting

### Geometry and visual interpretation

**Layer:** Core · **Release priority:** Core · **Purpose:** Understand screen composition while separating data from visual design.

- `LAYOUT-001` Box-model measurement
- `LAYOUT-002` Flow layout detection
- `LAYOUT-003` Flex layout detection
- `LAYOUT-004` Grid layout detection
- `LAYOUT-005` Table layout detection
- `LAYOUT-006` Absolute-positioned region detection
- `LAYOUT-007` Sticky header detection
- `LAYOUT-008` Sticky footer detection
- `LAYOUT-009` Overlay stacking detection
- `LAYOUT-010` Viewport clipping detection
- `LAYOUT-011` Element occlusion detection
- `LAYOUT-012` Scrollable axis detection
- `LAYOUT-013` Nested scroll containment
- `LAYOUT-014` Resize behavior observation
- `LAYOUT-015` Breakpoint classification
- `LAYOUT-016` Responsive reflow detection
- `LAYOUT-017` RTL layout interpretation
- `LAYOUT-018` Writing-mode interpretation
- `LAYOUT-019` Typography role classification
- `LAYOUT-020` Visual density measurement
- `LAYOUT-021` Alignment group inference
- `LAYOUT-022` Whitespace grouping
- `LAYOUT-023` Container boundary inference
- `LAYOUT-024` Icon-text association
- `LAYOUT-025` Color semantic mapping
- `LAYOUT-026` Contrast estimation
- `LAYOUT-027` Touch-target estimation
- `LAYOUT-028` Visual affordance recognition
- `LAYOUT-029` SVG element geometry
- `LAYOUT-030` High-DPI coordinate translation

### Interaction and event mechanics

**Layer:** Foundation · **Release priority:** Critical · **Purpose:** Execute the full range of browser-authorized interaction mechanisms.

- `EVENT-001` Pointer click
- `EVENT-002` Double click
- `EVENT-003` Context menu activation
- `EVENT-004` Pointer hover
- `EVENT-005` Pointer press-and-hold
- `EVENT-006` Pointer drag
- `EVENT-007` Drag and drop
- `EVENT-008` Drag autoscroll
- `EVENT-009` Pointer capture handling
- `EVENT-010` Touch tap
- `EVENT-011` Touch long press
- `EVENT-012` Pinch gesture
- `EVENT-013` Wheel scroll
- `EVENT-014` Keyboard text entry
- `EVENT-015` Keyboard shortcut
- `EVENT-016` Modifier chord
- `EVENT-017` Keyboard navigation
- `EVENT-018` Focus and blur
- `EVENT-019` IME composition
- `EVENT-020` Clipboard copy
- `EVENT-021` Clipboard paste
- `EVENT-022` Selection range manipulation
- `EVENT-023` Native input event dispatch
- `EVENT-024` Change event observation
- `EVENT-025` Submit event observation
- `EVENT-026` Prevent-default detection
- `EVENT-027` Debounced input stabilization
- `EVENT-028` Animation completion wait
- `EVENT-029` Transition completion wait
- `EVENT-030` Hit-target revalidation
- `EVENT-031` Interaction cancellation

### Forms and standard widgets

**Layer:** Capability · **Release priority:** Critical · **Purpose:** Represent and operate common editable controls and high-frequency custom widgets.

- `FORMS-001` Single-line text input
- `FORMS-002` Multiline text area
- `FORMS-003` Password input
- `FORMS-004` Email input
- `FORMS-005` Telephone input
- `FORMS-006` URL input
- `FORMS-007` Integer input
- `FORMS-008` Decimal input
- `FORMS-009` Currency input
- `FORMS-010` Percentage input
- `FORMS-011` Date input
- `FORMS-012` Time input
- `FORMS-013` Date-time input
- `FORMS-014` Month and year input
- `FORMS-015` Range slider
- `FORMS-016` Checkbox
- `FORMS-017` Radio group
- `FORMS-018` Native select
- `FORMS-019` Custom select
- `FORMS-020` Autocomplete combobox
- `FORMS-021` Multi-select
- `FORMS-022` Tag input
- `FORMS-023` Search input
- `FORMS-024` Toggle switch
- `FORMS-025` Stepper control
- `FORMS-026` Rating selector
- `FORMS-027` Color picker
- `FORMS-028` File input
- `FORMS-029` Hidden field dependency
- `FORMS-030` Conditional form branch
- `FORMS-031` Repeating form section
- `FORMS-032` Inline validation
- `FORMS-033` Server-side validation rendering
- `FORMS-034` Input mask
- `FORMS-035` Formatted number field
- `FORMS-036` Form dirty state
- `FORMS-037` Form reset
- `FORMS-038` Form submit
- `FORMS-039` Wizard form steps
- `FORMS-040` Dependent dropdown

### Navigation, menus and selection

**Layer:** Capability · **Release priority:** Critical · **Purpose:** Preserve application routing and discover nested navigation structures.

- `NAV-001` Top navigation bar
- `NAV-002` Sidebar navigation
- `NAV-003` Expandable menu
- `NAV-004` Mega menu
- `NAV-005` Nested submenu
- `NAV-006` Contextual menu
- `NAV-007` Command palette
- `NAV-008` Tab strip
- `NAV-009` Nested tabs
- `NAV-010` Accordion
- `NAV-011` Disclosure panel
- `NAV-012` Breadcrumb route
- `NAV-013` Pagination
- `NAV-014` Infinite scrolling
- `NAV-015` Anchor navigation
- `NAV-016` In-page table of contents
- `NAV-017` Search-result navigation
- `NAV-018` Deep link resolution
- `NAV-019` Route parameter binding
- `NAV-020` Query-string parameter binding
- `NAV-021` Back and forward behavior
- `NAV-022` Multi-window navigation
- `NAV-023` New-tab action
- `NAV-024` Popup window handling
- `NAV-025` Modal navigation
- `NAV-026` Drawer navigation
- `NAV-027` Split-view navigation
- `NAV-028` Selection persistence
- `NAV-029` Recent-items menu
- `NAV-030` Favorites navigation

### Tables, grids and collections

**Layer:** Capability · **Release priority:** Critical · **Purpose:** Cover virtualized enterprise data experiences and their interaction semantics.

- `TABLE-001` Static HTML table
- `TABLE-002` ARIA data grid
- `TABLE-003` Virtualized row grid
- `TABLE-004` Virtualized column grid
- `TABLE-005` Tree grid
- `TABLE-006` Pivot table
- `TABLE-007` Cross-tab report
- `TABLE-008` Frozen row headers
- `TABLE-009` Frozen columns
- `TABLE-010` Grouped column headers
- `TABLE-011` Nested row expansion
- `TABLE-012` Row selection
- `TABLE-013` Cell selection
- `TABLE-014` Range selection
- `TABLE-015` Editable cell
- `TABLE-016` Bulk cell edit
- `TABLE-017` Row actions
- `TABLE-018` Bulk row actions
- `TABLE-019` Column resize
- `TABLE-020` Column reorder
- `TABLE-021` Column visibility
- `TABLE-022` Sort single field
- `TABLE-023` Sort multiple fields
- `TABLE-024` Header filters
- `TABLE-025` Faceted filters
- `TABLE-026` Server-side pagination
- `TABLE-027` Client-side pagination
- `TABLE-028` Infinite row loading
- `TABLE-029` Lazy detail expansion
- `TABLE-030` Export selected rows
- `TABLE-031` Aggregate footer
- `TABLE-032` Cell validation
- `TABLE-033` Copy grid range
- `TABLE-034` Paste grid range
- `TABLE-035` Spreadsheet formulas
- `TABLE-036` Cell merge and split
- `TABLE-037` Hierarchical grid grouping
- `TABLE-038` Grid keyboard navigation
- `TABLE-039` Grid loading skeleton

### Data model and entity semantics

**Layer:** Core · **Release priority:** Critical · **Purpose:** Normalize discovered data without pretending unknown semantics are known.

- `DATA-001` Entity identification
- `DATA-002` Primary-key recognition
- `DATA-003` Composite-key recognition
- `DATA-004` Record detail view
- `DATA-005` Record list view
- `DATA-006` Field type inference
- `DATA-007` Enum value discovery
- `DATA-008` Nullable field detection
- `DATA-009` Read-only field detection
- `DATA-010` Required field detection
- `DATA-011` Default value observation
- `DATA-012` Derived field observation
- `DATA-013` Nested object extraction
- `DATA-014` Collection membership
- `DATA-015` Foreign-key-like relationship
- `DATA-016` Parent-child entity relation
- `DATA-017` Many-to-many relation
- `DATA-018` Reference lookup
- `DATA-019` Autocomplete record reference
- `DATA-020` Record creation
- `DATA-021` Record update
- `DATA-022` Record deletion
- `DATA-023` Record duplication
- `DATA-024` Bulk record mutation
- `DATA-025` Search query representation
- `DATA-026` Filter expression representation
- `DATA-027` Sort order representation
- `DATA-028` Pagination cursor representation
- `DATA-029` Aggregate representation
- `DATA-030` Validation constraint capture
- `DATA-031` Operation precondition
- `DATA-032` Operation postcondition
- `DATA-033` Source provenance mapping
- `DATA-034` Stable operation identifier

### Documents and rich content

**Layer:** Capability · **Release priority:** Core · **Purpose:** Represent common text and content editors without flattening essential semantics.

- `DOC-001` Markdown viewer
- `DOC-002` Markdown editor
- `DOC-003` WYSIWYG editor
- `DOC-004` Contenteditable surface
- `DOC-005` Rich text formatting
- `DOC-006` Paragraph style
- `DOC-007` Heading style
- `DOC-008` Bulleted list editing
- `DOC-009` Numbered list editing
- `DOC-010` Nested list editing
- `DOC-011` Table insertion
- `DOC-012` Image insertion
- `DOC-013` Embedded object handling
- `DOC-014` Hyperlink insertion
- `DOC-015` Footnote and citation
- `DOC-016` Math expression editing
- `DOC-017` Equation rendering
- `DOC-018` Comment annotation
- `DOC-019` Track changes
- `DOC-020` Suggestion mode
- `DOC-021` Document outline
- `DOC-022` Document navigation
- `DOC-023` Template placeholders
- `DOC-024` Page break representation
- `DOC-025` Print layout
- `DOC-026` Document revision history
- `DOC-027` Undo and redo stack
- `DOC-028` Autosave acknowledgment
- `DOC-029` Collaborative cursor overlay
- `DOC-030` Attachment inside document
- `DOC-031` Custom inline widgets

### Files, directories and transfer

**Layer:** Capability · **Release priority:** Core · **Purpose:** Handle binary data and enterprise asset workflows.

- `FILES-001` Single-file upload
- `FILES-002` Multiple-file upload
- `FILES-003` Drag-drop upload
- `FILES-004` Chunked upload
- `FILES-005` Resumable upload
- `FILES-006` Upload progress
- `FILES-007` Upload checksum verification
- `FILES-008` Folder picker
- `FILES-009` Directory tree browser
- `FILES-010` File rename
- `FILES-011` File move
- `FILES-012` File copy
- `FILES-013` File delete
- `FILES-014` File preview
- `FILES-015` File download
- `FILES-016` Bulk download
- `FILES-017` Archive extraction
- `FILES-018` Archive creation
- `FILES-019` Versioned file history
- `FILES-020` File metadata inspection
- `FILES-021` Mime type detection
- `FILES-022` Import wizard
- `FILES-023` Export wizard
- `FILES-024` CSV parsing
- `FILES-025` TSV parsing
- `FILES-026` JSON import
- `FILES-027` XML import
- `FILES-028` Spreadsheet import
- `FILES-029` PDF preview
- `FILES-030` Attachment relationship
- `FILES-031` Object storage browser

### Charts and numerical visualization

**Layer:** Capability · **Release priority:** Core · **Purpose:** Preserve analytical interactions and reconstruct standard visual displays.

- `CHART-001` Line chart
- `CHART-002` Bar chart
- `CHART-003` Stacked bar chart
- `CHART-004` Area chart
- `CHART-005` Scatter plot
- `CHART-006` Bubble chart
- `CHART-007` Pie and donut chart
- `CHART-008` Histogram
- `CHART-009` Box plot
- `CHART-010` Violin plot
- `CHART-011` Heatmap
- `CHART-012` Treemap
- `CHART-013` Sunburst chart
- `CHART-014` Waterfall chart
- `CHART-015` Funnel chart
- `CHART-016` Gauge chart
- `CHART-017` Sankey diagram
- `CHART-018` Radar chart
- `CHART-019` Candlestick chart
- `CHART-020` Depth chart
- `CHART-021` Error bars
- `CHART-022` Confidence interval
- `CHART-023` Plot legend filtering
- `CHART-024` Axis scaling
- `CHART-025` Logarithmic axis
- `CHART-026` Chart zoom and pan
- `CHART-027` Brushing selection
- `CHART-028` Crosshair inspection
- `CHART-029` Linked chart filtering
- `CHART-030` Time-series range selection
- `CHART-031` Live data chart updates
- `CHART-032` Tabular chart fallback
- `CHART-033` Data series extraction

### Graphs, canvases and diagram editors

**Layer:** Capability · **Release priority:** Extended · **Purpose:** Support interactive relationship models, technical workflows and spatial editors.

- `GRAPH-001` Directed graph rendering
- `GRAPH-002` Undirected graph rendering
- `GRAPH-003` Graph node selection
- `GRAPH-004` Graph edge selection
- `GRAPH-005` Graph neighborhood expansion
- `GRAPH-006` Hierarchical graph layout
- `GRAPH-007` Force-directed graph layout
- `GRAPH-008` Tree visualization
- `GRAPH-009` Dependency DAG view
- `GRAPH-010` Knowledge graph view
- `GRAPH-011` Entity relationship diagram
- `GRAPH-012` Flowchart editor
- `GRAPH-013` State-machine diagram
- `GRAPH-014` Sequence diagram
- `GRAPH-015` Swimlane diagram
- `GRAPH-016` BPMN-like workflow view
- `GRAPH-017` Node drag and reposition
- `GRAPH-018` Edge creation
- `GRAPH-019` Connector routing
- `GRAPH-020` Port attachment
- `GRAPH-021` Graph zoom and pan
- `GRAPH-022` Minimap navigation
- `GRAPH-023` Canvas snapping grid
- `GRAPH-024` Alignment guides
- `GRAPH-025` Multi-object selection
- `GRAPH-026` Object grouping
- `GRAPH-027` Z-order editing
- `GRAPH-028` Diagram import and export
- `GRAPH-029` Unknown canvas region preservation
- `GRAPH-030` Canvas event coordinate mapping

### Terminals, code and computational notebooks

**Layer:** Capability · **Release priority:** Core · **Purpose:** Preserve sophisticated developer, cloud and research tooling.

- `TERMINAL-001` Terminal screen buffer
- `TERMINAL-002` Terminal PTY transport
- `TERMINAL-003` ANSI escape rendering
- `TERMINAL-004` Terminal resize
- `TERMINAL-005` Terminal scrollback
- `TERMINAL-006` Terminal copy and paste
- `TERMINAL-007` Terminal alternate screen
- `TERMINAL-008` Terminal mouse reporting
- `TERMINAL-009` Terminal keyboard sequences
- `TERMINAL-010` Terminal multi-session tabs
- `TERMINAL-011` Terminal reconnect
- `TERMINAL-012` Shell completion passthrough
- `TERMINAL-013` Code editor text model
- `TERMINAL-014` Syntax highlighting
- `TERMINAL-015` Language-server diagnostics
- `TERMINAL-016` Code folding
- `TERMINAL-017` Editor minimap
- `TERMINAL-018` Multi-cursor editing
- `TERMINAL-019` Diff editor
- `TERMINAL-020` Merge conflict editor
- `TERMINAL-021` Code search
- `TERMINAL-022` Repository tree view
- `TERMINAL-023` Integrated debugger view
- `TERMINAL-024` Notebook cell model
- `TERMINAL-025` Notebook code execution
- `TERMINAL-026` Notebook output display
- `TERMINAL-027` Notebook kernel state
- `TERMINAL-028` Notebook rich output
- `TERMINAL-029` Notebook cell ordering
- `TERMINAL-030` Notebook execution history
- `TERMINAL-031` REPL interface
- `TERMINAL-032` Progressive log streaming
- `TERMINAL-033` Command failure rendering

### Scientific and laboratory capabilities

**Layer:** Capability · **Release priority:** Extended · **Purpose:** Support specialized scientific software by composable domain-neutral structures and codecs.

- `SCI-001` Sequence viewer
- `SCI-002` Sequence range selection
- `SCI-003` Nucleotide annotations
- `SCI-004` Amino-acid annotations
- `SCI-005` Reverse complement operation
- `SCI-006` Sequence alignment viewer
- `SCI-007` Sequence translation view
- `SCI-008` Chromatogram viewer
- `SCI-009` Circular plasmid map
- `SCI-010` Restriction-site visualization
- `SCI-011` Molecular lineage graph
- `SCI-012` Plate well grid
- `SCI-013` Well multi-selection
- `SCI-014` Plate heatmap
- `SCI-015` Microplate coordinate mapping
- `SCI-016` Sample chain of custody
- `SCI-017` Specimen registry record
- `SCI-018` Notebook experiment template
- `SCI-019` Laboratory protocol steps
- `SCI-020` Protocol parameter editor
- `SCI-021` Reagent inventory location
- `SCI-022` Storage box grid
- `SCI-023` Barcode-scanner input
- `SCI-024` Units and dimensions
- `SCI-025` Measurement uncertainty
- `SCI-026` Significant-figure display
- `SCI-027` Scientific unit conversion
- `SCI-028` Scientific data table
- `SCI-029` Instrument run status
- `SCI-030` Experiment run metadata
- `SCI-031` Time-course experiment plot
- `SCI-032` Assay result viewer
- `SCI-033` Molecule structure viewer
- `SCI-034` Chemical reaction scheme
- `SCI-035` Spectral peak visualization
- `SCI-036` Image-based assay viewer

### Media, graphics and 3D

**Layer:** Capability · **Release priority:** Extended · **Purpose:** Preserve embedded rich media and specialized graphical applications.

- `MEDIA-001` Image gallery
- `MEDIA-002` Image crop and zoom
- `MEDIA-003` Image annotation
- `MEDIA-004` Vector SVG viewer
- `MEDIA-005` SVG interactive hit regions
- `MEDIA-006` Canvas 2D surface passthrough
- `MEDIA-007` WebGL surface passthrough
- `MEDIA-008` WebGPU surface passthrough
- `MEDIA-009` Video playback
- `MEDIA-010` Video scrubbing
- `MEDIA-011` Audio playback
- `MEDIA-012` Audio waveform
- `MEDIA-013` Media subtitles
- `MEDIA-014` Media speed control
- `MEDIA-015` Media frame stepping
- `MEDIA-016` Screen recording controls
- `MEDIA-017` Camera permission surface
- `MEDIA-018` Microphone permission surface
- `MEDIA-019` 3D viewport orbit
- `MEDIA-020` 3D pan and zoom
- `MEDIA-021` 3D object selection
- `MEDIA-022` 3D scene graph view
- `MEDIA-023` 3D layer visibility
- `MEDIA-024` Spatial annotation
- `MEDIA-025` CAD property inspector
- `MEDIA-026` Canvas accessibility fallback
- `MEDIA-027` Color-space awareness
- `MEDIA-028` Unknown media widget preservation

### Maps, geospatial and spatial data

**Layer:** Capability · **Release priority:** Extended · **Purpose:** Handle GIS, logistics and location-aware systems.

- `GEO-001` Interactive map viewport
- `GEO-002` Map tile loading
- `GEO-003` Map pan and zoom
- `GEO-004` Map marker selection
- `GEO-005` Marker clustering
- `GEO-006` Polyline rendering
- `GEO-007` Polygon rendering
- `GEO-008` Polygon editing
- `GEO-009` Geofence drawing
- `GEO-010` Layer toggle
- `GEO-011` Map feature tooltip
- `GEO-012` Geocoding result selector
- `GEO-013` Address autocomplete
- `GEO-014` Coordinate entry
- `GEO-015` Projection and CRS handling
- `GEO-016` GeoJSON import
- `GEO-017` GeoJSON export
- `GEO-018` Route waypoint editing
- `GEO-019` Geospatial table linkage
- `GEO-020` Spatial bounding-box filter
- `GEO-021` Map measurement tool
- `GEO-022` Time-enabled geospatial layers
- `GEO-023` Raster overlay rendering
- `GEO-024` Map legend interpretation

### Business, finance and operational constructs

**Layer:** Capability · **Release priority:** Extended · **Purpose:** Reuse generic high-level structures found throughout ERP, CRM and finance products.

- `BIZ-001` Opportunity pipeline board
- `BIZ-002` Kanban card movement
- `BIZ-003` Sales funnel stage transition
- `BIZ-004` Customer account hierarchy
- `BIZ-005` Organization contact record
- `BIZ-006` Invoice line items
- `BIZ-007` Purchase order editor
- `BIZ-008` Expense claim form
- `BIZ-009` Approval matrix
- `BIZ-010` Procurement requisition
- `BIZ-011` Budget allocation grid
- `BIZ-012` General ledger table
- `BIZ-013` Journal entry editor
- `BIZ-014` Account reconciliation view
- `BIZ-015` Transaction detail ledger
- `BIZ-016` Bank statement matching
- `BIZ-017` Multi-currency amount
- `BIZ-018` Currency rate display
- `BIZ-019` Tax breakdown
- `BIZ-020` Payment status
- `BIZ-021` Subscription schedule
- `BIZ-022` Inventory stock movement
- `BIZ-023` Lot and serial tracking
- `BIZ-024` Work order dashboard
- `BIZ-025` Service ticket queue
- `BIZ-026` SLA timer
- `BIZ-027` Role assignment matrix
- `BIZ-028` Policy exception request
- `BIZ-029` Audit trail view
- `BIZ-030` Dashboard KPI card

### Date, calendar and scheduling

**Layer:** Capability · **Release priority:** Core · **Purpose:** Preserve temporal semantics, scheduling and time-zone correctness.

- `TIME-001` Calendar month view
- `TIME-002` Calendar week view
- `TIME-003` Calendar day view
- `TIME-004` Agenda list
- `TIME-005` Event editor
- `TIME-006` Recurring event rule
- `TIME-007` Recurrence exception
- `TIME-008` Time-zone conversion
- `TIME-009` DST boundary handling
- `TIME-010` Availability grid
- `TIME-011` Free-busy blocks
- `TIME-012` Drag-to-reschedule
- `TIME-013` Resource calendar
- `TIME-014` Booking capacity
- `TIME-015` Task due date
- `TIME-016` Deadline countdown
- `TIME-017` Timeline visualization
- `TIME-018` Gantt chart
- `TIME-019` Milestone tracking
- `TIME-020` Critical path relationship
- `TIME-021` Time range picker
- `TIME-022` Date interval filter
- `TIME-023` Shift scheduling
- `TIME-024` Timesheet grid
- `TIME-025` Duration input
- `TIME-026` Business-day calendar
- `TIME-027` Reminder control
- `TIME-028` Schedule conflict warning

### Communication and multiuser collaboration

**Layer:** Capability · **Release priority:** Core · **Purpose:** Support concurrent collaboration without losing original product rules.

- `COMM-001` Comment thread
- `COMM-002` Reply nesting
- `COMM-003` Mention selector
- `COMM-004` Reaction picker
- `COMM-005` Chat conversation
- `COMM-006` Chat message streaming
- `COMM-007` AI assistant conversation surface
- `COMM-008` AI assistant tool result display
- `COMM-009` Message attachments
- `COMM-010` Notification feed
- `COMM-011` Unread status
- `COMM-012` Inbox triage
- `COMM-013` Task assignment
- `COMM-014` Presence indicator
- `COMM-015` Live cursor
- `COMM-016` Collaborative edit event
- `COMM-017` Optimistic UI update
- `COMM-018` Conflict indicator
- `COMM-019` Lock ownership display
- `COMM-020` Shared workspace navigation
- `COMM-021` User directory picker
- `COMM-022` Team selection
- `COMM-023` Activity feed
- `COMM-024` Change attribution
- `COMM-025` Notification preferences
- `COMM-026` Conversation history pagination
- `COMM-027` Approval comment thread
- `COMM-028` Web notification handoff

### Network and protocol integrations

**Layer:** Adapter · **Release priority:** Core · **Purpose:** Prefer supported typed interfaces and handle browser streaming/protocol patterns.

- `NETWORK-001` HTTP GET transport
- `NETWORK-002` HTTP POST transport
- `NETWORK-003` HTTP PATCH transport
- `NETWORK-004` HTTP DELETE transport
- `NETWORK-005` HTTP status mapping
- `NETWORK-006` Request header mediation
- `NETWORK-007` Cookie-context binding
- `NETWORK-008` CSRF token propagation
- `NETWORK-009` CORS boundary handling
- `NETWORK-010` OpenAPI discovery
- `NETWORK-011` OpenAPI schema normalization
- `NETWORK-012` JSON Schema validator
- `NETWORK-013` GraphQL schema introspection
- `NETWORK-014` GraphQL query execution
- `NETWORK-015` GraphQL mutation execution
- `NETWORK-016` WebSocket message stream
- `NETWORK-017` Server-sent event stream
- `NETWORK-018` Long polling
- `NETWORK-019` Streaming download
- `NETWORK-020` Streaming upload
- `NETWORK-021` gRPC-web adapter
- `NETWORK-022` WebDAV adapter
- `NETWORK-023` REST pagination adapter
- `NETWORK-024` Cursor pagination adapter
- `NETWORK-025` ETag conditional request
- `NETWORK-026` Rate-limit handling
- `NETWORK-027` Retry-After handling
- `NETWORK-028` Idempotency-key support
- `NETWORK-029` Webhook signature verification
- `NETWORK-030` Webhook event binding
- `NETWORK-031` Binary protocol passthrough
- `NETWORK-032` Content negotiation
- `NETWORK-033` Error payload normalization
- `NETWORK-034` Protocol capability negotiation

### Identity, permissions and secrets

**Layer:** Runtime · **Release priority:** Critical · **Purpose:** Bind every operation to the actual authorized user context.

- `AUTH-001` Existing tab session binding
- `AUTH-002` Ephemeral incognito binding
- `AUTH-003` Managed browser profile
- `AUTH-004` Remote isolated browser profile
- `AUTH-005` OAuth authorization code with PKCE
- `AUTH-006` OIDC sign-in mediation
- `AUTH-007` OAuth scoped token storage
- `AUTH-008` OAuth refresh token lifecycle
- `AUTH-009` OIDC provider discovery
- `AUTH-010` Enterprise SAML login passthrough
- `AUTH-011` Session cookie isolation
- `AUTH-012` SSO redirect traversal
- `AUTH-013` MFA interactive handoff
- `AUTH-014` Passkey interactive handoff
- `AUTH-015` Credential vault reference
- `AUTH-016` Credential origin binding
- `AUTH-017` Just-in-time secret injection
- `AUTH-018` Secret redaction
- `AUTH-019` Session expiration detection
- `AUTH-020` Session reauthentication
- `AUTH-021` Account switching
- `AUTH-022` Tenant switching
- `AUTH-023` Permission visibility check
- `AUTH-024` Per-operation authorization binding
- `AUTH-025` Delegated token scope enforcement
- `AUTH-026` Session revocation
- `AUTH-027` Session provenance tracking
- `AUTH-028` Cross-tenant cache isolation
- `AUTH-029` Local keychain integration
- `AUTH-030` Customer-managed secret backend

### Asynchrony, state and workflows

**Layer:** Core · **Release priority:** Critical · **Purpose:** Model observable state transitions without inventing hidden business semantics.

- `STATE-001` Application finite-state machine
- `STATE-002` Workflow transition graph
- `STATE-003` Modal state machine
- `STATE-004` Wizard state machine
- `STATE-005` Progress state tracking
- `STATE-006` Pending request tracking
- `STATE-007` Optimistic mutation rollback
- `STATE-008` Eventual consistency polling
- `STATE-009` Async loading indicator
- `STATE-010` Idle detection
- `STATE-011` Operation completion signal
- `STATE-012` Toast-based success detection
- `STATE-013` Error-state classification
- `STATE-014` Retryable failure classification
- `STATE-015` Navigation-before-result handling
- `STATE-016` Background job handle
- `STATE-017` Batch job progress
- `STATE-018` Cancelable operation
- `STATE-019` Undo-capable operation
- `STATE-020` Compensating action
- `STATE-021` Conditional workflow branch
- `STATE-022` Parallel workflow branch
- `STATE-023` Sequential workflow composition
- `STATE-024` Workflow precondition
- `STATE-025` Workflow postcondition
- `STATE-026` Human confirmation step
- `STATE-027` Human reauthentication step
- `STATE-028` Workflow checkpoint
- `STATE-029` Workflow resume
- `STATE-030` Workflow abort
- `STATE-031` Long-running operation handle
- `STATE-032` Action timeout classification
- `STATE-033` Operation deduplication
- `STATE-034` Unknown effect containment

### Caching, versioning and consistency

**Layer:** Runtime · **Release priority:** Core · **Purpose:** Share safe compiled artifacts while keeping mutable user data isolated and fresh.

- `CACHE-001` Compiled application artifact
- `CACHE-002` DOM signature cache
- `CACHE-003` Primitive mapping cache
- `CACHE-004` Semantic schema cache
- `CACHE-005` Compiled selector cache
- `CACHE-006` Verified action cache
- `CACHE-007` Operation trace cache
- `CACHE-008` User-scoped data cache
- `CACHE-009` Role-scoped schema variant
- `CACHE-010` Tenant-scoped schema variant
- `CACHE-011` Read-through cache
- `CACHE-012` Write-through cache
- `CACHE-013` TTL cache policy
- `CACHE-014` ETag version invalidation
- `CACHE-015` Mutation dependency invalidation
- `CACHE-016` Read-after-write verification
- `CACHE-017` Cache provenance label
- `CACHE-018` Stale data signaling
- `CACHE-019` Cross-session cache portability
- `CACHE-020` Cache schema migration
- `CACHE-021` Contract semantic versioning
- `CACHE-022` Pinned interface version
- `CACHE-023` Additive API revision
- `CACHE-024` Breaking API revision
- `CACHE-025` Source-app change detection
- `CACHE-026` Structural diff computation
- `CACHE-027` Incremental recompilation
- `CACHE-028` Compatibility mapping migration
- `CACHE-029` Schema diff report
- `CACHE-030` Cached state encryption
- `CACHE-031` Cache eviction policy
- `CACHE-032` Offline snapshot mode
- `CACHE-033` Revalidation on resume
- `CACHE-034` Historical artifact rollback

### Generated UI and ergonomic transformations

**Layer:** Compiler · **Release priority:** Critical · **Purpose:** Compile a cohesive modern interface rather than simply restyling source HTML.

- `OUTPUT-001` Semantic component rendering
- `OUTPUT-002` Responsive page template
- `OUTPUT-003` Information hierarchy optimization
- `OUTPUT-004` Navigation consolidation
- `OUTPUT-005` Workspace layout
- `OUTPUT-006` Contextual action grouping
- `OUTPUT-007` Search-first navigation
- `OUTPUT-008` Progressive disclosure
- `OUTPUT-009` Density preference
- `OUTPUT-010` Accessible color theme
- `OUTPUT-011` Dark and light theme
- `OUTPUT-012` Brand theme tokens
- `OUTPUT-013` Keyboard-first command interface
- `OUTPUT-014` Quick action panel
- `OUTPUT-015` Command search
- `OUTPUT-016` Saved view layout
- `OUTPUT-017` Pinned entity view
- `OUTPUT-018` Split-pane workspace
- `OUTPUT-019` Side-by-side comparison
- `OUTPUT-020` Universal filter bar
- `OUTPUT-021` Context-aware breadcrumb
- `OUTPUT-022` Inline edit affordance
- `OUTPUT-023` Error recovery screen
- `OUTPUT-024` Pending state indicator
- `OUTPUT-025` Skeleton loading view
- `OUTPUT-026` Mobile adaptive view
- `OUTPUT-027` Touch-first controls
- `OUTPUT-028` Print-friendly view
- `OUTPUT-029` Locale-aware formatting
- `OUTPUT-030` High-contrast mode
- `OUTPUT-031` Reduced motion mode
- `OUTPUT-032` RTL interface rendering

### CLI, API, MCP and SDK generation

**Layer:** Compiler · **Release priority:** Critical · **Purpose:** Produce stable, typed alternate interfaces from verified common operations.

- `GENERATE-001` Generic interaction RPC
- `GENERATE-002` Typed entity endpoint
- `GENERATE-003` Typed action endpoint
- `GENERATE-004` CRUD endpoint synthesis
- `GENERATE-005` Read-only operation classification
- `GENERATE-006` Mutation operation classification
- `GENERATE-007` OpenAPI document emission
- `GENERATE-008` JSON Schema emission
- `GENERATE-009` Endpoint path stability
- `GENERATE-010` Endpoint parameter binding
- `GENERATE-011` Typed response normalization
- `GENERATE-012` Error code mapping
- `GENERATE-013` Pagination contract generation
- `GENERATE-014` Async job contract generation
- `GENERATE-015` Webhook contract generation
- `GENERATE-016` CLI command tree synthesis
- `GENERATE-017` CLI flags synthesis
- `GENERATE-018` CLI interactive prompts
- `GENERATE-019` CLI table formatter
- `GENERATE-020` CLI JSON output
- `GENERATE-021` CLI streaming output
- `GENERATE-022` CLI shell completion
- `GENERATE-023` MCP tools listing
- `GENERATE-024` MCP tool argument schema
- `GENERATE-025` MCP tool execution
- `GENERATE-026` MCP resources listing
- `GENERATE-027` MCP prompt template emission
- `GENERATE-028` MCP tool risk annotation
- `GENERATE-029` MCP account context binding
- `GENERATE-030` API authentication propagation
- `GENERATE-031` Versioned interface router
- `GENERATE-032` Generated SDK stub
- `GENERATE-033` Executable API examples
- `GENERATE-034` Contract compatibility tests

### Execution control and safe equivalence

**Layer:** Runtime · **Release priority:** Critical · **Purpose:** Ensure generated operations preserve authorized behavior and observable effects.

- `SAFETY-001` Original-context execution
- `SAFETY-002` Operation dependency tracking
- `SAFETY-003` Action preflight evaluation
- `SAFETY-004` Locator resolution verification
- `SAFETY-005` Target uniqueness assertion
- `SAFETY-006` Actionability checking
- `SAFETY-007` Side-effect classification
- `SAFETY-008` Explicit destructive confirmation
- `SAFETY-009` Approval-required action gate
- `SAFETY-010` Original validation passthrough
- `SAFETY-011` Transaction acknowledgment
- `SAFETY-012` Read-after-write result verification
- `SAFETY-013` Unexpected-result quarantine
- `SAFETY-014` Safe retry policy
- `SAFETY-015` At-most-once attempt guard
- `SAFETY-016` Idempotent replay guard
- `SAFETY-017` Execution audit record
- `SAFETY-018` Origin allowlist
- `SAFETY-019` Cross-origin transition validation
- `SAFETY-020` Credential leakage prevention
- `SAFETY-021` Sensitive output redaction
- `SAFETY-022` Operation timeout budget
- `SAFETY-023` Per-user concurrency limit
- `SAFETY-024` Per-origin rate throttle
- `SAFETY-025` Circuit breaker
- `SAFETY-026` Fallback to original control
- `SAFETY-027` Unrecognized operation preservation
- `SAFETY-028` Uncertain-operation refusal
- `SAFETY-029` Rollback only when supported
- `SAFETY-030` Partial failure reporting
- `SAFETY-031` User cancellation handling
- `SAFETY-032` Execution trace replay

### Validation, testing and observability

**Layer:** Foundation · **Release priority:** Critical · **Purpose:** Prove primitive generalization and prevent regressions across application classes.

- `TEST-001` Primitive unit test
- `TEST-002` Primitive property-based test
- `TEST-003` Model-based workflow test
- `TEST-004` Differential behavior test
- `TEST-005` Golden DOM fixture
- `TEST-006` Golden accessibility fixture
- `TEST-007` Visual regression fixture
- `TEST-008` Cross-browser test
- `TEST-009` Cross-origin iframe test
- `TEST-010` Shadow DOM test
- `TEST-011` Keyboard-only test
- `TEST-012` Screen-reader semantics test
- `TEST-013` Right-to-left test
- `TEST-014` Localization test
- `TEST-015` Touch input test
- `TEST-016` Slow-network test
- `TEST-017` Offline recovery test
- `TEST-018` Partial-render test
- `TEST-019` Concurrent-mutation test
- `TEST-020` Session-expiry test
- `TEST-021` Role-permission matrix test
- `TEST-022` Contract schema regression
- `TEST-023` API compatibility test
- `TEST-024` CLI output golden test
- `TEST-025` MCP tool schema test
- `TEST-026` Unknown-widget fallback test
- `TEST-027` Application upgrade simulation
- `TEST-028` Fuzzed widget composition
- `TEST-029` Performance budget test
- `TEST-030` Memory leak detection
- `TEST-031` Behavioral coverage report
- `TEST-032` Discovery confidence report
- `TEST-033` Unmapped component report
- `TEST-034` Selector drift detection
- `TEST-035` Cross-app primitive gain metric
- `TEST-036` Reproducible failure bundle

### Legacy, embedded and difficult UI interop

**Layer:** Adapter · **Release priority:** Extended · **Purpose:** Avoid all-or-nothing failure when modern semantics are absent.

- `LEGACY-001` Server-rendered postback form
- `LEGACY-002` ASP.NET-style state field
- `LEGACY-003` Legacy nested table layout
- `LEGACY-004` Custom jQuery widget recognition
- `LEGACY-005` Iframe-hosted application
- `LEGACY-006` Cross-origin iframe preserve
- `LEGACY-007` Browser popup workflow
- `LEGACY-008` PDF-embedded form
- `LEGACY-009` Java applet incompatibility notice
- `LEGACY-010` Remote desktop streaming surface
- `LEGACY-011` VNC-like remote viewport
- `LEGACY-012` Citrix-like virtual app passthrough
- `LEGACY-013` Canvas-only control preserve
- `LEGACY-014` Closed-shadow widget preserve
- `LEGACY-015` WebGL-only control preserve
- `LEGACY-016` Custom keyboard command discovery
- `LEGACY-017` Synthetic mouse compatibility
- `LEGACY-018` Unlabeled control fallback
- `LEGACY-019` Image-backed control preserve
- `LEGACY-020` Screen-size-dependent hotspot
- `LEGACY-021` Browser download interception
- `LEGACY-022` Native file dialog delegation
- `LEGACY-023` Print dialog delegation
- `LEGACY-024` OS permission dialog handoff
- `LEGACY-025` Unsupported component quarantine
- `LEGACY-026` Plugin-required feature warning
- `LEGACY-027` Third-party cookie dependency detection
- `LEGACY-028` CSP-constrained embedding fallback
- `LEGACY-029` Browser extension bridge mode
- `LEGACY-030` Original-app sidecar mode

### Reusable protocol and application-family adapters

**Layer:** Adapter · **Release priority:** Extended · **Purpose:** Target broad shared ecosystems without making named vendor support a prerequisite.

- `ADAPTER-001` OpenAPI vendor-neutral adapter
- `ADAPTER-002` GraphQL vendor-neutral adapter
- `ADAPTER-003` OAuth provider-family adapter
- `ADAPTER-004` SAML identity-provider adapter
- `ADAPTER-005` SCIM directory adapter
- `ADAPTER-006` SSH terminal adapter
- `ADAPTER-007` WebSocket terminal adapter
- `ADAPTER-008` Jupyter kernel adapter
- `ADAPTER-009` Jupyter notebook file adapter
- `ADAPTER-010` Git repository adapter
- `ADAPTER-011` Git forge workflow adapter
- `ADAPTER-012` S3-compatible object adapter
- `ADAPTER-013` SQL query console adapter
- `ADAPTER-014` SQL metadata browser adapter
- `ADAPTER-015` PostgreSQL protocol adapter
- `ADAPTER-016` MySQL protocol adapter
- `ADAPTER-017` LDAP directory adapter
- `ADAPTER-018` CalDAV calendar adapter
- `ADAPTER-019` CardDAV contacts adapter
- `ADAPTER-020` WebDAV document adapter
- `ADAPTER-021` OpenTelemetry metrics adapter
- `ADAPTER-022` Prometheus query adapter
- `ADAPTER-023` JSON Lines streaming adapter
- `ADAPTER-024` CSV dialect autodetection
- `ADAPTER-025` FHIR resource adapter
- `ADAPTER-026` DICOM viewer adapter
- `ADAPTER-027` SBML biology model adapter
- `ADAPTER-028` FASTA sequence adapter
- `ADAPTER-029` FASTQ sequence adapter
- `ADAPTER-030` PDB molecular model adapter
- `ADAPTER-031` GeoJSON geospatial adapter
- `ADAPTER-032` ICS calendar import adapter

## Suggested implementation acceptance contract

1. **Recognize:** deterministic evidence from authorized browser observations, optional protocol metadata and user-visible behavior.
2. **Model:** typed state, operation parameters, requirements, permissions, provenance, and unknown/partial states.
3. **Execute:** use the original authorized application context, documented API or protocol. Never fabricate semantics or skip validation.
4. **Verify:** test observable outcomes, including retries, failed operations, navigation changes and concurrent mutations.
5. **Render:** a native replacement when equivalent, otherwise a protocol-backed or preserved original surface.
6. **Generate:** high-level CLI/API/MCP only for established operations; preserve a low-level interaction interface for all other supported controls.
7. **Regress:** fixture tests, model/property-based tests, cross-application differential tests, accessibility and performance budgets.

## Measuring actual coverage

- Compatibility is measured against a **declared test population**, not all conceivable websites.
- Track structural representation, feature completeness, behavioral equivalence, output interface coverage, and fallback usage independently.
- Report weighting by real task frequency and impact, as well as site count.
- Some sites cannot be fully reconstructed from the DOM because business logic, server state, and canvas-only controls may not expose sufficient semantics.
- Keep sensitive content local by default; only share redacted opt-in failure signatures.
