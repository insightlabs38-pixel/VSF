import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { test } from "node:test";
import { type JobFile, runJob } from "../src/ci.ts";
import { checkWorkflow, evaluateGate, GATE, parseWorkflow } from "../src/gate.ts";

const root = resolve(import.meta.dirname, "../../..");
const text = readFileSync(resolve(root, ".github/workflows/ci.yml"), "utf8");
const jobsFile = (JSON.parse(readFileSync(resolve(root, "ci/jobs.json"), "utf8")) as JobFile).jobs;
const wf = parseWorkflow(text);

test("the real workflow passes its own structural check", () => {
  assert.deepEqual(checkWorkflow(wf, jobsFile), []);
  assert.ok(wf.jobs.some((j) => j.id === "commit-policy" && j.condition === "github.event_name == 'pull_request'"));
});

test("the gate waits for every other job, commit-policy included", () => {
  const gate = wf.jobs.find((j) => j.id === GATE);
  const others = wf.jobs.filter((j) => j.id !== GATE).map((j) => j.id);
  assert.deepEqual([...(gate?.needs ?? [])].sort(), [...others].sort());
});

const drop = (what: (t: string) => string): string[] => checkWorkflow(parseWorkflow(what(text)), jobsFile);

test("a gate that forgets a job, can be skipped or lets a job continue on error is rejected", () => {
  assert.ok(
    drop((t) => t.replace(", commit-policy, failure-canary]", ", failure-canary]")).some((p) =>
      /does not wait for job commit-policy/.test(p),
    ),
  );
  assert.ok(drop((t) => t.replace("    if: always()\n    needs:", "    needs:")).some((p) => /always/.test(p)));
  assert.ok(
    drop((t) =>
      t.replace(
        "  ts:\n    runs-on: ubuntu-24.04\n",
        "  ts:\n    runs-on: ubuntu-24.04\n    continue-on-error: true\n",
      ),
    ).some((p) => /continue-on-error/.test(p)),
  );
  assert.ok(
    drop((t) => t.replace("  ts:\n    runs-on", "  ts:\n    if: false\n    runs-on")).some((p) =>
      /unrecognised condition/.test(p),
    ),
  );
  assert.ok(
    drop((t) => t.replace("run: node tools/checks/src/gate-cli.ts .github/workflows/ci.yml", "run: echo ok")).some(
      (p) => /gate-cli/.test(p),
    ),
  );
  assert.ok(
    drop((t) => t.replace("  pull_request:\n", "  pull_request_target:\n")).some((p) => /pull_request_target/.test(p)),
  );
});

test("a ci job that no workflow job runs, or has no steps, is detected", () => {
  const extra = { ...jobsFile, orphan: [{ name: "x", cmd: ["true"] }], hollow: [] };
  const problems = checkWorkflow(wf, extra);
  assert.ok(problems.some((p) => /orphan is never run/.test(p)));
  assert.ok(problems.some((p) => /hollow has no steps/.test(p)));
});

const required = wf.jobs.filter((j) => j.id !== GATE).map((j) => ({ id: j.id, condition: j.condition }));
const allGreen = Object.fromEntries(required.map((j) => [j.id, { result: "success" }]));

test("all green passes on a pull request and on a push", () => {
  assert.equal(evaluateGate({ results: allGreen, event: "pull_request", required }).ok, true);
  const push = { ...allGreen, "commit-policy": { result: "skipped" } };
  assert.equal(evaluateGate({ results: push, event: "push", required }).ok, true);
});

test("a failed commit-policy cannot coexist with a green gate", () => {
  const failed = { ...allGreen, "commit-policy": { result: "failure" } };
  for (const event of ["pull_request", "push"]) {
    assert.equal(evaluateGate({ results: failed, event, required }).ok, false, event);
  }
});

test("skipped, cancelled, absent and failed required jobs all fail the gate", () => {
  for (const id of [
    "hygiene",
    "native",
    "go",
    "ts",
    "wasm-browser",
    "cross-language",
    "browser",
    "observation",
    "failure-canary",
  ]) {
    for (const result of ["skipped", "cancelled", "failure"]) {
      assert.equal(
        evaluateGate({ results: { ...allGreen, [id]: { result } }, event: "pull_request", required }).ok,
        false,
        `${id}/${result}`,
      );
    }
    const { [id]: _gone, ...without } = allGreen;
    assert.equal(evaluateGate({ results: without, event: "pull_request", required }).ok, false, `${id} absent`);
  }
});

test("commit-policy skipped on a pull request is a failure, not an exemption", () => {
  const skipped = { ...allGreen, "commit-policy": { result: "skipped" } };
  assert.equal(evaluateGate({ results: skipped, event: "pull_request", required }).ok, false);
});

test("a job with no steps cannot pass vacuously", () => {
  assert.throws(() => runJob(root, { jobs: { hollow: [] } }, "hollow", "/tmp/unused-hollow", false), /no steps/);
});
