import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { Page } from "@playwright/test";
import { expect, test } from "../support/harness.ts";

/** Playwright evaluates with a user gesture; a raw CDP evaluate without one is what a script alone gets. */
async function evaluateWithoutGesture(page: Page, expression: string): Promise<unknown> {
  const session = await page.context().newCDPSession(page);
  const r = (await session.send("Runtime.evaluate", {
    expression,
    userGesture: false,
    awaitPromise: true,
    returnByValue: true,
  })) as { result: { value?: unknown }; exceptionDetails?: { exception?: { description?: string } } };
  await session.detach();
  return r.exceptionDetails ? (r.exceptionDetails.exception?.description ?? "exception") : r.result.value;
}

interface Violation {
  directive: string;
  blocked: string;
}

test("a fresh install holds no host access and only the reviewed permissions", async ({ env }) => {
  const held = await env.panel.evaluate(async () => {
    const all = await chrome.permissions.getAll();
    return { origins: all.origins, permissions: all.permissions };
  });
  expect(held.permissions?.sort()).toEqual(["scripting", "storage"]);
  expect(held.origins).toEqual(["http://127.0.0.1/*"]);
});

test("a grant is only recorded for an origin the browser also granted", async ({ env }) => {
  expect(await env.send({ type: "grant", origin: "http://localhost:1" })).toMatchObject({
    ok: false,
    error: "permission_not_held",
  });
  expect(await env.send({ type: "grant", origin: "https://example.test" })).toMatchObject({
    ok: false,
    error: "permission_not_held",
  });
  expect(((await env.send({ type: "status" })) as { grants: string[] }).grants).toEqual([]);
  expect(await env.send({ type: "grant", origin: env.a })).toMatchObject({ ok: true });
});

test("authorizing needs a real click, and a scripted click is refused before any request", async ({ env }) => {
  await env.panel.goto(`chrome-extension://${env.extId}/panel.html`);
  const page = await env.context.newPage();
  await page.goto(`${env.a}/forms/`);
  await page.bringToFront();
  await evaluateWithoutGesture(env.panel, "document.getElementById('authorize').click()");
  await expect(env.panel.locator("#result")).toHaveText("needs a click");
  expect(((await env.send({ type: "status" })) as { grants: string[] }).grants).toEqual([]);
  await env.panel.bringToFront();
  await env.panel.locator("#authorize").click();
  // The panel is itself the active tab here, so the click passes the gesture gate and then finds no page.
  await expect(env.panel.locator("#result")).toHaveText("no page");
});

test("the browser itself refuses a permission request made without a user gesture", async ({ env }) => {
  const outcome = await evaluateWithoutGesture(
    env.panel,
    "chrome.permissions.request({ origins: ['https://example.test/*'] }).then(() => 'granted', (e) => e.message)",
  );
  expect(String(outcome)).toMatch(/user gesture/i);
});

test("extension pages cannot run script from anywhere but the package", async ({ env }) => {
  await env.panel.evaluate(() => {
    const w = window as unknown as { __violations: Violation[] };
    w.__violations = [];
    document.addEventListener("securitypolicyviolation", (e) =>
      w.__violations.push({ directive: e.violatedDirective, blocked: e.blockedURI }),
    );
    (window as unknown as { __ran: boolean }).__ran = false;
    const remote = document.createElement("script");
    remote.src = "http://127.0.0.1:1/remote.js";
    document.head.append(remote);
    const inline = document.createElement("script");
    inline.textContent = "window.__ran = true";
    document.head.append(inline);
    const handler = document.createElement("img");
    handler.setAttribute("onerror", "window.__ran = true");
    handler.src = "data:,";
    document.body.append(handler);
  });
  await env.panel.waitForFunction(() => (window as unknown as { __violations: unknown[] }).__violations.length >= 2);
  const result = await env.panel.evaluate(() => ({
    ran: (window as unknown as { __ran: boolean }).__ran,
    violations: (window as unknown as { __violations: Violation[] }).__violations,
  }));
  expect(result.ran).toBe(false);
  expect(result.violations.every((v) => v.directive.startsWith("script-src"))).toBe(true);
});

test("capturing a granted page makes no network request of the extension's own", async ({ env }) => {
  await env.send({ type: "grant", origin: env.a });
  const page = await env.context.newPage();
  await page.goto(`${env.a}/forms/`);
  const tabId = await env.tabIdOf(page);
  const seen: string[] = [];
  env.context.on("request", (r) => seen.push(r.url()));
  expect(await env.send({ type: "capture", tabId })).toMatchObject({ ok: true });
  expect(seen.filter((u) => !u.startsWith(env.a) && !u.startsWith("data:"))).toEqual([]);
});

test("the built package carries no remote locator", async ({ env }) => {
  const worker = await env.worker();
  const bundle = await worker.evaluate(async () => (await fetch(chrome.runtime.getURL("background.js"))).text());
  expect(bundle).not.toMatch(/https?:\/\/(?!www\.w3\.org|127\.0\.0\.1)/);
  expect(readFileSync(join(process.cwd(), "../../apps/extension/manifest.template.json"), "utf8")).not.toMatch(
    /update_url|externally_connectable/,
  );
});
