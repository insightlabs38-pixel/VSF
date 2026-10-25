import { createEnv } from "../../../../../../tests/browser/support/env.ts";
const { env, dispose } = await createEnv({ a: 0, b: 0 }, { shadow: "open" });
try {
  for (const f of ["plain.html", "svg.html"]) {
    const page = await env.context.newPage();
    const errs: string[] = [];
    page.on("pageerror", (e) => errs.push(String(e).slice(0, 120)));
    await page.goto(`http://127.0.0.1:18872/${f}`, { waitUntil: "load" });
    await env.send({ type: "grant", origin: "http://127.0.0.1:18872" });
    const tabId = await env.tabIdOf(page);
    const r = (await env.send({ type: "transform", tabId })) as Record<string, unknown>;
    console.log(f, JSON.stringify({ ok: r.ok, error: r.error, detail: r.detail, entities: r.entities, actions: r.actions }), errs);
  }
} finally { await dispose(); }
