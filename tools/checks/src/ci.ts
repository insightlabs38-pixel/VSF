import { createHash } from "node:crypto";
import { appendFileSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { type LoggedRun, runLogged, summarize } from "./runlogged.ts";

export interface Step {
  name: string;
  cmd: string[];
}

export interface JobFile {
  jobs: Record<string, Step[]>;
}

export interface JobManifest {
  schema: "vsf-ci-manifest-v1";
  job: string;
  status: "PASS" | "FAIL";
  induced_failure: boolean;
  steps: { name: string; command: string[]; exit: number; log: string; sha256: string; bytes: number }[];
}

export const CANARY: Step = {
  name: "induced-failure-canary",
  cmd: ["node", "-e", "console.error('deliberate CI failure canary'); process.exit(1)"],
};

export function loadJobs(path: string): JobFile {
  return JSON.parse(readFileSync(path, "utf8")) as JobFile;
}

/** Runs every step even after a failure so one report lists all breakage; the job passes only if all steps pass. */
export function runJob(root: string, jobs: JobFile, job: string, outDir: string, induce: boolean): JobManifest {
  const steps = jobs.jobs[job];
  if (!steps) throw new Error(`unknown job ${job}`);
  if (steps.length === 0) throw new Error(`job ${job} has no steps, which would pass vacuously`);
  const all = induce ? [...steps, CANARY] : steps;
  const logDir = join(outDir, "logs");
  const runs: LoggedRun[] = all.map((s) => runLogged(s.name, s.cmd, logDir, root));
  const manifest: JobManifest = {
    schema: "vsf-ci-manifest-v1",
    job,
    status: runs.every((r) => r.exitCode === 0) ? "PASS" : "FAIL",
    induced_failure: induce,
    steps: runs.map((r) => ({
      name: r.name,
      command: r.command,
      exit: r.exitCode,
      log: `logs/${r.name}.log`,
      sha256: r.sha256,
      bytes: r.bytes,
    })),
  };
  mkdirSync(outDir, { recursive: true });
  writeFileSync(join(outDir, "manifest.json"), `${JSON.stringify(manifest, null, 1)}\n`);
  writeFileSync(join(outDir, "SHA256SUMS"), `${runs.map((r) => `${r.sha256}  logs/${r.name}.log`).join("\n")}\n`);
  const md = summarize(runs);
  writeFileSync(join(outDir, "summary.md"), md);
  const ghSummary = process.env.GITHUB_STEP_SUMMARY;
  if (ghSummary) appendFileSync(ghSummary, `### ${job}\n${md}\n`);
  return manifest;
}

/** Re-verifies recorded log checksums so a tampered or truncated artifact fails the verifier. */
export function verifyManifest(outDir: string): string[] {
  const manifest = JSON.parse(readFileSync(join(outDir, "manifest.json"), "utf8")) as JobManifest;
  const problems: string[] = [];
  for (const s of manifest.steps) {
    let sum: string;
    try {
      sum = createHash("sha256")
        .update(readFileSync(join(outDir, s.log)))
        .digest("hex");
    } catch {
      problems.push(`${s.name}: log missing`);
      continue;
    }
    if (sum !== s.sha256) problems.push(`${s.name}: checksum mismatch`);
  }
  const failing = manifest.steps.some((s) => s.exit !== 0);
  if (failing && manifest.status !== "FAIL") problems.push("manifest status is PASS but a step failed");
  return problems;
}
