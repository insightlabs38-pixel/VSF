import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { runLogged } from "@vsf/checks/runlogged";
import { findSecretShapes } from "@vsf/redact";
import { Ajv } from "ajv";
import type { Plan } from "./model.ts";
import { gateOf } from "./queue.ts";
import type { Paths, Problem } from "./validate.ts";

export interface EvidenceRun {
  task_id: string;
  name: string;
  command: string;
  exit_code: number;
  result: "PASS" | "FAIL" | "SKIPPED";
  reason?: string;
  log: string;
  sha256: string;
}

export interface EvidenceManifest {
  schema: "vsf-evidence-v1";
  batch: string;
  gate: string;
  runs: EvidenceRun[];
}

export const evidenceDir = (p: Paths, batch: string): string => join(p.root, "docs/deep/evidence", `VSF-${batch}`);

export function initEvidence(p: Paths, plan: Plan, batch: string): string {
  const dir = evidenceDir(p, batch);
  if (existsSync(join(dir, "manifest.json"))) throw new Error(`evidence bundle for ${batch} already exists`);
  mkdirSync(join(dir, "logs"), { recursive: true });
  const manifest: EvidenceManifest = { schema: "vsf-evidence-v1", batch, gate: gateOf(plan, batch), runs: [] };
  writeFileSync(join(dir, "manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`);
  return dir;
}

function listFiles(dir: string): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) out.push(...listFiles(full));
    else out.push(full);
  }
  return out;
}

/** A bundle is acceptable only if every run is checksummed, failures are explicit, and nothing secret-shaped is stored. */
export function checkEvidence(p: Paths, plan: Plan, batch: string): Problem[] {
  const out: Problem[] = [];
  const dir = evidenceDir(p, batch);
  const manifestPath = join(dir, "manifest.json");
  if (!existsSync(manifestPath)) return [{ id: "evidence.missing", message: `${manifestPath} not found` }];
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as EvidenceManifest;
  const ajv = new Ajv({ allErrors: true, strict: false });
  const validate = ajv.compile(JSON.parse(readFileSync(join(p.queueDir, "schemas/evidence.schema.json"), "utf8")));
  if (!validate(manifest)) {
    return (validate.errors ?? []).map((e) => ({
      id: "evidence.schema",
      message: `${e.instancePath || "/"} ${e.message}`,
    }));
  }
  if (manifest.batch !== batch) out.push({ id: "evidence.batch", message: `manifest says ${manifest.batch}` });
  if (manifest.gate !== gateOf(plan, batch))
    out.push({ id: "evidence.gate", message: `manifest says ${manifest.gate}` });
  const planned = new Set(plan.tasks.filter((t) => t.batch === batch).map((t) => t.id));
  for (const run of manifest.runs) {
    if (run.task_id !== "GATE" && !planned.has(run.task_id))
      out.push({ id: "evidence.unknown-task", message: `${run.name}: ${run.task_id}` });
    if (run.result === "PASS" && run.exit_code !== 0)
      out.push({ id: "evidence.pass-with-failure", message: `${run.name} exit ${run.exit_code}` });
    if (run.result === "FAIL" && run.exit_code === 0) out.push({ id: "evidence.fail-without-code", message: run.name });
    if (run.result === "SKIPPED" && !run.reason) out.push({ id: "evidence.skip-without-reason", message: run.name });
    const logPath = join(dir, run.log);
    if (!existsSync(logPath)) {
      out.push({ id: "evidence.log-missing", message: run.log });
      continue;
    }
    const sum = createHash("sha256").update(readFileSync(logPath)).digest("hex");
    if (sum !== run.sha256) out.push({ id: "evidence.checksum", message: `${run.log} does not match recorded sha256` });
  }
  for (const file of listFiles(dir)) {
    const kinds = findSecretShapes(readFileSync(file, "utf8"));
    if (kinds.length > 0)
      out.push({ id: "evidence.secret-shape", message: `${file.replace(`${p.root}/`, "")}: ${kinds.join(",")}` });
  }
  return out;
}

/** Runs a command, stores its redacted log under the bundle and appends a checksummed run record. */
export function recordRun(
  p: Paths,
  plan: Plan,
  batch: string,
  taskId: string,
  name: string,
  command: string[],
): EvidenceRun {
  const dir = evidenceDir(p, batch);
  const manifestPath = join(dir, "manifest.json");
  if (!existsSync(manifestPath)) throw new Error(`run evidence-init ${batch} first`);
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as EvidenceManifest;
  if (taskId !== "GATE" && !plan.tasks.some((t) => t.id === taskId && t.batch === batch)) {
    throw new Error(`${taskId} is not a task of ${batch}`);
  }
  if (manifest.runs.some((r) => r.task_id === taskId && r.name === name))
    throw new Error(`run ${taskId}/${name} already recorded`);
  const logName = `${taskId}.${name}`;
  const run = runLogged(logName, command, join(dir, "logs"), p.root);
  const entry: EvidenceRun = {
    task_id: taskId,
    name,
    command: command.join(" "),
    exit_code: run.exitCode,
    result: run.exitCode === 0 ? "PASS" : "FAIL",
    log: `logs/${logName}.log`,
    sha256: run.sha256,
  };
  manifest.runs.push(entry);
  writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
  return entry;
}
