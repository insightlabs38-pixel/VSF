import { spawn, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { cpus, tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";
import { startServer } from "@vsf/fixture-apps/server";
import { loadFabric } from "@vsf/wasm-host";
import { type Stat, stat } from "./stats.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const bin = join(root, "artifacts/bin/vsf-fabric");
const wasmPath = join(root, "artifacts/wasm/vsf_wasm.wasm");
const out = resolve(process.argv[2] ?? "boundary.json");
const PLAN = [
  { nodes: 1000, fast: 200, slow: 100 },
  { nodes: 10000, fast: 100, slow: 60 },
  { nodes: 50000, fast: 40, slow: 25 },
  { nodes: 200000, fast: 15, slow: 10 },
];

const gen = (nodes: number): Buffer =>
  spawnSync(bin, ["pack-gen", "--seed", "1", "--nodes", String(nodes)], { maxBuffer: 1 << 30 }).stdout;
const rssKb = (stderr: string): number => Number(/rss_kb=(\d+)/.exec(stderr)?.[1] ?? 0);

function frame(op: string, payload: Buffer): Buffer {
  const head = Buffer.alloc(4);
  head.writeUInt32LE(payload.length + 1);
  return Buffer.concat([head, Buffer.from(op), payload]);
}

async function persistentNode(pack: Buffer, n: number) {
  const child = spawn(bin, ["serve"], {
    stdio: ["pipe", "pipe", "pipe"],
    env: { ...process.env, VSF_REPORT_RSS: "1" },
  });
  let err = "";
  child.stderr.on("data", (d: Buffer) => {
    err += d.toString();
  });
  let buf = Buffer.alloc(0);
  let waiter: (() => void) | undefined;
  child.stdout.on("data", (d: Buffer) => {
    buf = Buffer.concat([buf, d]);
    waiter?.();
  });
  const read = async (): Promise<Buffer> => {
    for (;;) {
      if (buf.length >= 5) {
        const len = buf.readUInt32LE(1);
        if (buf.length >= 5 + len) {
          const body = buf.subarray(5, 5 + len);
          buf = buf.subarray(5 + len);
          return body;
        }
      }
      await new Promise<void>((r) => {
        waiter = r;
      });
    }
  };
  const samples: number[] = [];
  for (let i = 0; i < n; i++) {
    const t0 = performance.now();
    child.stdin.write(frame("A", pack));
    await read();
    samples.push(performance.now() - t0);
  }
  child.stdin.end();
  await new Promise((r) => child.on("close", r));
  return { latencyMs: stat(samples), childPeakRssKb: rssKb(err) };
}

const results: Record<string, unknown>[] = [];
const a = await startServer(0);
const browser = await chromium.launch({ channel: "chromium" });
try {
  const fabric = await loadFabric(readFileSync(wasmPath));
  const page = await browser.newPage();
  await page.goto(`${a.url}/wasm-bench/`);
  await page.waitForFunction(() => document.getElementById("status")?.textContent === "ready");
  for (const { nodes, fast, slow } of PLAN) {
    const pack = gen(nodes);
    const row: Record<string, unknown> = { nodes, packBytes: pack.length };

    const rssBefore = process.memoryUsage().rss;
    const wasmSamples: number[] = [];
    for (let i = 0; i < fast; i++) {
      const t0 = performance.now();
      fabric.analyzePack(pack);
      wasmSamples.push(performance.now() - t0);
    }
    row.nodeInProcessWasm = {
      latencyMs: stat(wasmSamples),
      processRssDeltaKb: Math.round((process.memoryUsage().rss - rssBefore) / 1024),
    };

    const spawnSamples: number[] = [];
    let spawnRss = 0;
    for (let i = 0; i < slow; i++) {
      const t0 = performance.now();
      const r = spawnSync(bin, ["pack-analyze", "-"], {
        input: pack,
        env: { ...process.env, VSF_REPORT_RSS: "1" },
        maxBuffer: 1 << 28,
      });
      spawnSamples.push(performance.now() - t0);
      spawnRss = Math.max(spawnRss, rssKb(r.stderr.toString()));
    }
    row.nodeSpawnPerCall = { latencyMs: stat(spawnSamples), childPeakRssKb: spawnRss };
    row.nodePersistentWorker = await persistentNode(pack, fast);

    const benchPath = join(mkdtempSync(join(tmpdir(), "vsf-bench-")), "pack.bin");
    writeFileSync(benchPath, pack);
    const goRun = spawnSync(
      join(root, "artifacts/bin/vsfbench"),
      ["-bin", bin, "-pack", benchPath, "-iterations", String(slow)],
      { maxBuffer: 1 << 26 },
    );
    row.go = JSON.parse(goRun.stdout.toString());

    row.nativeCompute = JSON.parse(
      spawnSync(bin, [
        "pack-bench",
        "--seed",
        "1",
        "--nodes",
        String(nodes),
        "--iterations",
        String(fast),
      ]).stdout.toString(),
    );

    const b = await page.evaluate(([b64, n]) => window.__analyze?.(b64 as string, n as number), [
      pack.toString("base64"),
      fast,
    ] as const);
    row.browserWasm = { latencyMs: stat(b?.ms ?? []), error: b?.error };
    results.push(row);
  }

  const failures: Record<string, unknown> = {};
  const good = gen(1000);
  const truncated = good.subarray(0, good.length - 5);
  const code = (f: () => unknown): string => {
    try {
      f();
      return "no error";
    } catch (e) {
      return (e as { code?: string }).code ?? String(e);
    }
  };
  failures.truncatedInput = {
    nodeWasm: code(() => fabric.analyzePack(truncated)),
    nativeCli: spawnSync(bin, ["pack-analyze", "-"], { input: truncated }).stderr.toString().trim(),
    browserWasm: (await page.evaluate((b64) => window.__analyze?.(b64, 1), truncated.toString("base64")))?.error,
  };
  const big = Buffer.alloc(70 * 1024 * 1024);
  failures.oversizedInput70MiB = {
    nodeWasm: code(() => fabric.analyzePack(big)),
    nativeCli: spawnSync(bin, ["pack-analyze", "-"], { input: big, maxBuffer: 1 << 20 })
      .stderr.toString()
      .trim(),
  };
  const mem = (fabric as unknown as { memoryBytes?: number }).memoryBytes;
  failures.wasmMemoryNote =
    mem ?? "linear memory is never returned to the OS after growth; observed via process RSS delta above";
  const child = spawn(bin, ["serve"], { stdio: ["pipe", "pipe", "ignore"] });
  child.kill("SIGKILL");
  const closed = await new Promise<string>((r) => {
    child.on("close", (c, s) => r(`exit=${c} signal=${s}`));
  });
  failures.persistentWorkerKilled = { observedByNode: closed, goRestartMeasured: "see go[].restart_after_kill_ms" };
  mkdirSync(dirname(out), { recursive: true });
  writeFileSync(
    out,
    `${JSON.stringify(
      {
        schema: "vsf-bench-boundary-v1",
        environment: {
          cpus: cpus().length,
          cpuModel: cpus()[0]?.model,
          node: process.version,
          chromium: browser.version(),
        },
        operation: "pack-analyze (decode + validate + sha256 + class histogram)",
        note: "latency is per call; childPeakRssKb is the worker's own peak RSS; p99 equals max for n<100",
        sizes: results,
        failureModes: failures,
      },
      null,
      2,
    )}\n`,
  );
  console.log(`wrote ${out}`);
} finally {
  await browser.close();
  a.server.close();
}

export type { Stat };
