import { appendFileSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { launch } from "./session.ts";
import { runObserve } from "./vsf.ts";
import { CALIBRATION, WORKFLOWS } from "./workflows.ts";

const want = process.argv.slice(2).filter((a) => !a.startsWith("--"));
const out = process.argv.find((a) => a.startsWith("--out="))?.slice(6);
const picked = (process.argv.includes("--calib") ? CALIBRATION : WORKFLOWS).filter((w) => want.length === 0 || want.some((x) => w.id === x || w.app === x));
const browser = await launch();
for (const wf of picked) {
  const r = await runObserve(wf, browser).catch((e: Error) => ({ id: wf.id, app: wf.app, error: String(e.message).split("\n")[0] }));
  console.log(`${wf.id.padEnd(10)} ${"error" in r ? r.error : `${r.steps.length} steps, units at control ${r.steps.filter((s) => s.unitAtControl).length}, oracle ${r.oracle.ok}`}`);
  if (out) {
    mkdirSync(dirname(out), { recursive: true });
    appendFileSync(out, `${JSON.stringify(r)}\n`);
  }
}
await browser.close();
