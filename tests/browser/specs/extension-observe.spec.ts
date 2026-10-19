import { expect, test } from "../support/harness.ts";

test("authorized origin is observed; ungranted and foreign origins stay unobserved", async ({ env }) => {
  const pageA = await env.context.newPage();
  await pageA.goto(`${env.a}/forms/`);
  const pageB = await env.context.newPage();
  await pageB.goto(`${env.b}/forms/`);
  const pageC = await env.context.newPage();
  await pageC.goto(`${env.c}/forms/`);
  const [ta, tb, tc] = [await env.tabIdOf(pageA), await env.tabIdOf(pageB), await env.tabIdOf(pageC)];

  expect(await env.send({ type: "capture", tabId: ta })).toMatchObject({ ok: false, error: "origin_not_granted" });
  await env.send({ type: "grant", origin: env.a });
  const ok = await env.send({ type: "capture", tabId: ta });
  expect(ok, JSON.stringify(ok)).toMatchObject({ ok: true, origin: env.a });
  const summary = ok.summary as { node_count: number; classes: Record<string, number>; digest: string };
  expect(summary.node_count).toBeGreaterThan(10);
  expect(summary.classes.form).toBe(1);
  expect(summary.digest).toMatch(/^[0-9a-f]{64}$/);

  expect(await env.send({ type: "capture", tabId: tb })).toMatchObject({ ok: false, error: "origin_not_granted" });
  const foreign = await env.send({ type: "capture", tabId: tc });
  expect(foreign).toMatchObject({ ok: false, error: "origin_unobservable" });

  const status = (await env.send({ type: "status" })) as { stats: { executeCalls: Record<string, number> } };
  expect(status.stats.executeCalls[env.a]).toBe(1);
  expect(status.stats.executeCalls[env.b]).toBeUndefined();
  expect(status.stats.executeCalls[env.c]).toBeUndefined();
});

test("the extension runtime sees closed shadow roots and says which limits still apply", async ({ env }) => {
  const page = await env.context.newPage();
  await page.goto(`${env.a}/shadow/`);
  const tab = await env.tabIdOf(page);
  expect(await env.send({ type: "observe", tabId: tab })).toMatchObject({ ok: false, error: "origin_not_granted" });
  await env.send({ type: "grant", origin: env.a });
  const r = await env.send({ type: "observe", tabId: tab });
  expect(r, JSON.stringify(r)).toMatchObject({ ok: true, origin: env.a, state: "synced" });
  expect(r.shadow).toEqual({ open: 1, closed: 1 });
  const caps = r.capabilities as { id: string; state: string }[];
  expect(caps.find((c) => c.id === "observe.shadow.closed")?.state).toBe("available");
  expect(caps.find((c) => c.id === "observe.frames.cross_origin")?.state).toBe("unavailable");
  const missing = r.missing as { signal: string; reason: string }[];
  expect(missing.some((m) => m.signal === "closed_shadow_root")).toBe(false);
  expect(missing.some((m) => m.signal === "accessibility_tree" && m.reason === "not_supported")).toBe(true);
  expect(r.epoch).toBeGreaterThan(0);
  const summary = r.summary as { node_count: number };
  const plain = await env.send({ type: "capture", tabId: tab });
  expect(summary.node_count).toBeGreaterThan((plain.summary as { node_count: number }).node_count);
  const foreign = await env.context.newPage();
  await foreign.goto(`${env.c}/shadow/`);
  expect(await env.send({ type: "observe", tabId: await env.tabIdOf(foreign) })).toMatchObject({
    ok: false,
    error: "origin_unobservable",
  });
});
