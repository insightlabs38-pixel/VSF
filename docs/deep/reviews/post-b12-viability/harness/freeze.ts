import { createHash } from "node:crypto";
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { CACHE, ROOT } from "./apps.ts";
import { CALIBRATION, WORKFLOWS } from "./workflows.ts";

const sha = (b: Buffer | string): string => createHash("sha256").update(b).digest("hex");
const base = `${ROOT}docs/deep/reviews/post-b12-viability/`;
const files = (dir: string): string[] => readdirSync(dir, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? files(`${dir}/${e.name}`) : [`${dir}/${e.name}`]));
const harness = files(`${base}harness`).sort().map((f) => `${sha(readFileSync(f))}  ${f.slice(base.length)}`);
writeFileSync(`${base}evidence/harness.sha256`, `${harness.join("\n")}\n`);

const frozen = {
  schema: "vsf-audit-workflows-frozen-v1",
  note: "Declarative specifications as registered before any VSF run on these applications; oracles are in harness/apps/*.ts.",
  workflows: WORKFLOWS.map((w) => ({ id: w.id, app: w.app, variant: w.variant ?? null, title: w.title, objective: w.objective, effect: w.effect, categories: w.categories, difficulty: w.difficulty, start: w.start, steps: w.steps.length, targets: w.steps.filter((s) => s.target).map((s) => s.target), oracle: w.oracle })),
  calibration: CALIBRATION.map((w) => w.id),
};
writeFileSync(`${base}evidence/WORKFLOWS_FROZEN.json`, `${JSON.stringify(frozen, null, 1)}\n`);

const git = (...a: string[]): string => spawnSync("git", a, { cwd: ROOT, encoding: "utf8" }).stdout.trim();
const env = {
  audited_commit: git("rev-parse", "origin/main"),
  review_base: git("rev-parse", "HEAD"),
  recognizer_sha256: sha(readFileSync(`${ROOT}compiler/target/release/vsf-recognizer`)),
  wasm_sha256: sha(readFileSync(`${ROOT}artifacts/wasm/vsf_wasm.wasm`)),
  chromium: spawnSync(`${process.env.PLAYWRIGHT_BROWSERS_PATH}/chromium-1194/chrome-linux/chrome`, ["--version"], { encoding: "utf8" }).stdout.trim(),
  node: process.version,
  apps: {
    gitea: sha(readFileSync(`${CACHE}bin/gitea`)),
    pocketbase_binary: sha(readFileSync(`${CACHE}bin/pocketbase`)),
    pocketbase_zip: sha(readFileSync(`${CACHE}bin/pocketbase.zip`)),
    kanboard_rev: spawnSync("git", ["-C", `${CACHE}kanboard-src`, "rev-parse", "HEAD"], { encoding: "utf8" }).stdout.trim(),
    gradio_demo_rev: spawnSync("git", ["-C", `${CACHE}gradio-src`, "rev-parse", "HEAD"], { encoding: "utf8" }).stdout.trim(),
    petstore_spec_rev: spawnSync("git", ["-C", `${CACHE}swagger/petstore-src`, "rev-parse", "HEAD"], { encoding: "utf8" }).stdout.trim(),
    swagger_ui_dist: JSON.parse(readFileSync(`${CACHE}swagger/node_modules/swagger-ui-dist/package.json`, "utf8")).version,
    python_packages: sha(readFileSync(`${CACHE}pip-freeze-main.txt`)),
    python_packages_gradio: sha(readFileSync(`${CACHE}pip-freeze-gradio.txt`)),
  },
};
writeFileSync(`${base}evidence/environment.json`, `${JSON.stringify(env, null, 1)}\n`);
console.log(JSON.stringify(env, null, 1));
console.log(`${frozen.workflows.length} workflows`);
