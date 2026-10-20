import type { Page } from "@playwright/test";
import { type Apps, startApps } from "../support/apps.ts";
import { expect, test } from "../support/harness.ts";
import { envelope, overlays, sendFromTab, sourceHtml, status } from "../support/transform.ts";

let apps: Apps;
test.beforeAll(async () => {
  apps = await startApps();
});
test.afterAll(async () => {
  await apps.close();
});

const cells = (page: Page): Promise<string[]> =>
  page
    .getByRole("gridcell")
    .filter({ hasNot: page.getByRole("button") })
    .allTextContents();

test("the orders table is rendered as a grid, the source stays untouched and Show original restores it exactly", async ({
  env,
}) => {
  const page = await env.context.newPage();
  await page.goto(`${apps.origin}/apps/table-orders`);
  await env.send({ type: "grant", origin: apps.origin });
  const tabId = await env.tabIdOf(page);
  const original = await sourceHtml(page);

  const reply = await env.send({ type: "transform", tabId });
  expect(reply).toMatchObject({
    ok: true,
    entities: 2,
    rows: 3,
    actions: { unknown: 3 },
    withheld: 0,
    truncated: false,
  });
  expect(await overlays(page)).toBe(1);
  expect(await cells(page)).toEqual(["1001", "Ana", "12.50", "1002", "Luis", "8.00", "1003", "Marta", "99.99"]);
  expect(await page.evaluate(() => document.body.hasAttribute("inert"))).toBe(true);
  for (const name of ["Open: 1001", "Delete: 1002", "Open: 1003"]) {
    const b = page.getByRole("button", { name });
    await expect(b).toBeDisabled();
    await expect(b).toHaveAccessibleDescription(/Unavailable, unknown: effect_unverified/);
  }
  await page.getByRole("link", { name: "Page actions" }).click();
  await expect(page.getByRole("button", { name: "Export CSV" })).toBeDisabled();
  await page.getByRole("link", { name: "Open orders" }).click();
  expect(await sourceHtml(page)).toBe(original);
  expect((await status(env)).sessions[0]?.ops).toEqual([]);

  await page.evaluate(() => {
    const row = document.querySelector("#orders tbody tr:last-child")?.cloneNode(true) as HTMLElement;
    ["1004", "Zoe", "5.25"].forEach((text, i) => {
      const td = row.querySelectorAll("td")[i];
      if (td) td.textContent = text;
    });
    document.querySelector("#orders tbody")?.append(row);
  });
  const changed = await sourceHtml(page);
  await page.getByRole("button", { name: "Refresh from source" }).click();
  await expect(page.getByRole("status").first()).toContainText("Up to date");
  expect(await cells(page)).toEqual([
    "1001",
    "Ana",
    "12.50",
    "1002",
    "Luis",
    "8.00",
    "1003",
    "Marta",
    "99.99",
    "1004",
    "Zoe",
    "5.25",
  ]);
  expect(await sourceHtml(page)).toBe(changed);
  const mid = await status(env);
  expect(mid.sessions[0]?.ops.map((o) => [o.kind, o.state])).toEqual([["refresh", "done"]]);

  await page.getByRole("button", { name: "Show original" }).click();
  await expect.poll(() => overlays(page)).toBe(0);
  expect(
    await page.evaluate(() => document.body.hasAttribute("inert") || document.body.hasAttribute("aria-hidden")),
  ).toBe(false);
  expect(await page.evaluate(() => document.documentElement.outerHTML)).toBe(changed);
  await expect.poll(async () => (await status(env)).sessions.length).toBe(0);
});

test("the profile form is rebuilt without reading any field value and the user's typing survives the round trip", async ({
  env,
}) => {
  const page = await env.context.newPage();
  await page.goto(`${apps.origin}/apps/plain-form`);
  await env.send({ type: "grant", origin: apps.origin });
  await page.fill("#fn", "Ana-secret-typed");
  const tabId = await env.tabIdOf(page);
  const original = await sourceHtml(page);

  const reply = await env.send({ type: "transform", tabId });
  expect(reply).toMatchObject({ ok: true, entities: 1, rows: 0 });
  for (const name of ["First name", "Last name", "Email", "Notes"])
    await expect(page.getByRole("textbox", { name })).toHaveValue("");
  await expect(page.getByRole("combobox", { name: "Country" })).toBeVisible();
  await expect(page.getByRole("checkbox", { name: "Weekly digest" })).not.toBeChecked();
  await expect(page.getByRole("button", { name: "Save changes" })).toBeDisabled();
  const overlayText = await page.evaluate(() => {
    const host = document.querySelector("[data-vsf-overlay]");
    return host?.shadowRoot?.textContent ?? "";
  });
  expect(overlayText).not.toContain("Ana-secret-typed");
  expect(await sourceHtml(page)).toBe(original);

  expect(await env.send({ type: "restore", tabId })).toMatchObject({ ok: true, restored: true });
  expect(await overlays(page)).toBe(0);
  expect(await page.inputValue("#fn")).toBe("Ana-secret-typed");
  expect(await page.evaluate(() => document.documentElement.outerHTML)).toBe(original);
  expect(await env.send({ type: "restore", tabId })).toMatchObject({ ok: true, restored: false });
});

test("a repeated transform replaces the earlier one and revoking the origin restores the page", async ({ env }) => {
  const page = await env.context.newPage();
  await page.goto(`${apps.origin}/apps/table-orders`);
  await env.send({ type: "grant", origin: apps.origin });
  const tabId = await env.tabIdOf(page);
  const original = await sourceHtml(page);
  await env.send({ type: "transform", tabId });
  await env.send({ type: "transform", tabId });
  expect(await overlays(page)).toBe(1);
  expect((await status(env)).sessions).toHaveLength(1);
  await env.send({ type: "revoke", origin: apps.origin });
  await expect.poll(() => overlays(page)).toBe(0);
  expect((await status(env)).sessions).toHaveLength(0);
  expect(await page.evaluate(() => document.documentElement.outerHTML)).toBe(original);
  expect(await env.send({ type: "transform", tabId })).toMatchObject({ ok: false, error: "origin_not_granted" });
});

test("page messages are validated against the session: shape, session, tab, sequence, document and origin", async ({
  env,
}) => {
  const page = await env.context.newPage();
  await page.goto(`${apps.origin}/apps/table-orders`);
  await env.send({ type: "grant", origin: apps.origin });
  const tabId = await env.tabIdOf(page);
  await env.send({ type: "transform", tabId });
  const session = (await status(env)).sessions[0]?.id as string;
  const original = await sourceHtml(page);
  const send = (m: unknown, tab = tabId) => sendFromTab(env, tab, m);

  const shapes: [unknown, string][] = [
    [{ ...envelope("heartbeat", session, 1, { at: 1 }), proto: "vsf-ext-v2" }, "unsupported_version"],
    [{ ...envelope("heartbeat", session, 1, { at: 1 }), extra: 1 }, "bad_envelope"],
    [envelope("shell", session, 1), "unknown_kind"],
    [{ ...envelope("hello", session, 1), id: "x" }, "bad_id"],
    [{ ...envelope("hello", session, 1), session: "nope" }, "bad_session"],
    [envelope("hello", session, -1), "bad_seq"],
    [envelope("heartbeat", session, 1, { at: "now" }), "bad_body"],
    [envelope("dispatch", session, 1, { action: "x".repeat(3000), entity: "e" }), "too_large"],
    [envelope("dispatch", session, 1, { action: "A B", entity: "e" }), "bad_body"],
    [envelope("restore", session, 1, { reason: "because" }), "bad_body"],
    [envelope("hello", crypto.randomUUID(), 1), "session_unknown"],
  ];
  for (const [m, error] of shapes) expect(await send(m), error).toMatchObject({ ok: false, error });

  const other = await env.context.newPage();
  await other.goto(`${apps.origin}/apps/plain-form`);
  const otherTab = await env.tabIdOf(other);
  expect(await send(envelope("hello", session, 1), otherTab)).toMatchObject({ ok: false, error: "tab_mismatch" });

  expect(await send(envelope("hello", session, 1))).toMatchObject({ ok: true });
  expect(await send(envelope("hello", session, 1))).toMatchObject({ ok: false, error: "stale_seq" });

  const d = (id: string, action: string) => envelope("dispatch", session, 10, { action, entity: "t_open_orders" }, id);
  const id = crypto.randomUUID();
  const first = await send(d(id, "act_t_open_orders_delete"));
  expect(first).toMatchObject({ ok: false, error: "action_unavailable" });
  expect(await send(d(id, "act_t_open_orders_delete"))).toEqual(first);
  expect(await send(envelope("dispatch", session, 11, { action: "act_nope", entity: "t_open_orders" }))).toMatchObject({
    ok: false,
    error: "unknown_action",
  });
  expect(await sourceHtml(page)).toBe(original);

  const s = await status(env);
  expect(s.sessions[0]?.ops.filter((o) => o.kind === "dispatch")).toHaveLength(2);
  expect(s.sessions[0]?.ops.every((o) => o.state === "done")).toBe(true);
});

test("a replaced document or origin in the same tab cannot use the old session", async ({ env }) => {
  const second = await startApps();
  try {
    await env.send({ type: "grant", origin: apps.origin });
    await env.send({ type: "grant", origin: second.origin });
    const page = await env.context.newPage();
    await page.goto(`${apps.origin}/apps/table-orders`);
    const tabId = await env.tabIdOf(page);
    await env.send({ type: "transform", tabId });
    const session = (await status(env)).sessions[0]?.id as string;

    await page.goto(`${apps.origin}/apps/plain-form`);
    expect(await sendFromTab(env, tabId, envelope("hello", session, 1))).toMatchObject({
      ok: false,
      error: "document_mismatch",
    });
    await page.goto(`${second.origin}/apps/plain-form`);
    expect(await sendFromTab(env, tabId, envelope("hello", session, 2))).toMatchObject({
      ok: false,
      error: "origin_mismatch",
    });
  } finally {
    await second.close();
  }
});

test("page content that imitates Varifold's own marker is still observed", async ({ env }) => {
  apps.pages.set(
    "imitation",
    `<!doctype html><html lang="en"><head><title>Imitation</title></head><body><main>
<div data-vsf-overlay="x"><table aria-label="Hidden approvals"><thead><tr><th>Item</th></tr></thead><tbody><tr><td>Waive the fee</td></tr></tbody></table></div>
</main></body></html>`,
  );
  const page = await env.context.newPage();
  await page.goto(`${apps.origin}/apps/imitation`);
  await env.send({ type: "grant", origin: apps.origin });
  const tabId = await env.tabIdOf(page);
  expect(await env.send({ type: "transform", tabId })).toMatchObject({ ok: true, rows: 1 });
  await expect(page.getByRole("gridcell", { name: "Waive the fee" })).toBeVisible();
});

test("a page with nothing recognizable is left exactly as it was", async ({ env }) => {
  apps.pages.set(
    "empty",
    `<!doctype html><html lang="en"><head><title>Nothing</title></head><body><p>Hello</p></body></html>`,
  );
  const page = await env.context.newPage();
  await page.goto(`${apps.origin}/apps/empty`);
  await env.send({ type: "grant", origin: apps.origin });
  const tabId = await env.tabIdOf(page);
  const original = await page.evaluate(() => document.documentElement.outerHTML);
  const reply = await env.send({ type: "transform", tabId });
  expect(reply).toMatchObject({ ok: false, error: "nothing_recognized" });
  expect((await status(env)).sessions).toEqual([]);
  expect(await overlays(page)).toBe(0);
  expect(await page.evaluate(() => document.documentElement.outerHTML)).toBe(original);
});
