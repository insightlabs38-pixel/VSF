import { SplitMix64 } from "@vsf/schema";
import { Builder, encodePack, NONE } from "./pack.ts";

const TAGS = ["div", "span", "button", "a", "input", "tr", "td", "li", "h2", "form"] as const;
const ROLES = ["button", "link", "textbox", "row", "cell", "heading"] as const;
const WORDS = ["invoice", "customer", "status", "export", "filter", "total", "owner", "region"] as const;

function pick<T>(rng: SplitMix64, list: readonly T[]): T {
  return list[rng.below(list.length)] as T;
}

/** Mirrors vsf_fixtures::generate_pack call for call; any drift breaks the cross-language digests. */
export function generatePackBytes(seed: number, nodes: number): Uint8Array {
  const rng = new SplitMix64(seed);
  const b = new Builder();
  if (nodes > 0) b.node(NONE, "html", null, null, null, 1);
  for (let i = 1; i < nodes; i++) {
    const parent = rng.below(4) === 0 ? rng.below(i) : i - 1 - rng.below(Math.min(i, 8));
    const tag = pick(rng, TAGS);
    const role = rng.below(3) === 0 ? pick(rng, ROLES) : null;
    const name = rng.below(2) === 0 ? pick(rng, WORDS) : null;
    const text = rng.below(2) === 0 ? `${pick(rng, WORDS)} ${i}` : null;
    const flags = rng.below(16);
    b.node(parent, tag, role, name, text, flags);
  }
  return encodePack(b.snapshot);
}
