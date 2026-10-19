# ADR-035: UI architecture checkpoint and region layout

Status: accepted (B08 task `VSF-B08-T10`). Contract: `docs/deep/ui/00_UI_Architecture_Contract.md`.

## Context
The renderer is consistent with the plan: typed `PresentationIR`, native DOM and CSS, shared tokens, keyed updates, accessibility primitives and preserved source widgets. Its visual design and the larger product interface were not established, and only form and grid layouts existed. Those are early-stage limits that must not become permanent.

## Decisions
1. **Keep the renderer.** No framework and no rewrite. Native DOM and CSS with a closed shadow root stay, because the surface must ship inside a content script, be reviewable under extension policy and cost little. A framework would need comparative evidence and its own ADR.
2. **Two surfaces on one design system.** The management shell and the generated interface share tokens and components; only the generated surface is bound to source semantics.
3. **Layout is data, additive, in IR 1.2.0.** Region role, arrangement, parent, order, size in ch, dock, responsive breakpoints and a customization grant. Old documents are unaffected and refuse the new fields.
4. **Safety is structural.** A hideable region may not hold a non-read-only, non-idempotent operation; customization moves regions only; saved preferences are ignored without a grant; invalid layouts are never drawn. The same rules run in Rust (authority) and in the renderer.
5. **Reading order is defined** (top, start, centre, end, bottom, then order) so keyboard order is predictable and independent of visual placement.
6. **Responsiveness without a framework.** A container-width observer in ch selects the arrangement for each region; narrow defaults stack the page. Tabs reuse the same child regions with tab semantics and arrow-key navigation.

## Consequences
New layouts are new data plus, at most, new CSS for an arrangement. The management shell and the screenshot-verified gallery are B09 work on this foundation. Visual quality is not claimed until real screenshots exist.

## Rejected
- Per-application templates in the renderer (one-off vendor styling).
- Pixel sizes in the IR (breaks with density and zoom).
- Letting preferences reorder components (can detach an operation from its confirmation).
