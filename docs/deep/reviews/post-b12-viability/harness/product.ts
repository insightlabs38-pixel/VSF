import { appendFileSync, existsSync, mkdirSync, readFileSync } from "node:fs";
import { dirname } from "node:path";
import { createEnv } from "../../../../../tests/browser/support/env.ts";
import { norm } from "../../../../../tests/corpus/src/match.ts";
import { SEED, start } from "./apps.ts";
import type { Workflow } from "./spec.ts";
import { CALIBRATION, WORKFLOWS } from "./workflows.ts";

export interface ProductResult {
  id: string;
  app: string;
  transform: Record<string, unknown>;
  overlay: boolean;
  controls: { name: string; role: string; disabled: boolean }[];
  targets: { target: string; present: boolean; enabled: boolean }[];
  nativeFeasible: boolean;
  ms: number;
}

/** Runs the shipped extension on the workflow's start state and records what the generated interface offers. */
export async function runProduct(wf: Workflow): Promise<ProductResult> {
  const t0 = Date.now();
  const run = await start(wf.app, wf.variant);
  const { env, dispose } = await createEnv({ a: 0, b: 0 }, { shadow: "open", hostPermissions: ["http://127.0.0.1/*"] });
  try {
    const storage = `${SEED}${wf.app}.storage.json`;
    if (existsSync(storage)) {
      const st = JSON.parse(readFileSync(storage, "utf8")) as { cookies: never[]; origins: { origin: string; localStorage: { name: string; value: string }[] }[] };
      if (st.cookies.length) await env.context.addCookies(st.cookies);
      for (const o of st.origins)
        await env.context.addInitScript(
          ([origin, items]) => {
            if (location.origin === origin) for (const i of items as { name: string; value: string }[]) localStorage.setItem(i.name, i.value);
          },
          [o.origin, o.localStorage] as const,
        );
    }
    const page = await env.context.newPage();
    await page.goto(`${run.origin}${wf.start}`, { waitUntil: "load" });
    await page.waitForLoadState("networkidle", { timeout: 6000 }).catch(() => {});
    await page.waitForTimeout(wf.settle ?? 600);
    await env.send({ type: "grant", origin: run.origin });
    const tabId = await env.tabIdOf(page);
    const transform = (await env.send({ type: "transform", tabId })) as Record<string, unknown>;
    await page.waitForTimeout(600);
    const overlay = (await page.locator("[data-vsf-overlay]").count()) > 0;
    const controls = overlay
      ? await page.evaluate(() => {
          const host = document.querySelector("[data-vsf-overlay]") as HTMLElement | null;
          const root = host?.shadowRoot;
          if (!root) return [];
          return [...root.querySelectorAll("button, a, input, select, textarea, [role=button], [role=menuitem], [role=tab]")].map((e) => ({
            name: (e.getAttribute("aria-label") ?? e.textContent ?? "").trim().slice(0, 80),
            role: e.getAttribute("role") ?? e.tagName.toLowerCase(),
            disabled: (e as HTMLButtonElement).disabled === true || e.getAttribute("aria-disabled") === "true",
          }));
        })
      : [];
    const targets = wf.steps
      .filter((s) => s.do !== "press" && s.do !== "type" && s.target)
      .map((s) => {
        const hits = controls.filter((c) => norm(c.name).includes(norm(s.target)));
        return { target: s.target as string, present: hits.length > 0, enabled: hits.some((h) => !h.disabled) };
      });
    return { id: wf.id, app: wf.app, transform, overlay, controls: controls.slice(0, 60), targets, nativeFeasible: overlay && targets.length > 0 && targets.every((t) => t.present && t.enabled), ms: Date.now() - t0 };
  } finally {
    await dispose();
    await run.stop();
  }
}

if (process.argv[1]?.endsWith("product.ts")) {
  const want = process.argv.slice(2).filter((a) => !a.startsWith("--"));
  const out = process.argv.find((a) => a.startsWith("--out="))?.slice(6);
  const pool = process.argv.includes("--calib") ? CALIBRATION : WORKFLOWS;
  for (const wf of pool.filter((w) => want.length === 0 || want.some((x) => w.id === x || w.app === x))) {
    const r = await runProduct(wf).catch((e: Error) => ({ id: wf.id, app: wf.app, error: String(e.message).split("\n")[0] }));
    console.log(JSON.stringify(r).slice(0, 400));
    if (out) {
      mkdirSync(dirname(out), { recursive: true });
      appendFileSync(out, `${JSON.stringify(r)}\n`);
    }
  }
}
