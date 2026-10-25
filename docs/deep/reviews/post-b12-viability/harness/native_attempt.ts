import { appendFileSync, existsSync, mkdirSync, readFileSync } from "node:fs";
import { dirname } from "node:path";
import { createEnv } from "../../../../../tests/browser/support/env.ts";
import { SEED, start } from "./apps.ts";
import { ORACLES } from "./oracles.ts";
import { WORKFLOWS } from "./workflows.ts";

/** Attempts a workflow through the generated overlay only, using the controls the overlay exposes; no source interaction. */
const ROLES = ["button", "link", "textbox", "combobox", "checkbox", "radio", "menuitem", "searchbox", "spinbutton"] as const;
const re = (s: string): RegExp => new RegExp(s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&").replace(/\s+/g, "\\s+"), "i");
const want = process.argv.slice(2).filter((a) => !a.startsWith("--"));
const out = process.argv.find((a) => a.startsWith("--out="))?.slice(6);
for (const wf of WORKFLOWS.filter((w) => want.includes(w.id))) {
  const run = await start(wf.app, wf.variant);
  const { env, dispose } = await createEnv({ a: 0, b: 0 }, { shadow: "open" });
  const row: Record<string, unknown> = { id: wf.id, app: wf.app, steps: [] as unknown[] };
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
    row.transform = ((await env.send({ type: "transform", tabId })) as { ok: boolean }).ok;
    await page.waitForTimeout(600);
    let done = true;
    for (const [i, s] of wf.steps.entries()) {
      const rec: Record<string, unknown> = { i, do: s.do, target: s.target ?? s.key ?? "" };
      (row.steps as unknown[]).push(rec);
      if (s.do === "press") {
        await page.keyboard.press(s.key ?? "Enter");
        continue;
      }
      let loc = null;
      for (const role of ROLES) {
        const l = page.locator("[data-vsf-overlay]").getByRole(role, { name: re(s.target ?? "") });
        if ((await l.count()) > 0) {
          loc = l.first();
          rec.role = role;
          break;
        }
      }
      if (!loc) {
        rec.result = "no_control_in_overlay";
        done = false;
        break;
      }
      if (await loc.isDisabled()) {
        rec.result = "disabled";
        done = false;
        break;
      }
      try {
        if (s.do === "fill") await loc.fill(s.value ?? "", { timeout: 3000 });
        else if (s.do === "choose") await loc.selectOption({ label: s.value ?? "" }, { timeout: 3000 });
        else if (s.do === "toggle") await loc.setChecked(s.checked ?? true, { timeout: 3000 });
        else await loc.click({ timeout: 3000 });
        rec.result = "done";
        await page.waitForTimeout(800);
      } catch (e) {
        rec.result = `error: ${String((e as Error).message).split("\n")[0]?.slice(0, 80)}`;
        done = false;
        break;
      }
    }
    const oracle = ORACLES[wf.oracle];
    const post = oracle ? await oracle({ run, page, origin: run.origin, downloads: [], files: new Map() }).catch((e: Error) => ({ ok: false, detail: `oracle error ${e.message.slice(0, 60)}` })) : { ok: false, detail: "no oracle" };
    row.allStepsDone = done;
    row.oracle = post;
    row.native = done && post.ok;
  } finally {
    await dispose();
    await run.stop();
  }
  console.log(JSON.stringify(row));
  if (out) {
    mkdirSync(dirname(out), { recursive: true });
    appendFileSync(out, `${JSON.stringify(row)}\n`);
  }
}
