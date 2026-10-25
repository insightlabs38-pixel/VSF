import { mkdirSync, writeFileSync } from "node:fs";
import { attach } from "../../../../../tests/observation/support/world.ts";
import { launch, open } from "../harness/session.ts";
import { locate, refStep } from "../harness/ref.ts";
import { WORKFLOWS } from "../harness/workflows.ts";

/** EXPERIMENTAL: saves, for every target step, the observer snapshot the recognizer would see and the node of the control the reference driver acts on. */
const outDir = process.argv.find((a) => a.startsWith("--out="))?.slice(6) ?? "snaps";
const want = process.argv.slice(2).filter((a) => !a.startsWith("--"));
mkdirSync(outDir, { recursive: true });
const browser = await launch();
for (const wf of WORKFLOWS.filter((w) => want.length === 0 || want.some((x) => w.id === x || w.app === x))) {
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
      if (s.do !== "press" && s.do !== "type") {
        const ref = await locate(page, s).elementHandle({ timeout: 6000 }).catch(() => null);
        await page.waitForTimeout(300);
        await (h as NonNullable<typeof h>).settle();
        const snap = (h as NonNullable<typeof h>).collector.replica.toRecognizerInput();
        let refNode: number | null = null;
        if (ref) {
          refNode = await page.evaluate(
            ([r, ids]) => {
              const sess = (window as unknown as { __vsf: { session: { elementOf(i: number): Element | null } } }).__vsf.session;
              let best: { id: number; depth: number } | null = null;
              for (const id of ids as number[]) {
                const e = sess.elementOf(id);
                if (!e) continue;
                if (e === r || e.contains(r as Element)) {
                  let d = 0;
                  for (let p: Element | null = e; p; p = p.parentElement) d++;
                  if (!best || d > best.depth) best = { id, depth: d };
                }
              }
              return best?.id ?? null;
            },
            [ref, (snap.nodes as { id: number }[]).map((n) => n.id)] as const,
          );
        }
        writeFileSync(`${outDir}/${wf.id}-${i}.json`, JSON.stringify({ wf: wf.id, step: i, do: s.do, target: s.target ?? "", refNode, snapshot: snap }));
      }
      await refStep(page, s, live.ctx).catch(() => {});
    }
    console.log(wf.id, "ok");
  } finally {
    await live.close();
  }
}
await browser.close();
