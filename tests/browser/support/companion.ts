import { type ChildProcessWithoutNullStreams, spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../../", import.meta.url));

export interface Companion {
  url: string;
  state: string;
  events: Record<string, unknown>[];
  /** What the operator types at the companion. */
  say(line: string): void;
  next(event: string, timeoutMs?: number): Promise<Record<string, unknown>>;
  stop(): Promise<void>;
}

/** Starts the real companion in pairing mode; the test plays the operator on its standard input. */
export async function startCompanion(origin: string, extra: string[] = []): Promise<Companion> {
  const state = mkdtempSync(join(tmpdir(), "vsf-pair-"));
  const proc: ChildProcessWithoutNullStreams = spawn(
    join(root, "artifacts/bin/vsfbridge"),
    ["-origins", origin, "-state", state, ...extra],
    { stdio: ["pipe", "pipe", "pipe"] },
  );
  const events: Record<string, unknown>[] = [];
  const waiters: (() => void)[] = [];
  let buffered = "";
  proc.stdout.on("data", (d: Buffer) => {
    buffered += d.toString();
    const lines = buffered.split("\n");
    buffered = lines.pop() ?? "";
    for (const l of lines) if (l.startsWith("{")) events.push(JSON.parse(l));
    for (const w of waiters.splice(0)) w();
  });
  proc.stderr.on("data", () => undefined);
  const deadline = Date.now() + 10_000;
  while (events.length === 0 && Date.now() < deadline) await new Promise((r) => setTimeout(r, 25));
  const first = events[0] as { addr: string; mode: string };
  const taken = new Set<number>([0]);
  const c: Companion = {
    url: `http://${first.addr}`,
    state,
    events,
    say: (line) => void proc.stdin.write(`${line}\n`),
    async next(event, timeoutMs = 10_000) {
      const end = Date.now() + timeoutMs;
      for (;;) {
        const i = events.findIndex((e, n) => e.event === event && !taken.has(n));
        if (i >= 0) {
          taken.add(i);
          return events[i] as Record<string, unknown>;
        }
        if (Date.now() > end) throw new Error(`no ${event} event: ${JSON.stringify(events)}`);
        await new Promise<void>((r) => {
          waiters.push(r);
          setTimeout(r, 50);
        });
      }
    },
    async stop() {
      proc.kill();
      await new Promise((r) => proc.once("exit", r));
      rmSync(state, { recursive: true, force: true });
    },
  };
  return c;
}
