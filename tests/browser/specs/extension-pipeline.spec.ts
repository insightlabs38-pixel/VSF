import { readFileSync } from "node:fs";
import type { Page } from "@playwright/test";
import { canonicalJson, ENVELOPE_VERSION, sha256Hex } from "@vsf/extension/envelope";
import { expect, test } from "../support/harness.ts";

const forms = JSON.parse(readFileSync(new URL("../../../apps/extension/bundles/forms.json", import.meta.url), "utf8"));

async function shellState(page: Page): Promise<{ hosts: number; inert: boolean; headings: string[] }> {
  return page.evaluate(() => ({
    hosts: document.querySelectorAll("[data-vsf-shell]").length,
    inert: document.body.hasAttribute("inert"),
    headings: [...document.querySelectorAll("h1")].map((h) => h.textContent ?? ""),
  }));
}

interface Ax {
  role: string;
  name: string;
  disabled: boolean;
}

/** What assistive technology is offered: the browser's accessibility tree sees into closed shadow roots. */
async function axNodes(page: Page): Promise<Ax[]> {
  const session = await page.context().newCDPSession(page);
  const { nodes } = (await session.send("Accessibility.getFullAXTree")) as {
    nodes: {
      ignored?: boolean;
      role?: { value: string };
      name?: { value: string };
      properties?: { name: string; value: { value: unknown } }[];
    }[];
  };
  await session.detach();
  return nodes
    .filter((n) => !n.ignored)
    .map((n) => ({
      role: n.role?.value ?? "",
      name: String(n.name?.value ?? ""),
      disabled: n.properties?.some((p) => p.name === "disabled" && p.value.value === true) ?? false,
    }));
}

/** The source page's own markup, with only the attribute the shell is allowed to set removed. */
const sourceMarkup = (page: Page): Promise<string> =>
  page.evaluate(() => {
    const clone = document.body.cloneNode(true) as HTMLElement;
    clone.removeAttribute("inert");
    return clone.outerHTML;
  });

test("a matching page is observed, compiled and rendered through three envelopes, then restored byte for byte", async ({
  env,
}) => {
  await env.send({ type: "grant", origin: env.a });
  const page = await env.context.newPage();
  await page.goto(`${env.a}/forms/`);
  const tabId = await env.tabIdOf(page);
  const before = await sourceMarkup(page);

  const reply = await env.send({ type: "transform", tabId });
  expect(reply).toMatchObject({ ok: true, app: "forms-order", state: "ready", match: "matched", reason: null });
  const ids = (reply as { ids: Record<string, string> }).ids;
  expect(new Set(Object.values(ids)).size).toBe(3);
  expect(await shellState(page)).toMatchObject({ hosts: 1, inert: true });
  expect(await sourceMarkup(page)).toBe(before);
  const ax = await axNodes(page);
  expect(ax).toContainEqual({ role: "textbox", name: "Email", disabled: false });
  expect(ax.filter((n) => n.role === "textbox")).toHaveLength(3);
  const submit = ax.find((n) => n.role === "button");
  expect(submit).toEqual({
    role: "button",
    name: "Unavailable, unusable: No verified operation contract exists for this source. Submit order",
    disabled: true,
  });

  expect(await env.send({ type: "restore", tabId })).toMatchObject({ ok: true, restored: true });
  expect(await shellState(page)).toMatchObject({ hosts: 0, inert: false });
  expect(await sourceMarkup(page)).toBe(before);
  expect(await env.send({ type: "restore", tabId })).toMatchObject({ ok: true, restored: false });
});

test("a page that no longer looks like its bundle is left as the source made it", async ({ env }) => {
  await env.send({ type: "grant", origin: env.a });
  const page = await env.context.newPage();
  await page.goto(`${env.a}/forms/`);
  await page.evaluate(() => {
    for (const i of document.querySelectorAll("input")) i.remove();
  });
  const tabId = await env.tabIdOf(page);
  const reply = await env.send({ type: "transform", tabId });
  expect(reply).toMatchObject({ ok: true, state: "source_ui", match: "mismatch" });
  expect(String((reply as { reason: string }).reason)).toMatch(/expected at least 3 textbox/);
  expect(await shellState(page)).toMatchObject({ hosts: 0, inert: false });
});

test("a page with no bundle, or an origin without a grant, is never touched", async ({ env }) => {
  await env.send({ type: "grant", origin: env.a });
  const table = await env.context.newPage();
  await table.goto(`${env.a}/table/`);
  expect(await env.send({ type: "transform", tabId: await env.tabIdOf(table) })).toMatchObject({
    ok: true,
    state: "source_ui",
    match: "no_bundle",
  });
  expect(await shellState(table)).toMatchObject({ hosts: 0, inert: false });

  const other = await env.context.newPage();
  await other.goto(`${env.b}/forms/`);
  expect(await env.send({ type: "transform", tabId: await env.tabIdOf(other) })).toMatchObject({
    ok: false,
    error: "origin_not_granted",
  });
  expect(await shellState(other)).toMatchObject({ hosts: 0, inert: false });
});

test("the renderer inside the page refuses envelopes that do not verify", async ({ env }) => {
  await env.send({ type: "grant", origin: env.a });
  const page = await env.context.newPage();
  await page.goto(`${env.a}/forms/`);
  const tabId = await env.tabIdOf(page);
  const worker = await env.worker();
  const digest = await sha256Hex(canonicalJson(forms));
  const make = (over: Record<string, unknown> = {}, body: Record<string, unknown> = {}) => ({
    v: ENVELOPE_VERSION,
    kind: "render",
    id: "rnd-00000099",
    parent: "cmp-00000099",
    seq: 3,
    source: { tab: tabId, frame: 0, origin: env.a, epoch: "epoch-00000099" },
    body: { app: "forms-order", bundle_digest: digest, state: "ready", bundle: forms, ...body },
    ...over,
  });
  const attempt = (render: unknown, epoch = "epoch-00000099", tab = tabId) =>
    worker.evaluate(
      async ({ t, render, epoch }) => {
        await chrome.scripting.executeScript({ target: { tabId: t }, files: ["shell.js"] });
        const [r] = await chrome.scripting.executeScript({
          target: { tabId: t },
          func: (a) =>
            (globalThis as unknown as { __vsfShell: { mount(a: unknown): Promise<unknown> } }).__vsfShell.mount(a),
          args: [{ render, tab: t, epoch, compiled: { id: "cmp-00000099", seq: 2 } }],
        });
        return r?.result;
      },
      { t: tab, render, epoch },
    );

  const edited = { ...forms, title: "Evil" };
  const cases: [string, unknown, string][] = [
    ["tampered bundle", make({}, { bundle: edited }), "envelope.digest"],
    [
      "other origin",
      make({ source: { tab: tabId, frame: 0, origin: "http://evil.test", epoch: "epoch-00000099" } }),
      "envelope.source_mismatch",
    ],
    [
      "other tab",
      make({ source: { tab: tabId + 1, frame: 0, origin: env.a, epoch: "epoch-00000099" } }),
      "envelope.source_mismatch",
    ],
    [
      "other epoch",
      make({ source: { tab: tabId, frame: 0, origin: env.a, epoch: "epoch-00000098" } }),
      "envelope.stale_epoch",
    ],
    ["wrong parent", make({ parent: "cmp-00000001" }), "envelope.parent"],
    ["replayed sequence", make({ seq: 2 }), "envelope.replay"],
    ["wrong version", make({ v: 2 }), "envelope.version"],
    ["app mismatch", make({}, { app: "orders-stage" }), "envelope.digest"],
  ];
  for (const [name, render, error] of cases) expect(await attempt(render), name).toMatchObject({ ok: false, error });
  expect(await shellState(page)).toMatchObject({ hosts: 0, inert: false });
  expect(await attempt(make())).toMatchObject({ ok: true, state: "ready" });
  expect(await shellState(page)).toMatchObject({ hosts: 1, inert: true });
});

test("the page cannot see or drive the shell", async ({ env }) => {
  await env.send({ type: "grant", origin: env.a });
  const page = await env.context.newPage();
  await page.goto(`${env.a}/forms/`);
  const tabId = await env.tabIdOf(page);
  expect(await env.send({ type: "transform", tabId })).toMatchObject({ ok: true, state: "ready" });
  const probe = await page.evaluate(() => {
    const host = document.querySelector("[data-vsf-shell]") as HTMLElement;
    return {
      shellGlobal: typeof (window as unknown as { __vsfShell?: unknown }).__vsfShell,
      envelopeGlobal: typeof (window as unknown as { __vsfEnvelope?: unknown }).__vsfEnvelope,
      shadow: host.shadowRoot === null,
    };
  });
  expect(probe).toEqual({ shellGlobal: "undefined", envelopeGlobal: "undefined", shadow: true });
});
