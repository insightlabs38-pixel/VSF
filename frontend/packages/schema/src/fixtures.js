import { makeEnvelope } from "./envelope.js";
export const GOLDEN_SEEDS = [1, 42, 20261018];
const ROLES = ["button", "textbox", "row", "link", "checkbox", "heading"];
const WORDS = ["invoice", "customer", "status", "export", "filter", "total", "owner", "region"];
const MASK = (1n << 64n) - 1n;
export class SplitMix64 {
  state;
  constructor(seed) {
    this.state = BigInt(seed) & MASK;
  }
  nextU64() {
    this.state = (this.state + 0x9e3779b97f4a7c15n) & MASK;
    let z = this.state;
    z = ((z ^ (z >> 30n)) * 0xbf58476d1ce4e5b9n) & MASK;
    z = ((z ^ (z >> 27n)) * 0x94d049bb133111ebn) & MASK;
    return z ^ (z >> 31n);
  }
  below(bound) {
    return Number(this.nextU64() % BigInt(bound));
  }
}
function pick(rng, list) {
  return list[rng.below(list.length)];
}
const hex16 = (n) => n.toString(16).padStart(16, "0");
function textFor(rng, index) {
  const base = `${pick(rng, WORDS)} ${pick(rng, WORDS)}`;
  if (index % 4 === 3) {
    const key = ["api", "_key"].join("");
    const a = rng.nextU64();
    const b = rng.nextU64();
    return `${base} ${key}=${hex16(a)}${hex16(b)}`;
  }
  return base;
}
export function generatePayload(seed) {
  const rng = new SplitMix64(seed);
  const count = 4 + rng.below(5);
  const nodes = [];
  for (let i = 0; i < count; i++) {
    const role = pick(rng, ROLES);
    const name = pick(rng, WORDS);
    const text = textFor(rng, i);
    nodes.push(
      new Map([
        ["id", i],
        ["role", role],
        ["name", name],
        ["text", text],
      ]),
    );
  }
  return new Map([
    ["app", "synthetic-orders"],
    ["seed", seed],
    ["nodes", nodes],
  ]);
}
export function generateEnvelopeBytes(seed) {
  return makeEnvelope(seed, generatePayload(seed));
}
