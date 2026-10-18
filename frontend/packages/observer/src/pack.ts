export const MAGIC = [0x56, 0x53, 0x46, 0x53] as const;
export const VERSION = 1;
export const HEADER_LEN = 24;
export const NONE = 0xffffffff;
export const MAX_PACK_BYTES = 64 * 1024 * 1024;
export const MAX_NODES = 2_000_000;
export const MAX_STRINGS = 4_000_000;
export const FLAG_MASK = 0b1111;

export const FLAG_VISIBLE = 1;
export const FLAG_INTERACTIVE = 2;
export const FLAG_EDITABLE = 4;
export const FLAG_REDACTED = 8;

export type PackCode =
  | "pack.empty"
  | "pack.truncated"
  | "pack.bad_magic"
  | "pack.bad_version"
  | "pack.bad_flags"
  | "pack.reserved_nonzero"
  | "pack.too_large"
  | "pack.length_mismatch"
  | "pack.offsets_invalid"
  | "pack.invalid_utf8"
  | "pack.padding_nonzero"
  | "pack.parent_invalid"
  | "pack.string_index_out_of_range"
  | "pack.node_flags_invalid"
  | "pack.not_canonical";

export class PackError extends Error {
  readonly code: PackCode;
  constructor(code: PackCode) {
    super(code);
    this.name = "PackError";
    this.code = code;
  }
}

export interface Node {
  parent: number;
  tag: number;
  role: number;
  name: number;
  text: number;
  flags: number;
}

export interface Snapshot {
  strings: string[];
  nodes: Node[];
}

/** Interns strings in first-use order so each logical snapshot has exactly one encoding. */
export class Builder {
  readonly snapshot: Snapshot = { strings: [], nodes: [] };
  private readonly index = new Map<string, number>();

  private intern(s: string): number {
    const hit = this.index.get(s);
    if (hit !== undefined) return hit;
    const i = this.snapshot.strings.length;
    this.snapshot.strings.push(s);
    this.index.set(s, i);
    return i;
  }

  private optional(s: string | null | undefined): number {
    return s === null || s === undefined ? NONE : this.intern(s);
  }

  node(
    parent: number,
    tag: string,
    role: string | null | undefined,
    name: string | null | undefined,
    text: string | null | undefined,
    flags: number,
  ): number {
    const t = this.intern(tag);
    const r = this.optional(role);
    const n = this.optional(name);
    const x = this.optional(text);
    this.snapshot.nodes.push({ parent, tag: t, role: r, name: n, text: x, flags });
    return this.snapshot.nodes.length - 1;
  }
}

const encoder = new TextEncoder();
const decoderFatal = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true });

export function encodePack(snapshot: Snapshot): Uint8Array {
  const blobs = snapshot.strings.map((s) => encoder.encode(s));
  const blobLen = blobs.reduce((n, b) => n + b.length, 0);
  const pad = (4 - (blobLen % 4)) % 4;
  const total = HEADER_LEN + 4 * (blobs.length + 1) + blobLen + pad + 24 * snapshot.nodes.length;
  const out = new Uint8Array(total);
  const view = new DataView(out.buffer);
  out.set(MAGIC, 0);
  view.setUint16(4, VERSION, true);
  view.setUint16(6, 0, true);
  view.setUint32(8, blobs.length, true);
  view.setUint32(12, blobLen, true);
  view.setUint32(16, snapshot.nodes.length, true);
  view.setUint32(20, 0, true);
  let at = HEADER_LEN;
  let offset = 0;
  view.setUint32(at, 0, true);
  at += 4;
  for (const b of blobs) {
    offset += b.length;
    view.setUint32(at, offset, true);
    at += 4;
  }
  for (const b of blobs) {
    out.set(b, at);
    at += b.length;
  }
  at += pad;
  for (const n of snapshot.nodes) {
    for (const v of [n.parent, n.tag, n.role, n.name, n.text, n.flags]) {
      view.setUint32(at, v, true);
      at += 4;
    }
  }
  return out;
}

export function decodePack(bytes: Uint8Array): Snapshot {
  if (bytes.length === 0) throw new PackError("pack.empty");
  if (bytes.length > MAX_PACK_BYTES) throw new PackError("pack.too_large");
  if (bytes.length < HEADER_LEN) throw new PackError("pack.truncated");
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  for (let i = 0; i < 4; i++) if (bytes[i] !== MAGIC[i]) throw new PackError("pack.bad_magic");
  if (view.getUint16(4, true) !== VERSION) throw new PackError("pack.bad_version");
  if (view.getUint16(6, true) !== 0) throw new PackError("pack.bad_flags");
  const stringCount = view.getUint32(8, true);
  const blobLen = view.getUint32(12, true);
  const nodeCount = view.getUint32(16, true);
  if (view.getUint32(20, true) !== 0) throw new PackError("pack.reserved_nonzero");
  if (stringCount > MAX_STRINGS || nodeCount > MAX_NODES || blobLen > MAX_PACK_BYTES)
    throw new PackError("pack.too_large");
  const offsetsLen = (stringCount + 1) * 4;
  const pad = (4 - (blobLen % 4)) % 4;
  const expected = HEADER_LEN + offsetsLen + blobLen + pad + nodeCount * 24;
  if (bytes.length < expected) throw new PackError("pack.truncated");
  if (bytes.length > expected) throw new PackError("pack.length_mismatch");
  const blobAt = HEADER_LEN + offsetsLen;
  const nodesAt = blobAt + blobLen + pad;
  for (let i = blobAt + blobLen; i < nodesAt; i++) if (bytes[i] !== 0) throw new PackError("pack.padding_nonzero");
  const offset = (i: number): number => view.getUint32(HEADER_LEN + 4 * i, true);
  if (offset(0) !== 0 || offset(stringCount) !== blobLen) throw new PackError("pack.offsets_invalid");
  const strings: string[] = [];
  for (let i = 0; i < stringCount; i++) {
    const a = offset(i);
    const b = offset(i + 1);
    if (a > b || b > blobLen) throw new PackError("pack.offsets_invalid");
    try {
      strings.push(decoderFatal.decode(bytes.subarray(blobAt + a, blobAt + b)));
    } catch {
      throw new PackError("pack.invalid_utf8");
    }
  }
  const nodes: Node[] = [];
  for (let i = 0; i < nodeCount; i++) {
    const at = nodesAt + i * 24;
    const node: Node = {
      parent: view.getUint32(at, true),
      tag: view.getUint32(at + 4, true),
      role: view.getUint32(at + 8, true),
      name: view.getUint32(at + 12, true),
      text: view.getUint32(at + 16, true),
      flags: view.getUint32(at + 20, true),
    };
    if (node.parent !== NONE && node.parent >= i) throw new PackError("pack.parent_invalid");
    if (node.tag === NONE || [node.tag, node.role, node.name, node.text].some((s) => s !== NONE && s >= stringCount)) {
      throw new PackError("pack.string_index_out_of_range");
    }
    if ((node.flags & ~FLAG_MASK) !== 0) throw new PackError("pack.node_flags_invalid");
    nodes.push(node);
  }
  const snapshot = { strings, nodes };
  if (!isCanonical(snapshot)) throw new PackError("pack.not_canonical");
  return snapshot;
}

function isCanonical(s: Snapshot): boolean {
  let next = 0;
  for (const n of s.nodes) {
    for (const idx of [n.tag, n.role, n.name, n.text]) {
      if (idx === NONE) continue;
      if (idx === next) next++;
      else if (idx > next) return false;
    }
  }
  return next === s.strings.length && new Set(s.strings).size === s.strings.length;
}

export const CLASSES = [
  "button",
  "link",
  "textbox",
  "checkbox",
  "heading",
  "row",
  "cell",
  "table",
  "form",
  "list",
  "other",
] as const;

export function classify(tag: string, role: string | undefined): string {
  switch (role ?? tag) {
    case "button":
      return "button";
    case "link":
    case "a":
      return "link";
    case "textbox":
    case "input":
    case "textarea":
      return "textbox";
    case "checkbox":
      return "checkbox";
    case "heading":
    case "h1":
    case "h2":
    case "h3":
    case "h4":
    case "h5":
    case "h6":
      return "heading";
    case "row":
    case "tr":
      return "row";
    case "cell":
    case "columnheader":
    case "td":
    case "th":
      return "cell";
    case "table":
      return "table";
    case "form":
      return "form";
    case "list":
    case "ul":
    case "ol":
      return "list";
    default:
      return "other";
  }
}

export interface PackSummary {
  digest: string;
  node_count: number;
  string_count: number;
  max_depth: number;
  classes: Record<string, number>;
}

/** Same JSON the Rust `analyze` emits (keys sorted by the canonical writer). */
export async function analyzePack(bytes: Uint8Array): Promise<PackSummary> {
  const s = decodePack(bytes);
  const depth = new Array<number>(s.nodes.length).fill(0);
  let maxDepth = 0;
  const counts = new Map<string, number>();
  s.nodes.forEach((n, i) => {
    if (n.parent !== NONE) depth[i] = (depth[n.parent] ?? 0) + 1;
    maxDepth = Math.max(maxDepth, depth[i] ?? 0);
    const cls = classify(s.strings[n.tag] ?? "", n.role === NONE ? undefined : s.strings[n.role]);
    counts.set(cls, (counts.get(cls) ?? 0) + 1);
  });
  const digest = await sha256Hex(bytes);
  return {
    digest,
    node_count: s.nodes.length,
    string_count: s.strings.length,
    max_depth: maxDepth,
    classes: Object.fromEntries([...counts.entries()].sort(([a], [b]) => (a < b ? -1 : 1))),
  };
}

export async function sha256Hex(bytes: Uint8Array): Promise<string> {
  const d = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes as BufferSource));
  return Array.from(d, (b) => b.toString(16).padStart(2, "0")).join("");
}

export function summaryJson(sum: PackSummary): string {
  const classes = Object.entries(sum.classes)
    .map(([k, v]) => `${JSON.stringify(k)}:${v}`)
    .join(",");
  return `{"classes":{${classes}},"digest":"${sum.digest}","max_depth":${sum.max_depth},"node_count":${sum.node_count},"string_count":${sum.string_count}}`;
}
