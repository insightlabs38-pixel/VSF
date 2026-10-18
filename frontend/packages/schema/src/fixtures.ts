import type { Value } from "./canon.ts";
import { makeEnvelope } from "./envelope.ts";

export const GOLDEN_SEEDS: readonly number[] = [1, 42, 20261018];

const ROLES = ["button", "textbox", "row", "link", "checkbox", "heading"] as const;
const WORDS = ["invoice", "customer", "status", "export", "filter", "total", "owner", "region"] as const;
const MASK = (1n << 64n) - 1n;

export class SplitMix64 {
  private state: bigint;
  constructor(seed: bigint | number) {
    this.state = BigInt(seed) & MASK;
  }

  nextU64(): bigint {
    this.state = (this.state + 0x9e3779b97f4a7c15n) & MASK;
    let z = this.state;
    z = ((z ^ (z >> 30n)) * 0xbf58476d1ce4e5b9n) & MASK;
    z = ((z ^ (z >> 27n)) * 0x94d049bb133111ebn) & MASK;
    return z ^ (z >> 31n);
  }

  below(bound: number): number {
    return Number(this.nextU64() % BigInt(bound));
  }
}

function pick<T>(rng: SplitMix64, list: readonly T[]): T {
  return list[rng.below(list.length)] as T;
}

const hex16 = (n: bigint): string => n.toString(16).padStart(16, "0");

function textFor(rng: SplitMix64, index: number): string {
  const base = `${pick(rng, WORDS)} ${pick(rng, WORDS)}`;
  if (index % 4 === 3) {
    const key = ["api", "_key"].join("");
    const a = rng.nextU64();
    const b = rng.nextU64();
    return `${base} ${key}=${hex16(a)}${hex16(b)}`;
  }
  return base;
}

export function generatePayload(seed: number): Value {
  const rng = new SplitMix64(seed);
  const count = 4 + rng.below(5);
  const nodes: Value[] = [];
  for (let i = 0; i < count; i++) {
    const role = pick(rng, ROLES);
    const name = pick(rng, WORDS);
    const text = textFor(rng, i);
    nodes.push(
      new Map<string, Value>([
        ["id", i],
        ["role", role],
        ["name", name],
        ["text", text],
      ]),
    );
  }
  return new Map<string, Value>([
    ["app", "synthetic-orders"],
    ["seed", seed],
    ["nodes", nodes],
  ]);
}

export function generateEnvelopeBytes(seed: number): Uint8Array {
  return makeEnvelope(seed, generatePayload(seed));
}
