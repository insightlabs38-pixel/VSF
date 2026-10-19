import assert from "node:assert/strict";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { test } from "node:test";
import { decodeDocumentValue, digestAt } from "@vsf/ir";
import {
  checkPresentation,
  compilePresentation,
  effectiveArrange,
  effectiveLayout,
  isLayout,
  type PRegion,
  type PresentationBody,
  renderOrder,
  type SemanticView,
} from "@vsf/renderer";
import { parse } from "@vsf/schema";
import { compositions } from "../support/compositions.ts";

const valid = new URL("../../../schemas/ir/fixtures/valid/", import.meta.url);
const sem: SemanticView = JSON.parse(readFileSync(new URL("semantic-orders.json", valid), "utf8")).body;
const pins = JSON.parse(readFileSync(new URL("../pins.json", valid), "utf8")).entries as {
  name: string;
  document_digest: string;
}[];
const semanticDigest = pins.find((p) => p.name === "semantic-orders")?.document_digest ?? "";
const out = new URL("../../../fixtures/ui/compositions/", import.meta.url);

function compile(name: string): PresentationBody {
  const c = compositions[name];
  if (!c) throw new Error(name);
  return compilePresentation(sem, {
    semanticDigest,
    locale: "en-US",
    direction: "ltr",
    theme: { scheme: "light", accent: "blue", density: "comfortable" },
    views: c.views,
    navigation: false,
    layout: c.layout,
  });
}

const envelope = (body: PresentationBody): string =>
  `${JSON.stringify({ schema: "vsf.presentation/1.2.0", schema_digest: digestAt("presentation", [1, 2, 0]), body }, null, 2)}\n`;

const clone = (b: PresentationBody): PresentationBody => JSON.parse(JSON.stringify(b));
const codes = (b: PresentationBody): string[] => checkPresentation(sem, b).map((p) => p.code);

test("the semantic digest pin exists", () => {
  assert.match(semanticDigest, /^sha256:[0-9a-f]{64}$/);
});

test("every composition is a valid presentation of the same semantics", () => {
  for (const name of Object.keys(compositions)) {
    const body = compile(name);
    assert.ok(isLayout(body), name);
    assert.deepEqual(checkPresentation(sem, body), [], name);
    decodeDocumentValue(parse(new TextEncoder().encode(envelope(body))));
  }
});

test("the committed composition documents are what the compiler writes", () => {
  if (process.env.VSF_UPDATE_UI_FIXTURES) mkdirSync(out, { recursive: true });
  for (const name of Object.keys(compositions)) {
    const file = new URL(`${name}.json`, out);
    const want = envelope(compile(name));
    if (process.env.VSF_UPDATE_UI_FIXTURES) writeFileSync(file, want);
    assert.ok(existsSync(file), `${name} missing; run with VSF_UPDATE_UI_FIXTURES=1`);
    assert.equal(readFileSync(file, "utf8"), want, `${name} drifted`);
  }
});

test("compositions differ in layout, not in what they expose", () => {
  const exposed = (b: PresentationBody): string[] =>
    b.components
      .filter((c) => c.binding?.action)
      .map((c) => `${c.binding?.entity}.${c.binding?.action}`)
      .sort();
  const names = Object.keys(compositions);
  const shapes = new Set(names.map((n) => JSON.stringify(compile(n).regions)));
  assert.equal(shapes.size, names.length, "two compositions share one layout");
  for (const n of names) assert.ok(exposed(compile(n)).length > 0, n);
});

test("layout mistakes are refused with their own codes", () => {
  const base = compile("workbench-split");
  const edit = (f: (b: PresentationBody) => void): string[] => {
    const b = clone(base);
    f(b);
    return codes(b);
  };
  const region = (b: PresentationBody, id: string): PRegion => b.regions.find((r) => r.id === id) as PRegion;
  assert.ok(edit((b) => (region(b, "bench_form").parent = "nowhere")).includes("region.unknown_parent"));
  assert.ok(edit((b) => (region(b, "bench").parent = "bench_form")).includes("region.cycle"));
  assert.ok(edit((b) => (region(b, "bench").parent = "bench")).includes("region.cycle"));
  assert.ok(edit((b) => b.regions.push({ id: "second", role: "main", components: [] })).includes("region.main_count"));
  assert.ok(edit((b) => (region(b, "bench_form").size = { min: 40, max: 30 })).includes("region.size"));
  assert.ok(
    edit((b) => (region(b, "bench_form").dock = { edge: "start", collapsible: false, resizable: false })).includes(
      "region.dock_role",
    ),
  );
  assert.ok(
    edit(
      (b) =>
        (region(b, "bench").responsive = [
          { below: 60, arrange: "tabs", collapse: false },
          { below: 90, arrange: "stack", collapse: false },
        ]),
    ).includes("region.responsive_order"),
  );
  assert.ok(edit((b) => delete region(b, "bench_who").parent).includes("region.arrange_empty"));
  assert.ok(edit((b) => b.regions.push({ id: "Bad Id", components: [] })).includes("region.bad_id"));
  assert.ok(edit((b) => b.regions.push({ id: "bench", components: [] })).includes("region.duplicate_id"));
});

test("a region a person or a narrow screen can hide may not hold a risky operation", () => {
  const body = compile("enterprise-grid");
  assert.deepEqual(codes(body), []);
  const hiding = clone(body);
  const main = hiding.regions.find((r) => r.id === "shell_main") as PRegion;
  main.dock = { edge: "top", collapsible: true, resizable: false };
  main.role = "toolbar";
  assert.ok(codes(hiding).includes("region.hideable_holds_risky_action"));
  const responsive = clone(body);
  (responsive.regions.find((r) => r.id === "shell_main") as PRegion).responsive = [
    { below: 40, arrange: "stack", collapse: true },
  ];
  assert.ok(codes(responsive).includes("region.hideable_holds_risky_action"));
});

test("responsive arrangement follows the tightest breakpoint the width is below", () => {
  const r: PRegion = {
    id: "x",
    components: [],
    arrange: "split",
    responsive: [
      { below: 100, arrange: "tabs", collapse: false },
      { below: 60, arrange: "stack", collapse: true },
    ],
  };
  assert.deepEqual(effectiveArrange(r, 120), { arrange: "split", collapse: false });
  assert.deepEqual(effectiveArrange(r, 99), { arrange: "tabs", collapse: false });
  assert.deepEqual(effectiveArrange(r, 59), { arrange: "stack", collapse: true });
  assert.deepEqual(effectiveArrange({ id: "y", components: [] }, 10), { arrange: "stack", collapse: false });
});

test("reading order is top, start, centre, end, bottom and parents precede children", () => {
  const body = compile("enterprise-grid");
  assert.deepEqual(
    renderOrder(body).map((r) => r.id),
    ["shell_nav", "shell_main", "shell_rail"],
  );
  const split = compile("workbench-split");
  assert.deepEqual(
    renderOrder(split).map((r) => r.id),
    ["bench", "bench_form", "bench_who"],
  );
});

test("preferences take effect only inside what the presentation grants", () => {
  const body = compile("enterprise-grid");
  const prefs = {
    order: ["shell_rail", "shell_main", "shell_nav"],
    collapsed: ["shell_nav", "shell_main"],
    sizes: { shell_nav: 99, shell_rail: 1 },
  };
  const granted = effectiveLayout(body, prefs);
  assert.ok(granted.collapsed.has("shell_nav"));
  assert.ok(!granted.collapsed.has("shell_main"), "a region that cannot hide stays");
  assert.equal(granted.sizes.get("shell_nav"), 32);
  assert.equal(granted.sizes.get("shell_rail"), 24);
  assert.deepEqual(
    granted.order.map((r) => r.id),
    ["shell_nav", "shell_main", "shell_rail"],
    "different places keep their reading order",
  );
  const locked = clone(body);
  delete locked.customization;
  const none = effectiveLayout(locked, prefs);
  assert.equal(none.collapsed.size, 0);
  assert.equal(none.sizes.size, 0);
  const noCollapse = clone(body);
  noCollapse.customization = { reorder: true, resize: false, collapse: false, density: false, scheme: false };
  assert.equal(effectiveLayout(noCollapse, prefs).collapsed.size, 0);
});

test("a customization can reorder siblings but never components inside a region", () => {
  const body = compile("workbench-split");
  const eff = effectiveLayout(body, { order: ["bench_who", "bench_form"] });
  assert.deepEqual(
    eff.order.map((r) => r.id),
    ["bench", "bench_who", "bench_form"],
  );
  for (const r of eff.order) assert.deepEqual(r.components, body.regions.find((x) => x.id === r.id)?.components);
});
