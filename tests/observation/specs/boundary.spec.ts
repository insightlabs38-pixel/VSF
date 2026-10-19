import { expect, type Page, test } from "@playwright/test";
import {
  FLAG_ACCOUNT,
  FLAG_CREDENTIAL,
  type NodeRec,
  observationBody,
  type SharePolicy,
  sharedEvidence,
} from "@vsf/observer";
import { BOUNDARY, boundaryPage } from "../support/pages.ts";
import { attach, startWorld } from "../support/world.ts";

const PRINCIPAL = "prin_0123456789abcdef0123456789abcdef";
const NOW = 1_790_000_000_000;

async function boot(page: Page) {
  const world = await startWorld();
  world.a.pages.set("/", boundaryPage);
  const h = await attach(page, { allow: [world.a.url] });
  await page.goto(`${world.a.url}/`);
  await h.settle();
  const nodes = () => [...h.collector.replica.nodes.values()];
  const byId = (needle: string): NodeRec | undefined => nodes().find((n) => n.text === needle || n.name === needle);
  const policy = (roles: string[]): SharePolicy => ({
    schema: "vsf-share-policy-v1",
    id: "spol_0123456789abcdef",
    origin: world.a.url,
    principal_ref: PRINCIPAL,
    text_roles: roles,
    max_chars: 60,
    issued_at: NOW - 1000,
    expires_at: NOW + 3600_000,
  });
  const ctx = { origin: world.a.url, principal: PRINCIPAL, now: NOW };
  return { world, h, nodes, byId, policy, ctx };
}

const wire = (h: { collector: { batches: unknown[] } }) => JSON.stringify(h.collector.batches);

test("a public marker never lowers the intrinsic classification of credential controls", async ({ page }) => {
  const { world, h, nodes } = await boot(page);
  const controls = nodes().filter((n) => n.tag === "input" || n.role === "textbox");
  const cred = controls.filter((n) => n.flags & FLAG_CREDENTIAL);
  expect(cred.length).toBe(9);
  expect(cred.every((n) => n.text === null)).toBe(true);
  expect(wire(h)).not.toContain("hunter2");
  await world.stop();
});

test("benign look-alikes stay usable: a passenger name or free notes are not credentials", async ({ page }) => {
  const { world, nodes } = await boot(page);
  const ok = nodes().filter((n) => n.name === "Passenger name" || n.name === "Notes");
  expect(ok.length).toBe(2);
  expect(ok.every((n) => (n.flags & (FLAG_CREDENTIAL | FLAG_ACCOUNT)) === 0)).toBe(true);
  const email = nodes().find((n) => n.name === "Contact");
  expect((email?.flags ?? 0) & FLAG_ACCOUNT).toBeTruthy();
  expect((email?.flags ?? 0) & FLAG_CREDENTIAL).toBe(0);
  await world.stop();
});

test("nested scopes take the strictest marker and an unknown marker restricts", async ({ page }) => {
  const { world, byId } = await boot(page);
  expect(byId(BOUNDARY.nested)).toBeUndefined();
  const acct = byId(BOUNDARY.account);
  expect((acct?.flags ?? 0) & FLAG_ACCOUNT).toBeTruthy();
  const unknown = byId(BOUNDARY.unknown);
  expect((unknown?.flags ?? 0) & FLAG_ACCOUNT).toBeTruthy();
  await world.stop();
});

test("the credential region yields no text even when an inner element says public", async ({ page }) => {
  const { world, h, nodes } = await boot(page);
  const p = nodes().find((n) => n.tag === "p" && (n.flags & FLAG_CREDENTIAL) !== 0);
  expect(p?.text ?? null).toBeNull();
  expect(wire(h)).not.toContain(BOUNDARY.nested);
  await world.stop();
});

test("an authenticated table keeps its structure and local semantics but exports no record values", async ({
  page,
}) => {
  const { world, nodes, byId, policy, h, ctx } = await boot(page);
  expect(byId(BOUNDARY.invoice)).toBeDefined();
  expect(nodes().filter((n) => n.role === "cell").length).toBe(3 + 1);
  const shared = sharedEvidence(h.collector.replica, ctx, policy(["button", "link", "heading", "columnheader"]));
  const out = JSON.stringify(shared);
  for (const v of [BOUNDARY.invoice, BOUNDARY.customer, BOUNDARY.amount, BOUNDARY.late]) expect(out).not.toContain(v);
  expect(out).toContain("Invoice");
  expect(out).toContain("Customer");
  expect(shared.nodes.filter((n) => n.role === "cell").length).toBe(4);
  expect(shared.nodes.filter((n) => n.role === "columnheader").map((n) => n.name)).toEqual([
    "Invoice",
    "Customer",
    "Amount",
    "Actions",
  ]);
  await world.stop();
});

test("without a valid policy shared evidence is structure only", async ({ page }) => {
  const { world, h, policy, ctx } = await boot(page);
  const none = sharedEvidence(h.collector.replica, ctx);
  expect(none.policy).toEqual({ applied: false, id: null, refusal: "policy.absent" });
  expect(none.nodes.every((n) => n.name === null && n.text === null)).toBe(true);
  expect(none.nodes.length).toBe(h.collector.replica.size);
  const forged = sharedEvidence(h.collector.replica, { ...ctx, origin: "http://evil.example" }, policy(["button"]));
  expect(forged.policy.refusal).toBe("policy.origin");
  expect(forged.nodes.every((n) => n.name === null && n.text === null)).toBe(true);
  const allowed = sharedEvidence(h.collector.replica, ctx, policy(["button", "heading"]));
  expect(allowed.policy.applied).toBe(true);
  expect(allowed.nodes.find((n) => n.name === "Save")?.name).toBe("Save");
  expect(
    allowed.nodes.find((n) => n.role === "heading")?.text ?? allowed.nodes.find((n) => n.role === "heading")?.name,
  ).toBe("Billing");
  await world.stop();
});

test("a marker added later withholds text already captured; removing it restores nothing it should not", async ({
  page,
}) => {
  const { world, h, byId } = await boot(page);
  expect(byId(BOUNDARY.late)).toBeDefined();
  await page.evaluate(() => document.getElementById("dyn")?.setAttribute("data-vsf-scope", "credential"));
  await h.settle();
  expect(byId(BOUNDARY.late)).toBeUndefined();
  expect(JSON.stringify([...h.collector.replica.nodes.values()])).not.toContain(BOUNDARY.late);
  await world.stop();
});

test("changing a control type to password, or its label to a credential word, reclassifies it", async ({ page }) => {
  const { world, h, nodes } = await boot(page);
  await page.evaluate(() => {
    (document.getElementById("ok2") as HTMLInputElement).type = "password";
    (document.getElementById("ok1") as HTMLInputElement).setAttribute("aria-label", "Access token");
  });
  await h.settle();
  const flagged = (name: string) => (nodes().find((n) => n.name === name)?.flags ?? 0) & FLAG_CREDENTIAL;
  expect(flagged("Notes")).toBeTruthy();
  expect(flagged("Access token")).toBeTruthy();
  await world.stop();
});

test("private evidence cannot reach a reusable artifact: the observation body carries counts, never text", async ({
  page,
}) => {
  const { world, h } = await boot(page);
  const body = JSON.stringify(
    observationBody(h.collector.replica, { frames: 0, documents: 1 } as never, {} as never, {
      sourceAppId: "web:billing",
      variantResolution: "unknown",
      runtimeId: "rt_test",
      principalRef: PRINCIPAL,
      capturedAt: NOW,
      staleAfterMs: 1000,
      packDigest: `sha256:${"0".repeat(64)}`,
      packBytes: 1,
      patternsDigest: `sha256:${"1".repeat(64)}`,
    }),
  );
  for (const v of Object.values(BOUNDARY)) expect(body).not.toContain(v);
  await world.stop();
});

test("the one-shot pack path honours the same scopes: marked and credential text is absent, ordinary text stays local", async ({
  page,
}) => {
  const { world } = await boot(page);
  const text = await page.evaluate(() => {
    const O = (window as unknown as { VSFObserver: typeof import("@vsf/observer") }).VSFObserver;
    return JSON.stringify(O.snapshotDocument(document).snapshot);
  });
  for (const v of [BOUNDARY.nested, BOUNDARY.account, BOUNDARY.unknown]) expect(text.includes(v), v).toBe(false);
  expect(text).toContain("Billing");
  expect(text).toContain(BOUNDARY.invoice);
  expect(text, "benign controls keep their names").toContain("Passenger name");
  expect(text.includes("One-time code"), "credential control names are withheld").toBe(false);
  await world.stop();
});
