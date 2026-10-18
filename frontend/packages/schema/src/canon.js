export const MAX_INPUT_BYTES = 16 * 1024 * 1024;
export const MAX_DEPTH = 64;
export const MAX_SAFE_INT = 9007199254740991;
export class CanonError extends Error {
  code;
  constructor(code) {
    super(code);
    this.name = "CanonError";
    this.code = code;
  }
}
const decoder = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true });
const encoder = new TextEncoder();
export function parse(input) {
  if (input.length > MAX_INPUT_BYTES) throw new CanonError("canon.too_large");
  let text;
  try {
    text = decoder.decode(input);
  } catch {
    throw new CanonError("canon.invalid_utf8");
  }
  const parser = new Parser(text);
  parser.skipWs();
  const value = parser.value(0);
  parser.skipWs();
  if (!parser.atEnd()) throw new CanonError("canon.trailing_data");
  return value;
}
export function canonicalize(input) {
  return toCanonical(parse(input));
}
export function toCanonical(value) {
  return encoder.encode(write(value));
}
export async function sha256Hex(bytes) {
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  let hex = "";
  for (const b of digest) hex += b.toString(16).padStart(2, "0");
  return hex;
}
function write(value) {
  if (value === null) return "null";
  switch (typeof value) {
    case "boolean":
      return value ? "true" : "false";
    case "number":
      return String(value);
    case "string":
      return JSON.stringify(value);
  }
  if (Array.isArray(value)) return `[${value.map(write).join(",")}]`;
  const entries = [...value.entries()];
  entries.sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0));
  return `{${entries.map(([k, v]) => `${JSON.stringify(k)}:${write(v)}`).join(",")}}`;
}
class Parser {
  pos = 0;
  src;
  constructor(src) {
    this.src = src;
  }
  atEnd() {
    return this.pos === this.src.length;
  }
  skipWs() {
    while (this.pos < this.src.length) {
      const c = this.src.charCodeAt(this.pos);
      if (c !== 0x20 && c !== 0x09 && c !== 0x0a && c !== 0x0d) break;
      this.pos++;
    }
  }
  expect(ch) {
    if (this.src[this.pos] !== ch) throw new CanonError("canon.syntax");
    this.pos++;
  }
  literal(word, value) {
    if (!this.src.startsWith(word, this.pos)) throw new CanonError("canon.syntax");
    this.pos += word.length;
    return value;
  }
  value(depth) {
    if (depth > MAX_DEPTH) throw new CanonError("canon.depth_exceeded");
    const c = this.src[this.pos];
    if (c === "n") return this.literal("null", null);
    if (c === "t") return this.literal("true", true);
    if (c === "f") return this.literal("false", false);
    if (c === '"') return this.string();
    if (c === "[") return this.array(depth);
    if (c === "{") return this.object(depth);
    if (c === "-" || (c !== undefined && c >= "0" && c <= "9")) return this.number();
    throw new CanonError("canon.syntax");
  }
  array(depth) {
    this.expect("[");
    const items = [];
    this.skipWs();
    if (this.src[this.pos] === "]") {
      this.pos++;
      return items;
    }
    for (;;) {
      this.skipWs();
      items.push(this.value(depth + 1));
      this.skipWs();
      const c = this.src[this.pos];
      if (c === ",") this.pos++;
      else if (c === "]") {
        this.pos++;
        return items;
      } else throw new CanonError("canon.syntax");
    }
  }
  object(depth) {
    this.expect("{");
    const entries = new Map();
    this.skipWs();
    if (this.src[this.pos] === "}") {
      this.pos++;
      return entries;
    }
    for (;;) {
      this.skipWs();
      if (this.src[this.pos] !== '"') throw new CanonError("canon.syntax");
      const key = this.string();
      this.skipWs();
      this.expect(":");
      this.skipWs();
      const val = this.value(depth + 1);
      if (entries.has(key)) throw new CanonError("canon.duplicate_key");
      entries.set(key, val);
      this.skipWs();
      const c = this.src[this.pos];
      if (c === ",") this.pos++;
      else if (c === "}") {
        this.pos++;
        return entries;
      } else throw new CanonError("canon.syntax");
    }
  }
  number() {
    const negative = this.src[this.pos] === "-";
    if (negative) this.pos++;
    const start = this.pos;
    const first = this.src[this.pos];
    if (first === "0") this.pos++;
    else if (first !== undefined && first >= "1" && first <= "9") {
      while (this.isDigit(this.src[this.pos])) this.pos++;
    } else throw new CanonError("canon.syntax");
    const next = this.src[this.pos];
    if (next === "." || next === "e" || next === "E") throw new CanonError("canon.non_integer");
    if (this.isDigit(next)) throw new CanonError("canon.syntax");
    const digits = this.src.slice(start, this.pos);
    if (digits.length > 16) throw new CanonError("canon.integer_out_of_range");
    const magnitude = Number(digits);
    if (magnitude > MAX_SAFE_INT) throw new CanonError("canon.integer_out_of_range");
    return negative && magnitude !== 0 ? -magnitude : magnitude;
  }
  isDigit(c) {
    return c !== undefined && c >= "0" && c <= "9";
  }
  hex4() {
    const slice = this.src.slice(this.pos, this.pos + 4);
    if (!/^[0-9a-fA-F]{4}$/.test(slice)) throw new CanonError("canon.invalid_escape");
    this.pos += 4;
    return Number.parseInt(slice, 16);
  }
  string() {
    this.expect('"');
    let out = "";
    for (;;) {
      const runStart = this.pos;
      while (this.pos < this.src.length) {
        const c = this.src.charCodeAt(this.pos);
        if (c === 0x22 || c === 0x5c || c < 0x20) break;
        this.pos++;
      }
      out += this.src.slice(runStart, this.pos);
      const c = this.src[this.pos];
      if (c === '"') {
        this.pos++;
        return out;
      }
      if (c !== "\\") throw new CanonError("canon.syntax");
      this.pos++;
      const esc = this.src[this.pos];
      if (esc === undefined) throw new CanonError("canon.invalid_escape");
      this.pos++;
      switch (esc) {
        case '"':
          out += '"';
          break;
        case "\\":
          out += "\\";
          break;
        case "/":
          out += "/";
          break;
        case "b":
          out += "\b";
          break;
        case "f":
          out += "\f";
          break;
        case "n":
          out += "\n";
          break;
        case "r":
          out += "\r";
          break;
        case "t":
          out += "\t";
          break;
        case "u": {
          const hi = this.hex4();
          if (hi >= 0xd800 && hi < 0xdc00) {
            if (!this.src.startsWith("\\u", this.pos)) throw new CanonError("canon.lone_surrogate");
            this.pos += 2;
            const lo = this.hex4();
            if (lo < 0xdc00 || lo >= 0xe000) throw new CanonError("canon.lone_surrogate");
            out += String.fromCharCode(hi, lo);
          } else if (hi >= 0xdc00 && hi < 0xe000) {
            throw new CanonError("canon.lone_surrogate");
          } else {
            out += String.fromCharCode(hi);
          }
          break;
        }
        default:
          throw new CanonError("canon.invalid_escape");
      }
    }
  }
}
