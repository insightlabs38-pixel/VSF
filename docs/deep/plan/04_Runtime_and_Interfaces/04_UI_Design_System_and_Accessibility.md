# Presentation engine, UI system, accessibility and customization

## Product design direction

Neutral black/white UI; light/dark/system; restrained blue and mint accents; status-only colors (red critical/error, orange warning/elevated, yellow caution/pending, green verified success; never color alone). Minimal ornament, functional density. **VSF should not impose one fixed layout on every source**; let semantic constraints and user preferences drive arrangement.

## Native renderer responsibilities

HTML semantic DOM, CSS cascade layers and custom properties, typed TS event/state lifecycle, accessible keyboard events, fine-grained state subscriptions, keyed reconciliation, virtualized large views, focus/ARIA management, undo/audit for user layout changes and load-on-demand specialty widgets. Renderer consumes Presentation IR; source behavior remains in Semantic IR / OperationContract, not duplicated in TS components.

## Theme token groups

`color.surface`, `color.text`, `color.border`, `color.accent`, `color.error`, `color.warning`, `color.caution`, `color.success`, `density`, `spacing`, `type.scale`, `type.font`, `radius`, `focus`, `motion`, `table.row-height`, `nav.layout`, `editor.preference`. Theme variations are token layers, not divergent components.

## Customizable dimensions

- Light/dark/system and mint/blue/neutral accent; semantic colors non-negotiable for status meaning.
- Compact, balanced and spacious density; resizable panels, user-controlled typography and custom keyboard shortcuts.
- Navigation sidebar/top/command-palette/minimal; data table/list/card views when semantic equivalence permits.
- Column visibility/order, filters/grouping, section layout, shortcuts, workspace templates and contextual panels.
- Saved profiles can be personal, team, organization or per application, with revision history and conflict-aware merge.
- Versioned presentation changes must not silently alter required source confirmations or field constraints.

## Deterministic layout synthesis

Build a constraint solver that considers semantic grouping, mandatory controls, information density, reading/focus order, interaction cost, available viewport and user preferences. Compare deterministic candidate costs; hold layout stable to avoid constantly rearranging familiar workflows. A frequency-driven optimizer is *optional* and must not leak private usage telemetry by default.

## Accessibility acceptance

- Keyboard-only navigation for all common primitives; visible focus, predictable tab order, no keyboard traps and focus restoration on dialogs.
- Screen reader labels, status/live-region behavior, error descriptions and accessible alternatives for complex drag controls.
- High contrast, reflow and target sizes with WCAG 2.2 checks (automated + actual assistive-technology runs).
- Localization: RTL layout, Unicode and IME composition, timezone-aware date/time, decimal separator and unit handling; avoid converting typed scientific or financial data incorrectly.
- When specialized original surface is preserved, clearly indicate context and avoid trapping focus between VSF and source.

## Performance targets (engineering hypotheses)

Local purely rendered interaction: p95 VSF overhead <=100ms, long-task count monitored; warm interface initial display <=500ms on standard fixtures, more complex app data fetched progressively. Bundle footprint and memory tracked per surface. Terminal/editor libraries lazy-loaded only when needed. Avoid transitions that block keyboard input or introduce layout thrash.

## Test fixtures

Dense enterprise grid, multistep form, split research notebook with chart, mobile card layout, RTL financial table, locale-specific decimal input, 200k-row virtualized list, color-blind status use, screen-reader dialog, keyboard-only scientific plate grid. Verify semantic operation effects unchanged across theme/layout revisions.
