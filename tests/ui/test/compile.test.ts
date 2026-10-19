import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { ACCENTS, contrast, DENSITY, PALETTE } from "@vsf/design-system";
import { decodeDocumentValue, digestAt, IR_VERSION, IR_VERSION_TEXT } from "@vsf/ir";
import {
  type CompileOptions,
  checkPresentation,
  compilePresentation,
  EFFECT_ORDER,
  IdAllocator,
  type Measured,
  operationsOf,
  type PresentationBody,
  type SemanticView,
} from "@vsf/renderer";
import { parse } from "@vsf/schema";

const dir = new URL("../../../schemas/ir/fixtures/valid/", import.meta.url);
const sem: SemanticView = JSON.parse(readFileSync(new URL("semantic-orders.json", dir), "utf8")).body;
const digest = `sha256:${"29".repeat(32)}`;
const base: CompileOptions = {
  semanticDigest: digest,
  locale: "en-US",
  direction: "ltr",
  theme: { scheme: "system", accent: "mint", density: "comfortable" },
  views: [{ entity: "order", layout: "form" }],
  navigation: false,
};
const measured: Measured = {
  targetPx: DENSITY.compact.target,
  controlContrast: Math.min(
    contrast(ACCENTS.light.blue.onAccent, ACCENTS.light.blue.accent),
    contrast(ACCENTS.dark.mint.onAccent, ACCENTS.dark.mint.accent),
  ),
  textContrast: Math.min(contrast(PALETTE.light.text, PALETTE.light.bg), contrast(PALETTE.dark.text, PALETTE.dark.bg)),
};
const clone = (b: PresentationBody): PresentationBody => JSON.parse(JSON.stringify(b));
const codes = (b: PresentationBody, m?: Measured): string[] =>
  checkPresentation(sem, b, m)
    .map((p) => p.code)
    .sort();

test("a compiled form decodes as PresentationIR and carries every field and operation of the entity", () => {
  const body = compilePresentation(sem, base);
  for (const version of [[1, 0, 0] as const, IR_VERSION]) {
    const text = version === IR_VERSION ? IR_VERSION_TEXT : version.join(".");
    const doc = { schema: `vsf.presentation/${text}`, schema_digest: digestAt("presentation", version), body };
    assert.ok(decodeDocumentValue(parse(new TextEncoder().encode(JSON.stringify(doc)))), text);
  }
  assert.deepEqual(checkPresentation(sem, body, measured), []);
  const ops = operationsOf(sem, body).map((o) => o.action);
  assert.deepEqual(ops, ["delete_order", "open_order", "submit_order"]);
  const fields = body.components.filter((c) => c.binding?.field).map((c) => c.binding?.field);
  assert.deepEqual(fields, ["order_id", "total", "placed_on"]);
});

test("operations are ordered safest first and the destructive one is last, as a hard constraint", () => {
  const body = compilePresentation(sem, base);
  const order = body.constraints.find((c) => c.kind === "ordering");
  assert.deepEqual(order?.subjects, ["act_order_open_order", "act_order_submit_order", "act_order_delete_order"]);
  const swapped = clone(body);
  const grp = swapped.components.find((c) => c.id === "grp_order_actions");
  assert.ok(grp);
  grp.children.reverse();
  assert.ok(codes(swapped).includes("constraint.ordering_violated"));
});

test("forms, grids and navigation expose the same source operations", () => {
  const views = [
    { entity: "order", layout: "form" as const },
    { entity: "customer", layout: "form" as const },
  ];
  const form = compilePresentation(sem, { ...base, views, navigation: true });
  const grid = compilePresentation(sem, {
    ...base,
    views: views.map((v) => ({ ...v, layout: "grid" as const })),
    navigation: true,
  });
  assert.deepEqual(checkPresentation(sem, form, measured), []);
  assert.deepEqual(checkPresentation(sem, grid, measured), []);
  assert.deepEqual(operationsOf(sem, form), operationsOf(sem, grid));
  assert.ok(grid.components.some((c) => c.kind === "table"));
  assert.ok(form.components.some((c) => c.kind === "list" && c.accessibility.role === "navigation"));
});

test("dropping an operation, a field or a binding target is rejected", () => {
  const body = compilePresentation(sem, base);
  const noDelete = clone(body);
  noDelete.components = noDelete.components.filter((c) => c.id !== "act_order_delete_order");
  (noDelete.components.find((c) => c.id === "grp_order_actions") as { children: string[] }).children = [
    "act_order_open_order",
    "act_order_submit_order",
  ];
  noDelete.constraints = noDelete.constraints.map((k) => ({
    ...k,
    subjects: k.subjects.filter((s) => s !== "act_order_delete_order"),
  }));
  assert.ok(codes(noDelete).includes("operation.action_dropped"));
  const noTotal = clone(body);
  noTotal.components = noTotal.components.filter((c) => c.id !== "fld_order_total");
  const formOrder = noTotal.components.find((c) => c.id === "form_order");
  assert.ok(formOrder);
  formOrder.children = formOrder.children.filter((c) => c !== "fld_order_total");
  assert.ok(codes(noTotal).includes("operation.field_dropped"));
  const bogus = clone(body);
  (bogus.components.find((c) => c.id === "act_order_open_order") as { binding: object }).binding = {
    entity: "order",
    action: "wire_money",
  };
  assert.ok(codes(bogus).includes("binding.unknown_action"));
  const wrongEntity = clone(body);
  (wrongEntity.components.find((c) => c.id === "act_order_open_order") as { binding: object }).binding = {
    entity: "customer",
    action: "open_order",
  };
  assert.ok(codes(wrongEntity).includes("binding.action_entity_mismatch"));
});

test("structural defects: duplicates, unknown children, cycles, orphans and multiple parents", () => {
  const body = compilePresentation(sem, base);
  const dup = clone(body);
  dup.components.push({ ...(dup.components[1] as object) } as never);
  assert.ok(codes(dup).includes("component.duplicate_id"));
  const unknown = clone(body);
  (unknown.components[0] as { children: string[] }).children.push("ghost");
  assert.ok(codes(unknown).includes("component.unknown_child"));
  const orphan = clone(body);
  orphan.components.push({
    id: "lost",
    kind: "text",
    children: [],
    accessibility: { role: "text", name_source: "content" },
  });
  assert.ok(codes(orphan).includes("component.orphan"));
  const cyc = clone(body);
  cyc.components.push(
    { id: "ca", kind: "group", children: ["cb"], accessibility: { role: "group", name_source: "explicit", name: "a" } },
    { id: "cb", kind: "group", children: ["ca"], accessibility: { role: "group", name_source: "explicit", name: "b" } },
  );
  cyc.regions[0]?.components.push("ca");
  assert.ok(codes(cyc).includes("component.cycle"));
  const two = clone(body);
  (two.components.find((c) => c.id === "grp_order_actions") as { children: string[] }).children.push("fld_order_total");
  assert.ok(codes(two).includes("component.multiple_parents"));
});

test("accessibility defects: missing names, wrong roles, labels without fields", () => {
  const body = compilePresentation(sem, base);
  const noName = clone(body);
  (
    noName.components.find((c) => c.id === "act_order_open_order") as { accessibility: { name?: string } }
  ).accessibility.name = " ";
  assert.ok(codes(noName).includes("a11y.missing_name"));
  const role = clone(body);
  (
    role.components.find((c) => c.id === "act_order_open_order") as { accessibility: { role: string } }
  ).accessibility.role = "checkbox";
  assert.ok(codes(role).includes("a11y.role_mismatch"));
  const label = clone(body);
  (
    label.components.find((c) => c.id === "act_order_open_order") as { accessibility: { name_source: string } }
  ).accessibility.name_source = "field_label";
  assert.ok(codes(label).includes("a11y.label_without_field"));
  const content = clone(body);
  (
    content.components.find((c) => c.id === "act_order_open_order") as { accessibility: { name_source: string } }
  ).accessibility.name_source = "content";
  assert.ok(codes(content).includes("a11y.interactive_named_by_content"));
});

test("constraints: unknown subjects, keep_together, target floor and coverage are enforced", () => {
  const body = compilePresentation(sem, base);
  const sub = clone(body);
  sub.constraints[0]?.subjects.push("ghost");
  assert.ok(codes(sub).includes("constraint.unknown_subject"));
  const apart = clone(body);
  const keep = apart.constraints.find((c) => c.kind === "keep_together");
  assert.ok(keep);
  keep.subjects = ["act_order_open_order", "fld_order_total"];
  assert.ok(codes(apart).includes("constraint.keep_together_violated"));
  const tiny = clone(body);
  const target = tiny.constraints.find((c) => c.kind === "min_target_size");
  assert.ok(target);
  target.value = 10;
  assert.ok(codes(tiny).includes("constraint.target_below_floor"));
  const lax = clone(body);
  lax.constraints = lax.constraints.filter((c) => c.kind !== "min_target_size");
  assert.ok(codes(lax).includes("constraint.target_uncovered"));
  const lowContrast = clone(body);
  const cc = lowContrast.constraints.find((c) => c.id === "contrast_text");
  assert.ok(cc);
  cc.value = 3;
  assert.ok(codes(lowContrast).includes("constraint.contrast_below_floor"));
});

test("a renderer whose tokens miss a hard constraint is reported, not trusted", () => {
  const body = compilePresentation(sem, base);
  assert.ok(codes(body, { ...measured, targetPx: 20 }).includes("constraint.target_not_met"));
  assert.ok(codes(body, { ...measured, textContrast: 3 }).includes("constraint.contrast_not_met"));
  assert.ok(codes(body, { ...measured, controlContrast: 2 }).includes("constraint.contrast_not_met"));
});

test("compilation is deterministic and an unknown entity is refused", () => {
  assert.deepEqual(compilePresentation(sem, base), compilePresentation(sem, base));
  assert.throws(() => compilePresentation(sem, { ...base, views: [{ entity: "nope", layout: "form" }] }));
});

test("unsupported widgets are carried as preserved surfaces", () => {
  const body = compilePresentation(sem, {
    ...base,
    views: [{ entity: "order", layout: "form", preserved: [{ id: "chart", name: "Original chart" }] }],
  });
  const surf = body.components.find((c) => c.kind === "preserved_surface");
  assert.equal(surf?.id, "surf_chart");
  assert.deepEqual(checkPresentation(sem, body, measured), []);
});

test("operation order follows the IR danger order: destructive before indeterminate", () => {
  assert.deepEqual(
    Object.entries(EFFECT_ORDER)
      .sort((a, b) => a[1] - b[1])
      .map(([k]) => k),
    ["read_only", "idempotent_write", "conditional_write", "non_idempotent_write", "destructive", "indeterminate"],
  );
});

test("component ids stay within the limit and never collide, however the source ids differ in case or length", () => {
  const long = "a".repeat(70);
  const ids = new IdAllocator();
  const got = [
    ids.id("act", "order", "Delete"),
    ids.id("act", "order", "delete"),
    ids.id("act", "order", "DELETE"),
    ids.id("act", "order", long),
    ids.id("act", "order", `${long}b`),
    ids.id("act", "Order", "delete"),
  ];
  assert.equal(new Set(got).size, got.length);
  for (const id of got) assert.ok(/^[a-z][a-z0-9_-]{0,63}$/.test(id), id);
  const again = new IdAllocator();
  assert.deepEqual(
    [again.id("act", "order", "Delete"), again.id("act", "order", "delete")],
    got.slice(0, 2),
    "allocation is deterministic",
  );
});
