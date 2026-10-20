import { type ChildProcess, spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../../", import.meta.url));

export interface LogLine {
  method: string;
  path: string;
  status: number;
  origin: string;
  host: string;
}

export interface Companion {
  addr: string;
  endpoint: string;
  port: number;
  code: string;
  token: string | undefined;
  log: LogLine[];
  proc: ChildProcess;
  stderr: string[];
  stop(): Promise<void>;
}

/** The real companion binary with pairing on; its request log is the independent witness for what reached it. */
export async function startCompanion(
  extOrigin: string,
  opts: { devToken?: boolean; port?: number } = {},
): Promise<Companion> {
  const state = mkdtempSync(join(tmpdir(), "vsf-companion-"));
  const args = ["-origins", extOrigin, "-state", state, "-pair", "-port", String(opts.port ?? 0)];
  if (opts.devToken) args.push("-dev-token");
  const proc = spawn(join(root, "artifacts/bin/vsfbridge"), args, { stdio: ["ignore", "pipe", "pipe"] });
  const log: LogLine[] = [];
  const stderr: string[] = [];
  let pending = "";
  proc.stderr?.on("data", (d: Buffer) => {
    pending += d.toString();
    const lines = pending.split("\n");
    pending = lines.pop() ?? "";
    for (const l of lines) {
      stderr.push(l);
      if (l.startsWith("{") && l.includes('"method"')) log.push(JSON.parse(l));
    }
  });
  const first = await new Promise<string>((resolve) => proc.stdout?.once("data", (d: Buffer) => resolve(d.toString())));
  const out = JSON.parse(first) as { addr: string; pair_code: string; token?: string };
  return {
    addr: out.addr,
    endpoint: out.addr,
    port: Number(out.addr.split(":")[1]),
    code: out.pair_code,
    token: out.token,
    log,
    proc,
    stderr,
    async stop() {
      const done = new Promise<void>((r) => proc.once("exit", () => r()));
      proc.kill();
      await done;
      rmSync(state, { recursive: true, force: true });
    },
  };
}
