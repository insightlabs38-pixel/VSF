import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { attach } from "../../../../../tests/observation/support/world.ts";
import { recognizeSnapshot } from "../../../../../tests/corpus/src/evaluate.ts";
import { locate, refStep } from "./ref.ts";
import { launch, open } from "./session.ts";
import { WORKFLOWS } from "./workflows.ts";

const [id, idx] = process.argv.slice(2);
const wf = WORKFLOWS.find((w) => w.id === id);
if (!wf) throw new Error("no workflow");
const upto = Number(idx ?? 0);
const browser = await launch();
let h: Awaited<ReturnType<typeof attach>> | undefined;
const live = await open(browser, wf, async (page, origin) => {
  h = await attach(page, { allow: [origin] });
});
try {
  const { page, origin } = live.ctx;
  await page.goto(`${origin}${wf.start}`, { waitUntil: "load" });
  await page.waitForLoadState("networkidle", { timeout: 6000 }).catch(() => {});
  await page.waitForTimeout(wf.settle ?? 600);
  for (const [i, s] of wf.steps.entries()) {
    if (i >= upto) break;
    await refStep(page, s, live.ctx);
  }
  const s = wf.steps[upto];
  if (!s) throw new Error("no step");
  const el = await locate(page, s).elementHandle({ timeout: 5000 });
  if (!el) throw new Error("no element");
  console.log("HTML:", (await el.evaluate((e) => e.outerHTML.replace(/\s+/g, " ").slice(0, 500))));
  console.log("PARENT:", (await el.evaluate((e) => (e.parentElement?.outerHTML ?? "").replace(/\s+/g, " ").slice(0, 300))));
  console.log("ARIA:", (await locate(page, s).ariaSnapshot().catch(() => "n/a")).slice(0, 200));
  await (h as NonNullable<typeof h>).settle();
  const snap = (h as NonNullable<typeof h>).collector.replica.toRecognizerInput();
  const rec = recognizeSnapshot(snap, `inspect-${id}`, mkdtempSync(`${tmpdir()}/inspect-`)).result;
  const nodes = await page.evaluate(
    ([r, ids]) => {
      const sess = (window as unknown as { __vsf: { session: { elementOf(i: number): Element | null } } }).__vsf.session;
      return (ids as number[]).filter((n) => {
        const e = sess.elementOf(n);
        return !!e && (e === r || e.contains(r as Element) || (r as Element).contains(e));
      });
    },
    [el, rec.units.map((u) => u.node)] as const,
  );
  for (const u of rec.units.filter((x) => nodes.includes(x.node))) console.log("UNIT:", JSON.stringify(u).slice(0, 400));
  const comps = (rec.composites ?? []).filter((c) => c.members.some((m) => nodes.includes(m.node)) || nodes.includes(c.owner ?? -1));
  for (const c of comps) console.log("COMPOSITE:", c.kind, c.label, c.members.length);
  console.log("units total", rec.units.length, "usable", rec.units.filter((u) => u.usable).length);
} finally {
  await live.close();
  await browser.close();
}
