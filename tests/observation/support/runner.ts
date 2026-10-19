import type { Page } from "@playwright/test";
import type { CanonNode } from "@vsf/observer";
import { type Harness, truthOf } from "./world.ts";

export interface Step {
  name: string;
  run(): Promise<void>;
}

export interface StepResult {
  name: string;
  epoch: number;
  seq: number;
  state: string;
  url: string | null;
  nodes: number;
  equalsPage: boolean;
}

const stripIds = (n: CanonNode[]): string => JSON.stringify(n);

/** Runs steps in order; after each one the replica must settle and equal an independent capture of the page. */
export async function runScenario(page: Page, h: Harness, steps: Step[]): Promise<StepResult[]> {
  const out: StepResult[] = [];
  for (const step of steps) {
    await step.run();
    await page.waitForLoadState("load");
    await h.settle();
    const r = h.collector.replica;
    const truth = stripIds(await truthOf(page));
    if (process.env.VSF_DEBUG_RUNNER && stripIds(r.canonical()) !== truth) {
      const a = stripIds(r.canonical());
      let i = 0;
      while (i < a.length && i < truth.length && a[i] === truth[i]) i++;
      console.log(
        step.name,
        "\nREPLICA:",
        a.slice(Math.max(0, i - 150), i + 250),
        "\nTRUTH:",
        truth.slice(Math.max(0, i - 150), i + 250),
      );
    }
    out.push({
      name: step.name,
      epoch: r.epoch,
      seq: r.seq,
      state: r.state,
      url: r.url,
      nodes: r.size,
      equalsPage: stripIds(r.canonical()) === stripIds(await truthOf(page)),
    });
  }
  return out;
}

/** Epochs depend on the clock, so scenarios are compared by how epochs relate, not by their values. */
export function normalise(results: StepResult[]): unknown[] {
  const first = results[0]?.epoch ?? 0;
  const ranks = [...new Set(results.map((r) => r.epoch))].sort((a, b) => a - b);
  return results.map((r) => ({
    name: r.name,
    epochRank: ranks.indexOf(r.epoch),
    sameAsFirst: r.epoch === first,
    state: r.state,
    url: r.url?.replace(/^https?:\/\/[^/]+/, "") ?? null,
    nodes: r.nodes,
    equalsPage: r.equalsPage,
  }));
}
