import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createEnv } from "../../../../../tests/browser/support/env.ts";
import { SEED, start } from "./apps.ts";
import { WORKFLOWS } from "./workflows.ts";

/** EXPERIMENTAL (run with the SVG guard): saves the head of the generated interface's accessibility tree for chosen start states. */
const ids = process.argv.slice(2).filter((a) => !a.startsWith("--"));
const dir = process.argv.find((a) => a.startsWith("--out="))?.slice(6) ?? "overlay-samples";
mkdirSync(dir, { recursive: true });
for (const wf of WORKFLOWS.filter((w) => ids.includes(w.id))) {
  const run = await start(wf.app, wf.variant);
  const { env, dispose } = await createEnv({ a: 0, b: 0 }, { shadow: "open" });
  try {
    const storage = `${SEED}${wf.app}.storage.json`;
    if (existsSync(storage)) {
      const st = JSON.parse(readFileSync(storage, "utf8")) as { cookies: never[]; origins: { origin: string; localStorage: { name: string; value: string }[] }[] };
      if (st.cookies.length) await env.context.addCookies(st.cookies);
      for (const o of st.origins) await env.context.addInitScript(([origin, items]) => { if (location.origin === origin) for (const i of items as { name: string; value: string }[]) localStorage.setItem(i.name, i.value); }, [o.origin, o.localStorage] as const);
    }
    const page = await env.context.newPage();
    await page.goto(`${run.origin}${wf.start}`, { waitUntil: "load" });
    await page.waitForLoadState("networkidle", { timeout: 6000 }).catch(() => {});
    await page.waitForTimeout(wf.settle ?? 600);
    await env.send({ type: "grant", origin: run.origin });
    const tabId = await env.tabIdOf(page);
    const r = (await env.send({ type: "transform", tabId })) as { ok: boolean; entities?: number; rows?: number };
    await page.waitForTimeout(600);
    const snap = (await page.locator("[data-vsf-overlay]").ariaSnapshot().catch(() => "")).split("\n").map((l) => l.slice(0, 170));
    writeFileSync(`${dir}/${wf.id}.txt`, `# ${wf.id} ${wf.title}\n# transform ok=${r.ok} entities=${r.entities} rows=${r.rows}; first 70 lines of the generated interface's accessibility tree\n${snap.slice(0, 70).join("\n")}\n`);
  } finally {
    await dispose();
    await run.stop();
  }
}
