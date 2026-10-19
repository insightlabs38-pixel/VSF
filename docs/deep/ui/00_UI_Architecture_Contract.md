# VSF UI architecture contract

Status: accepted for B08 (task `VSF-B08-T10`); the integrated reference and screenshot regression close in B09 (`VSF-B09-T09`). Decision record: ADR-035. Layout schema: IR 1.2.0 `PresentationIR` (additive).

## 1. Two surfaces, one system
| Surface | Owner | Built from | Bound to a source? |
|---|---|---|---|
| **A. Management shell** | VSF (first party) | the same design tokens and renderer components, hosted by extension pages, localhost and hosted web | No. It shows applications, versions, permissions, environments, storage and preferences. |
| **B. Generated interface** | the compiler | `PresentationIR` rendered by the renderer | Yes. Every field and operation is bound to `SemanticIR`; unsupported widgets stay as preserved surfaces. |

Both draw from `@vsf/design-system` tokens and the renderer's component set. Surface B must adapt to very different source applications; surface A may be more opinionated. Neither may carry business logic: presentation components display state and dispatch named operations, and the runtime decides whether an operation may run.

## 2. Shell information architecture
```
Workspace  >  Application (origin, variant)  >  Interface version (channel, short id)
  Application sections:  Interface | Versions | Permissions | Execution environments | Storage and cache | Activity
  Workspace sections:    Applications | Team (later) | Private deployments (later) | Billing (later)
  Global:                Preferences | Command palette | Help
```
- **Location is always visible**: a breadcrumb names workspace, application and interface version. A pinned or revoked version shows its state beside its id.
- **Statuses** use the four semantic tokens (success, caution, warning, danger) plus a glyph, never colour alone. The compatibility states `compatible`, `degraded` and `blocked` (B08 pin resolution) map to success, caution and danger; `revoked` is danger with the reason.
- **Execution environments** (browser extension, localhost, hosted, headless, private) are listed as peers with their capability digest and authorization state; unavailable modes are marked unavailable, never hidden.
- **Version affordances** (not the B17 workflow): history list from the lineage graph, a compatibility banner from `resolve`, a pin toggle, and a diff view that renders the machine-readable report (`vsf-diff-report-v1`): classes, review flag and the selected tests. Migration views are read-only until B17.

## 3. Extension popup versus full-page workspace
| | Popup | Workspace page |
|---|---|---|
| Purpose | status and quick actions for the current tab: connected or not, pinned version, compatibility banner, open workspace, switch back to the source UI | everything in section 2, plus the generated interface at full size |
| Limits | small fixed width, no persistent layout, no editing of preferences beyond theme and density | resizable regions, customization, version history, diffs |
| Rule | the popup never runs a write; it links to the workspace or the source UI | writes run only from a rendered operation with its confirmation context |

## 4. Design system
Tokens are the only source of colour, spacing and size (`frontend/packages/design-system`).
- **Scheme** light, dark, system. **Accent** neutral (`none`), blue, mint. **Density** compact (target 32px), comfortable (44px), spacious (52px). Preferences are three independent attributes on the host.
- **Type and spacing** scale with the density font size; layout widths are in `ch` so they follow text, not pixels.
- **Surfaces** page, surface and raised surface; borders use `border` for structure and `control-border` for controls (meets 3:1 on every surface).
- **Accessibility invariants** (enforced by the presentation constraints and tests): interactive targets at least 24px (hard floor) and at least the density target; text contrast 7:1 and control contrast 4.5:1 for the shipped palettes; visible focus ring; logical properties only (LTR and RTL); `prefers-reduced-motion`, `forced-colors` and keyboard-only operation; every landmark and control has a name.
- **Variants** are exercised in B09 screenshots: light/dark, desktop/tablet/phone, compact/comfortable, and the states normal, empty, loading, unavailable, error, unknown, conflicted.

## 5. Layout model (PresentationIR 1.2.0)
A `Region` gains optional, versioned fields; a presentation without them renders exactly as before.
- `role`: navigation, header, main, aside, toolbar, footer. At most one `main` per level.
- `arrange`: stack, row, grid, split, tabs. Split and tabs need at least two child regions.
- `parent`, `order`: nesting and sibling order. **Reading and keyboard order is top, start, centre, end, bottom, then `order`, then declaration; parents precede children.** Components keep their authored order inside a region.
- `size` (min, max, basis in ch) and `dock` (edge, collapsible, resizable) for edge-attached regions; `responsive` breakpoints (below N ch: another arrangement, optional collapse).
- `customization`: which of reorder, resize, collapse, density and scheme a person may change.

Hard rules (validated in Rust and mirrored in the renderer; a presentation that breaks one is never drawn):
1. Ids are identifiers; parents exist and form no cycle.
2. Sizes are consistent; a dock needs an edge-capable role; breakpoints descend.
3. **A region that can be hidden (collapsible dock or collapsing breakpoint) may not hold a component bound to an operation that is not read-only or idempotent**, so a destructive or non-idempotent action never disappears from its confirmation context.
4. Customization changes regions only. It cannot reorder components, cannot hide a region that is not hideable, cannot resize past the authored bounds, and never touches `keep_together` or `ordering` constraints. Saved preferences are ignored when the presentation grants no customization.
5. Every accepted change is a numbered revision with a bounded undo stack; `resetPreferences` returns to the authored layout.

## 6. Component ownership and lifecycle
- **Data** (`PresentationIR`): component kinds, bindings, accessibility, constraints, regions. Versioned and compatible.
- **Renderer** (`frontend/packages/renderer`): one DOM component per kind, keyed by stable id, updated in place; the region host owns region frames, tabs and collapse; the palette lists operations from the presentation.
- **Runtime** (provided by the host): semantic view, value store, availability, dispatch. The renderer never calls a source.
- **Lifecycle**: `update(body)` keeps instances with the same id, restores focus to the same operation, and refuses an invalid or foreign-digest presentation before drawing anything. Events are native DOM events inside a closed shadow root; script-made clicks are ignored.
- **Extension compliance**: no remote code; compiled presentations are data; styles are bundled tokens; the host page's styles never reach the renderer except through the guarded source-style layer.

## 7. Compositions
All four use the same semantics (`semantic-orders`) and renderer; committed as documents in `fixtures/ui/compositions/`.
| Composition | Layout | Narrow behaviour |
|---|---|---|
| `enterprise-grid` | docked navigation, a main grid, a docked rail; all resizable within bounds | navigation and rail collapse behind buttons; regions stack |
| `grouped-form` | navigation, a grouped form, a side summary | groups stay; side summary collapses |
| `workbench-split` | two panels, one hosting a preserved original surface | split becomes tabs below 90 ch |
| `master-detail` | grid beside a detail form | split becomes tabs below 100 ch |
The management shell is a fifth composition owned by B09: navigation region (workspace and application), header breadcrumb, main section area and a status rail, using these same regions.

## 8. Tests that hold this contract
- Rust: layout rule cases (`semantic/cases.json`), the compositions validated against their semantics, risky-action rule, old documents refuse layout fields.
- TypeScript: composition compile and drift, mirrored rules, responsive and customization functions (22 unit tests).
- Chromium: 19 layout tests across desktop, tablet and phone: no horizontal overflow, no operation lost or hidden, tabs and collapse by keyboard, preserved surface inside a panel, reversible customization, rejected layouts never drawn.
- Existing renderer, accessibility, visual-contract and incremental-update suites are unchanged and pass.
B09 adds screenshot baselines and the integrated shell and gallery.

## 9. Limits
No drag handles or live region reordering UI yet (preferences API only); no team or organization profiles; shell screens are specified, not built; pixel appearance is not yet verified (computed-style contract only until B09).
