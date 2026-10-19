import { expect, test } from "@playwright/test";
import { FLAG_ACCOUNT, FLAG_CREDENTIAL, FLAG_REDACTED, sharedEvidence } from "@vsf/observer";
import { SECRETS, secretsPage } from "../support/pages.ts";
import { attach, startWorld } from "../support/world.ts";

async function boot(page: import("@playwright/test").Page) {
  const world = await startWorld();
  world.a.pages.set("/", secretsPage);
  await page.addInitScript(() => {
    const w = window as unknown as { __egress: string[] };
    w.__egress = [];
    const note = (what: string) => w.__egress.push(what);
    const f = window.fetch;
    window.fetch = (...a: Parameters<typeof f>) => (note("fetch"), f(...a));
    const o = XMLHttpRequest.prototype.open;
    XMLHttpRequest.prototype.open = function (this: XMLHttpRequest, ...a: unknown[]) {
      note("xhr");
      return (o as (...x: unknown[]) => void).apply(this, a);
    };
    navigator.sendBeacon = () => (note("beacon"), false);
  });
  const requests: string[] = [];
  page.on("request", (r) => requests.push(r.url()));
  const h = await attach(page, { allow: [world.a.url] });
  await page.goto(`${world.a.url}/`);
  await h.settle();
  return { world, h, requests };
}

const wire = (h: { collector: { batches: unknown[] } }) => JSON.stringify(h.collector.batches);

test("credentials never enter the observation, whether typed, labelled or in a credential region", async ({ page }) => {
  const { world, h } = await boot(page);
  await page.fill("#pw", SECRETS.password);
  await page.fill("#tk", SECRETS.apiToken);
  await page.fill("#cc", SECRETS.card);
  await h.settle();
  const w = wire(h);
  for (const secret of [SECRETS.password, SECRETS.apiToken, SECRETS.passphrase]) expect(w).not.toContain(secret);
  const nodes = [...h.collector.replica.nodes.values()];
  const pw = nodes.find((n) => n.tag === "input" && n.name === "Password");
  expect((pw?.flags ?? 0) & FLAG_CREDENTIAL).toBeTruthy();
  const flagged = nodes.filter((n) => n.flags & FLAG_CREDENTIAL);
  expect(flagged.length).toBeGreaterThanOrEqual(5);
  expect(flagged.every((n) => n.text === null)).toBe(true);
  expect(flagged.filter((n) => n.tag !== "input").every((n) => n.name === null)).toBe(true);
  await world.stop();
});

test("secret-shaped text is redacted at capture and again when it appears later", async ({ page }) => {
  const { world, h } = await boot(page);
  const log = [...h.collector.replica.nodes.values()].find((n) => n.tag === "pre");
  expect(log?.text).toContain("[REDACTED:aws_access_key]");
  expect(log?.text).not.toContain(SECRETS.aws);
  expect((log?.flags ?? 0) & FLAG_REDACTED).toBeTruthy();
  await page.evaluate(() => {
    (document.getElementById("plain") as HTMLElement).textContent = "password=hunter2hunter2 and more";
  });
  await h.settle();
  const w = wire(h);
  expect(w).not.toContain("hunter2hunter2");
  expect(w).toContain("[REDACTED:keyed_secret]");
  expect(w).not.toContain(SECRETS.bearer.split(" ")[1] as string);
  await world.stop();
});

test("account text stays local, is flagged, and is withheld from shared evidence", async ({ page }) => {
  const { world, h } = await boot(page);
  const nodes = [...h.collector.replica.nodes.values()];
  const find = (needle: string) => nodes.find((n) => n.text?.includes(needle));
  for (const needle of [SECRETS.name, SECRETS.balance, SECRETS.email, SECRETS.card, SECRETS.phone]) {
    const n = find(needle);
    expect(n, needle).toBeDefined();
    expect((n?.flags ?? 0) & FLAG_ACCOUNT, needle).toBeTruthy();
  }
  const plain = find("Plain public sentence");
  expect((plain?.flags ?? 0) & (FLAG_ACCOUNT | FLAG_CREDENTIAL)).toBe(0);
  const shared = sharedEvidence(h.collector.replica);
  const out = JSON.stringify(shared);
  for (const secret of Object.values(SECRETS)) expect(out).not.toContain(secret);
  expect(out).toContain("Plain public sentence");
  expect(shared.withheld).toBeGreaterThanOrEqual(10);
  expect(
    shared.nodes.every((n) => (n.flags & (FLAG_ACCOUNT | FLAG_CREDENTIAL) ? n.text === null && n.name === null : true)),
  ).toBe(true);
  await world.stop();
});

test("a scope marker on a shadow host covers the shadow tree", async ({ page }) => {
  const { world, h } = await boot(page);
  const inner = [...h.collector.replica.nodes.values()].find(
    (n) => n.tag === "p" && n.flags & FLAG_CREDENTIAL && n.parent !== -1,
  );
  expect(inner).toBeDefined();
  expect(wire(h)).not.toContain("inside shadow");
  await world.stop();
});

test("diagnostics hold codes and numbers only and never page text", async ({ page }) => {
  const { world, h } = await boot(page);
  await page.evaluate(() => document.getElementById("who")?.setAttribute("data-x", "y"));
  await h.settle();
  const events = await page.evaluate(
    () => (window as unknown as { __vsf: { diag: { events: unknown[] } } }).__vsf.diag.events,
  );
  const dump = JSON.stringify(events);
  for (const secret of Object.values(SECRETS)) expect(dump).not.toContain(secret);
  const refused = await page.evaluate(() => {
    const O = (window as unknown as { VSFObserver: typeof import("@vsf/observer") }).VSFObserver;
    const d = new O.Diagnostics();
    const tries: string[] = [];
    for (const f of [
      () => d.emit("ok.code", { n: 1 }),
      () => d.emit("Bad Code"),
      () => d.emit("x", { s: "text" as unknown as number }),
    ]) {
      try {
        f();
        tries.push("accepted");
      } catch {
        tries.push("refused");
      }
    }
    return tries;
  });
  expect(refused).toEqual(["accepted", "refused", "refused"]);
  await world.stop();
});

test("local mode sends nothing anywhere: no request leaves the page's own origin", async ({ page }) => {
  const { world, h, requests } = await boot(page);
  await page.fill("#free", "typed note");
  await page.evaluate(() => {
    const p = document.createElement("p");
    p.textContent = `new ${"x".repeat(50)}`;
    document.body.append(p);
  });
  await h.settle();
  const egress = await page.evaluate(() => (window as unknown as { __egress: string[] }).__egress);
  expect(egress).toEqual([]);
  expect(requests.every((u) => u.startsWith(world.a.url))).toBe(true);
  expect(world.b.hits).toEqual([]);
  await world.stop();
});
