/** A decimal kept as the text the source showed, so nothing is rounded on the way to a table. */
export interface Decimal {
  negative: boolean;
  /** Digits without the point, leading zeros removed (at least "0"). */
  digits: string;
  /** Number of digits after the point. */
  scale: number;
  /** The exact text the source gave. */
  text: string;
}

const PLAIN = /^([+-])?(\d+)(?:\.(\d+))?$/;

/** Plain decimals only. Exponents, grouping separators and locale forms are not guessed: they stay labels. */
export function parseDecimal(text: string): Decimal | null {
  const m = PLAIN.exec(text.trim());
  if (!m) return null;
  const whole = (m[2] as string).replace(/^0+(?=\d)/, "");
  const frac = m[3] ?? "";
  const digits = `${whole}${frac}`.replace(/^0+(?=\d)/, "");
  return { negative: m[1] === "-" && !/^0*$/.test(`${m[2]}${frac}`), digits, scale: frac.length, text: text.trim() };
}

/** Exact comparison without converting to floating point. */
export function compareDecimal(a: Decimal, b: Decimal): -1 | 0 | 1 {
  if (a.negative !== b.negative) return a.negative ? -1 : 1;
  const scale = Math.max(a.scale, b.scale);
  const x = BigInt(a.digits + "0".repeat(scale - a.scale));
  const y = BigInt(b.digits + "0".repeat(scale - b.scale));
  const c = x === y ? 0 : x < y ? -1 : 1;
  return (a.negative ? -c : c) as -1 | 0 | 1;
}

export interface Series {
  id: string;
  name: string;
  /** Hidden by the source's own legend or toggle; hidden series stay in the table with that state. */
  hidden: boolean;
}

export interface Point {
  /** Stable identity from the source (a data id, else series and category), never a position. */
  id: string;
  series: string;
  x: string;
  y: string;
  /** A unit symbol the source printed after the value ("%", "$"); kept apart so the value stays a plain decimal. */
  unit: string | null;
}

export type FilterOp = "eq" | "ne" | "gte" | "lte" | "in";

export interface Filter {
  field: "x" | "y" | "series";
  op: FilterOp;
  value: string | string[];
}

export type Origin =
  | { kind: "labelled_marks"; supported: true }
  | { kind: "data_table"; supported: true }
  | { kind: "canvas"; supported: false; reason: string }
  | { kind: "image"; supported: false; reason: string };

export interface ChartModel {
  schema: "vsf-chart-v1";
  source: string;
  kind: "bar" | "line" | "area" | "pie" | "scatter";
  title: string;
  xLabel: string | null;
  yLabel: string | null;
  series: Series[];
  points: Point[];
  origin: Origin;
}

export interface Unsupported {
  supported: false;
  reason: string;
  /** What to do instead: the source's own chart stays on screen. */
  fallback: "retain_source_ui";
}

/** One predicate engine for the chart and its table, so they cannot disagree about what a filter keeps. */
export function matches(p: Point, f: Filter): boolean {
  const have = p[f.field];
  const want = f.value;
  switch (f.op) {
    case "eq":
      return typeof want === "string" && have === want;
    case "ne":
      return typeof want === "string" && have !== want;
    case "in":
      return Array.isArray(want) && want.includes(have);
    case "gte":
    case "lte": {
      const a = parseDecimal(have);
      const b = typeof want === "string" ? parseDecimal(want) : null;
      // A value that is not a plain decimal is never kept by a numeric filter: guessing would change the data.
      if (!a || !b) return false;
      const c = compareDecimal(a, b);
      return f.op === "gte" ? c >= 0 : c <= 0;
    }
  }
}

export function applyFilters(points: readonly Point[], filters: readonly Filter[]): Point[] {
  return points.filter((p) => filters.every((f) => matches(p, f)));
}

export interface Table {
  caption: string;
  columns: string[];
  rows: { id: string; cells: string[]; hidden: boolean }[];
  /** Points the filters removed, so a table that shows fewer rows says how many and why. */
  omitted: number;
  filters: Filter[];
  note: string;
}

/** The accessible tabular form of a chart: the same points, exact values, hidden series kept and marked. */
export function toTable(model: ChartModel, filters: readonly Filter[] = []): Table {
  const kept = applyFilters(model.points, filters);
  const hidden = new Set(model.series.filter((s) => s.hidden).map((s) => s.id));
  const name = new Map(model.series.map((s) => [s.id, s.name]));
  const xs = model.xLabel ?? "Category";
  const ys = model.yLabel ?? "Value";
  const units = model.points.some((p) => p.unit !== null);
  return {
    caption: model.title,
    columns: units ? ["Series", xs, ys, "Unit"] : ["Series", xs, ys],
    rows: kept.map((p) => ({
      id: p.id,
      cells: units
        ? [name.get(p.series) ?? p.series, p.x, p.y, p.unit ?? ""]
        : [name.get(p.series) ?? p.series, p.x, p.y],
      hidden: hidden.has(p.series),
    })),
    omitted: model.points.length - kept.length,
    filters: [...filters],
    note: "Values are shown exactly as the source gave them.",
  };
}

export interface Mark {
  /** Accessible name the source gave the mark, e.g. "Q1, 12.50". */
  name: string;
  series: string;
  id?: string;
}

export interface SourceChart {
  source: string;
  kind: ChartModel["kind"];
  title: string;
  xLabel: string | null;
  yLabel: string | null;
  series: Series[];
  marks: Mark[];
  /** What the chart is drawn on. */
  surface: "svg" | "html" | "canvas" | "image";
}

/** Reads "label, value" or "label: value" off a mark's name. Anything else is refused rather than interpreted. */
export function readMark(name: string): { x: string; y: string; unit: string | null } | null {
  const m = /^(.*?)\s*[,:]\s*([+-]?\d+(?:\.\d+)?)\s*([%$€£])?$/.exec(name.trim());
  if (!m?.[1]) return null;
  return { x: m[1], y: m[2] as string, unit: m[3] ?? null };
}

/** Beyond this many marks the model would cost more memory than the source chart is worth, so the source interface stays. */
export const MAX_MARKS = 100_000;

export function fromSource(c: SourceChart): ChartModel | Unsupported {
  if (c.marks.length > MAX_MARKS)
    return {
      supported: false,
      reason: `${c.marks.length} marks exceed the limit of ${MAX_MARKS}`,
      fallback: "retain_source_ui",
    };
  if (c.surface === "canvas")
    return {
      supported: false,
      reason: "canvas content is not in the page structure; values cannot be recovered",
      fallback: "retain_source_ui",
    };
  if (c.surface === "image")
    return { supported: false, reason: "an image has no recoverable data", fallback: "retain_source_ui" };
  const points: Point[] = [];
  const seen = new Set<string>();
  for (const mark of c.marks) {
    const v = readMark(mark.name);
    if (!v)
      return {
        supported: false,
        reason: `mark "${mark.name.slice(0, 40)}" has no label and value`,
        fallback: "retain_source_ui",
      };
    const id = mark.id ?? `${mark.series}/${v.x}`;
    if (seen.has(id))
      return { supported: false, reason: `two marks share the identity ${id}`, fallback: "retain_source_ui" };
    seen.add(id);
    points.push({ id, series: mark.series, x: v.x, y: v.y, unit: v.unit });
  }
  if (points.length === 0) return { supported: false, reason: "no labelled marks", fallback: "retain_source_ui" };
  const known = new Set(c.series.map((s) => s.id));
  if (points.some((p) => !known.has(p.series)))
    return { supported: false, reason: "a mark belongs to no declared series", fallback: "retain_source_ui" };
  return {
    schema: "vsf-chart-v1",
    source: c.source,
    kind: c.kind,
    title: c.title,
    xLabel: c.xLabel,
    yLabel: c.yLabel,
    series: c.series,
    points,
    origin: { kind: "labelled_marks", supported: true },
  };
}

/** True when the table and the chart show the same points for the same filters. */
export function sameData(model: ChartModel, filters: readonly Filter[]): boolean {
  const chart = applyFilters(model.points, filters).map((p) => p.id);
  const table = toTable(model, filters).rows.map((r) => r.id);
  return chart.length === table.length && chart.every((id, i) => id === table[i]);
}
