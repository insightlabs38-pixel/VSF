import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { redact } from "@vsf/redact";

export interface LoggedRun {
  name: string;
  command: string[];
  exitCode: number;
  logPath: string;
  sha256: string;
  bytes: number;
}

const sha = (text: string): string => createHash("sha256").update(text).digest("hex");

/** Runs a command, stores a redacted log plus checksum, and returns the exit code unchanged. */
export function runLogged(name: string, command: string[], logDir: string, cwd: string): LoggedRun {
  const [cmd, ...args] = command;
  if (!cmd) throw new Error("empty command");
  const r = spawnSync(cmd, args, { cwd, encoding: "utf8", maxBuffer: 256 * 1024 * 1024 });
  const raw = `${r.stdout ?? ""}${r.stderr ?? ""}${r.error ? `spawn error: ${r.error.message}\n` : ""}`;
  const exitCode = r.status ?? (r.error ? 127 : 1);
  const body = redact(`$ ${command.join(" ")}\n${raw}[exit ${exitCode}]\n`);
  mkdirSync(logDir, { recursive: true });
  const logPath = join(logDir, `${name}.log`);
  writeFileSync(logPath, body);
  return { name, command, exitCode, logPath, sha256: sha(body), bytes: Buffer.byteLength(body) };
}

export function summarize(runs: readonly LoggedRun[]): string {
  const rows = runs.map(
    (r) => `| ${r.name} | ${r.exitCode === 0 ? "PASS" : "FAIL"} | ${r.exitCode} | ${r.sha256.slice(0, 12)} |`,
  );
  const failed = runs.filter((r) => r.exitCode !== 0);
  const head = failed.length ? `## FAILED: ${failed.map((r) => r.name).join(", ")}\n` : "## All steps passed\n";
  return `${head}\n| step | result | exit | log sha256 |\n|---|---|---|---|\n${rows.join("\n")}\n`;
}
