import { appendFileSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { launch } from "./session.ts";
import { runVsf } from "./vsf.ts";
import { CALIBRATION, WORKFLOWS } from "./workflows.ts";

const want = process.argv.slice(2).filter((a) => !a.startsWith("--"));
const out = process.argv.find((a) => a.startsWith("--out="))?.slice(6);
const picked = (process.argv.includes("--calib") ? CALIBRATION : WORKFLOWS).filter((w) => want.length === 0 || want.some((x) => w.id === x || w.app === x));
const browser = await launch();
for (const wf of picked) {
  const r = await runVsf(wf, browser).catch((e: Error) => ({ id: wf.id, app: wf.app, outcome: "harness_error", error: String(e.message).split("\n")[0] }));
  const o = r as { outcome: string; stoppedAt?: number; cause?: string; ms?: number; error?: string };
  console.log(`${o.outcome.padEnd(17)} ${wf.id.padEnd(10)} ${o.ms ?? ""}ms stop=${o.stoppedAt ?? "-"} ${o.cause ?? ""} ${o.error ?? ""}`);
  if (out) {
    mkdirSync(dirname(out), { recursive: true });
    appendFileSync(out, `${JSON.stringify(r)}\n`);
  }
}
await browser.close();
