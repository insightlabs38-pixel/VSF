import assert from "node:assert/strict";
import { test } from "node:test";
import {
  applyFilters,
  type ChartModel,
  compareDecimal,
  type Filter,
  fromSource,
  MAX_MARKS,
  parseDecimal,
  readMark,
  type SourceChart,
  sameData,
  toTable,
} from "../src/chart.ts";

const source = (over: Partial<SourceChart> = {}): SourceChart => ({
  source: "sales",
  kind: "bar",
  title: "Revenue by quarter",
  xLabel: "Quarter",
  yLabel: "Revenue (USD)",
  series: [
    { id: "eu", name: "Europe", hidden: false },
    { id: "us", name: "United States", hidden: true },
  ],
  marks: [
    { name: "Q1, 12.50", series: "eu" },
    { name: "Q2, 0.30", series: "eu" },
    { name: "Q1, 1200000.125", series: "us" },
    { name: "Q2, -4", series: "us" },
  ],
  surface: "svg",
  ...over,
});

const model = (): ChartModel => {
  const m = fromSource(source());
  assert.ok("points" in m);
  return m;
};

test("marks become points with identity from the source and the exact text of every value", () => {
  const m = model();
  assert.deepEqual(
    m.points.map((p) => [p.id, p.x, p.y]),
    [
      ["eu/Q1", "Q1", "12.50"],
      ["eu/Q2", "Q2", "0.30"],
      ["us/Q1", "Q1", "1200000.125"],
      ["us/Q2", "Q2", "-4"],
    ],
  );
  const t = toTable(m);
  assert.deepEqual(
    t.rows.map((r) => r.cells),
    [
      ["Europe", "Q1", "12.50"],
      ["Europe", "Q2", "0.30"],
      ["United States", "Q1", "1200000.125"],
      ["United States", "Q2", "-4"],
    ],
  );
  assert.equal(t.rows[2]?.hidden, true);
  assert.equal(t.caption, "Revenue by quarter");
});

test("trailing zeros, long fractions and big numbers are never rounded or reformatted", () => {
  const m = fromSource(
    source({
      marks: [
        { name: "A, 0.1000000000000000055511151231257827", series: "eu" },
        { name: "B, 9007199254740993", series: "eu" },
      ],
    }),
  );
  assert.ok("points" in m);
  assert.deepEqual(
    toTable(m).rows.map((r) => r.cells[2]),
    ["0.1000000000000000055511151231257827", "9007199254740993"],
  );
});

test("decimal comparison is exact where floating point is not", () => {
  const d = (s: string) => parseDecimal(s) as NonNullable<ReturnType<typeof parseDecimal>>;
  assert.equal(compareDecimal(d("0.1"), d("0.10")), 0);
  assert.equal(compareDecimal(d("9007199254740993"), d("9007199254740992")), 1);
  assert.equal(compareDecimal(d("-0.0"), d("0")), 0);
  assert.equal(compareDecimal(d("-2"), d("1")), -1);
  assert.equal(compareDecimal(d("-3"), d("-2")), -1);
  assert.equal(parseDecimal("1,234.5"), null);
  assert.equal(parseDecimal("1e3"), null);
  assert.equal(parseDecimal(""), null);
});

test("filters keep the same points in the chart and in its table", () => {
  const m = model();
  const cases: Filter[][] = [
    [],
    [{ field: "series", op: "eq", value: "eu" }],
    [{ field: "y", op: "gte", value: "1" }],
    [{ field: "y", op: "lte", value: "0.30" }],
    [{ field: "x", op: "in", value: ["Q2"] }],
    [
      { field: "series", op: "ne", value: "eu" },
      { field: "y", op: "gte", value: "-10" },
    ],
  ];
  for (const f of cases) assert.ok(sameData(m, f), JSON.stringify(f));
  assert.deepEqual(
    applyFilters(m.points, cases[2] as Filter[]).map((p) => p.id),
    ["eu/Q1", "us/Q1"],
  );
  assert.deepEqual(
    applyFilters(m.points, cases[3] as Filter[]).map((p) => p.id),
    ["eu/Q2", "us/Q2"],
  );
  const t = toTable(m, cases[1] as Filter[]);
  assert.equal(t.omitted, 2);
  assert.equal(t.rows.length, 2);
});

test("a numeric filter never keeps a value it cannot read exactly", () => {
  const m = fromSource(
    source({ marks: [{ name: "Q1, 5", series: "eu" }], series: [{ id: "eu", name: "Europe", hidden: false }] }),
  );
  assert.ok("points" in m);
  const odd = { id: "x", series: "eu", x: "Q1", y: "5 units", unit: null };
  assert.deepEqual(applyFilters([odd], [{ field: "y", op: "gte", value: "1" }]), []);
  assert.deepEqual(applyFilters([m.points[0] as never], [{ field: "y", op: "gte", value: "abc" }]), []);
});

test("canvas and image charts are unsupported, with a reason and the source interface retained", () => {
  for (const surface of ["canvas", "image"] as const) {
    const r = fromSource(source({ surface }));
    assert.ok("supported" in r && r.supported === false);
    assert.equal(r.fallback, "retain_source_ui");
    assert.ok(r.reason.length > 10);
  }
});

test("data that cannot be read exactly is refused instead of guessed", () => {
  for (const marks of [
    [{ name: "Q1", series: "eu" }],
    [{ name: "Q1, about 12", series: "eu" }],
    [{ name: "Q1, 12", series: "nobody" }],
    [
      { name: "Q1, 12", series: "eu" },
      { name: "Q1, 13", series: "eu" },
    ],
    [],
  ]) {
    const r = fromSource(source({ marks }));
    assert.ok("supported" in r, JSON.stringify(marks));
  }
});

test("a source-provided identity beats position and survives reordering", () => {
  const a = fromSource(
    source({
      marks: [
        { name: "Q1, 1", series: "eu", id: "row-9" },
        { name: "Q2, 2", series: "eu", id: "row-3" },
      ],
    }),
  );
  const b = fromSource(
    source({
      marks: [
        { name: "Q2, 2", series: "eu", id: "row-3" },
        { name: "Q1, 1", series: "eu", id: "row-9" },
      ],
    }),
  );
  assert.ok("points" in a && "points" in b);
  assert.deepEqual(a.points.map((p) => p.id).sort(), b.points.map((p) => p.id).sort());
});

test("mark names are read narrowly", () => {
  assert.deepEqual(readMark("Q1: 12.5"), { x: "Q1", y: "12.5", unit: null });
  assert.deepEqual(readMark("North America, 7%"), { x: "North America", y: "7", unit: "%" });
  assert.equal(readMark("12.5"), null);
  assert.equal(readMark("Q1, 1e5"), null);
});

test("a chart with more marks than the limit is not modelled and keeps the source interface", () => {
  const marks = Array.from({ length: MAX_MARKS + 1 }, (_, i) => ({ name: `x${i}, ${i}`, series: "eu" }));
  const m = fromSource(source({ marks }));
  assert.ok("supported" in m && m.supported === false);
  assert.equal(m.fallback, "retain_source_ui");
  assert.match(m.reason, /exceed the limit/);
});

test("a chart at the limit is modelled and its table matches its filtered points", () => {
  const marks = Array.from({ length: MAX_MARKS }, (_, i) => ({ name: `x${i}, ${i}`, series: i % 2 ? "us" : "eu" }));
  const m = fromSource(source({ marks }));
  assert.ok("points" in m);
  const filters: Filter[] = [{ field: "series", op: "eq", value: "eu" }];
  assert.equal(toTable(m, filters).rows.length, MAX_MARKS / 2);
  assert.equal(sameData(m, filters), true);
});
