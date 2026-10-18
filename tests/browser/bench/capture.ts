import { mkdirSync, writeFileSync } from "node:fs";
import { cpus, totalmem } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { startServer } from "@vsf/fixture-apps/server";
import { createEnv } from "../support/env.ts";
import { stat } from "./stats.ts";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const out = resolve(repoRoot, process.argv[2] ?? "capture.json");
const PLAN: { nodes: number; iterations: number }[] = [
  { nodes: 1000, iterations: 40 },
  { nodes: 10000, iterations: 20 },
  { nodes: 50000, iterations: 10 },
];

interface Reply {
  ok: boolean;
  packBytes: number;
  summary: { node_count: number };
  timings: { captureMs: number; packMs: number; injectRoundTripMs: number; decodeMs: number; recognizeMs: number };
}

const a = await startServer(0);
const b = await startServer(0);
const { env, dispose } = await createEnv({ a: Number(new URL(a.url).port), b: Number(new URL(b.url).port) });
const result: Record<string, unknown>[] = [];
try {
  await env.send({ type: "grant", origin: env.a });
  for (const { nodes, iterations } of PLAN) {
    const page = await env.context.newPage();
    await page.goto(`${env.a}/large/?n=${nodes}&seed=1`);
    await page.waitForSelector("html[data-built]");
    const domNodes = await page.evaluate(() => document.querySelectorAll("*").length);
    const tabId = await env.tabIdOf(page);
    const rows: Record<string, number[]> = {
      captureMs: [],
      packMs: [],
      transferMs: [],
      decodeMs: [],
      recognizeMs: [],
      endToEndMs: [],
      overheadMs: [],
    };
    let sample: Reply | undefined;
    for (let i = 0; i < iterations + 2; i++) {
      const t0 = Date.now();
      const r = (await env.send({ type: "capture", tabId })) as unknown as Reply;
      const wall = Date.now() - t0;
      if (!r.ok) throw new Error(`capture failed: ${JSON.stringify(r)}`);
      sample = r;
      if (i < 2) continue;
      const t = r.timings;
      rows.captureMs?.push(t.captureMs);
      rows.packMs?.push(t.packMs);
      rows.transferMs?.push(t.injectRoundTripMs - t.captureMs - t.packMs);
      rows.decodeMs?.push(t.decodeMs);
      rows.recognizeMs?.push(t.recognizeMs);
      rows.endToEndMs?.push(wall);
      rows.overheadMs?.push(
        wall - (t.captureMs + t.packMs + (t.injectRoundTripMs - t.captureMs - t.packMs) + t.decodeMs + t.recognizeMs),
      );
    }
    result.push({
      requestedNodes: nodes,
      domElements: domNodes,
      snapshotNodes: sample?.summary.node_count,
      packBytes: sample?.packBytes,
      base64Bytes: Math.ceil(((sample?.packBytes ?? 0) * 4) / 3),
      phases: Object.fromEntries(Object.entries(rows).map(([k, v]) => [k, stat(v)])),
    });
    await page.close();
  }
  const transport: Record<string, unknown>[] = [];
  const page = await env.context.newPage();
  await page.goto(`${env.a}/forms/`);
  const tabId = await env.tabIdOf(page);
  const worker = await env.worker();
  for (const bytes of [100_000, 1_000_000, 4_000_000]) {
    for (const variant of ["base64", "number-array"] as const) {
      const samples: number[] = [];
      for (let i = 0; i < 12; i++) {
        const ms = await worker.evaluate(
          async ({ id, size, v }) => {
            const t0 = performance.now();
            const [r] = await chrome.scripting.executeScript({
              target: { tabId: id },
              args: [size, v],
              func: (n: number, kind: string) => {
                const data = new Uint8Array(n);
                for (let k = 0; k < n; k++) data[k] = k & 255;
                if (kind === "base64") {
                  let s = "";
                  for (let k = 0; k < n; k += 0x8000) s += String.fromCharCode(...data.subarray(k, k + 0x8000));
                  return btoa(s);
                }
                return Array.from(data);
              },
            });
            const len = Array.isArray(r?.result) ? r.result.length : String(r?.result).length;
            return len > 0 ? performance.now() - t0 : -1;
          },
          { id: tabId, size: bytes, v: variant },
        );
        if (i >= 2) samples.push(ms);
      }
      transport.push({ payloadBytes: bytes, variant, roundTripMs: stat(samples) });
    }
  }
  mkdirSync(dirname(out), { recursive: true });
  writeFileSync(
    out,
    `${JSON.stringify(
      {
        schema: "vsf-bench-capture-v1",
        environment: {
          cpus: cpus().length,
          cpuModel: cpus()[0]?.model,
          memGiB: Math.round(totalmem() / 2 ** 30),
          node: process.version,
          chromium: env.context.browser()?.version() ?? "141 (persistent context)",
        },
        note: "phases: captureMs=DOM walk, packMs=encode+base64, transferMs=executeScript round trip minus the two, decodeMs=base64->bytes in worker, recognizeMs=Rust WASM analyze; p99 equals max when n<100",
        sizes: result,
        transportVariants: transport,
      },
      null,
      2,
    )}\n`,
  );
  console.log(`wrote ${out}`);
} finally {
  await dispose();
  a.server.close();
  b.server.close();
}
