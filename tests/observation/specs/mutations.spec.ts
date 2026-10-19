import { expect, test } from "@playwright/test";
import type { Batch } from "@vsf/observer";
import { listPage, richPage } from "../support/pages.ts";
import { attach, startWorld, truthOf } from "../support/world.ts";

type Stats = {
  described: number;
  scannedSiblings: number;
  fullSyncs: number;
  replays: number;
  subtreeSyncs: number;
  batches: number;
};

async function open(rows = 20, opts: Record<string, unknown> = {}) {
  const world = await startWorld();
  world.a.pages.set("/", listPage(rows));
  return { world, opts };
}

async function boot(page: import("@playwright/test").Page, rows = 20, opts: Record<string, unknown> = {}) {
  const { world } = await open(rows);
  const h = await attach(page, { allow: [world.a.url], ...opts });
  await page.goto(`${world.a.url}/`);
  await h.settle();
  const stats = () =>
    page.evaluate(() => ({ ...(window as unknown as { __vsf: { session: { stats: Stats } } }).__vsf.session.stats }));
  return { world, h, stats };
}

const deltas = (h: { collector: { batches: Batch[] } }, fromIndex: number) =>
  h.collector.batches.slice(fromIndex).filter((b) => b.kind === "delta");

test("batches carry a fixed epoch and a sequence that rises by exactly one", async ({ page }) => {
  const { world, h } = await boot(page);
  const start = h.collector.batches.length;
  for (let i = 0; i < 5; i++) {
    await page.evaluate((n) => {
      (document.getElementById("note") as HTMLElement).textContent = `note ${n}`;
    }, i);
    await h.flush();
  }
  const seqs = h.collector.batches.map((b) => b.seq);
  const epochs = new Set(h.collector.batches.map((b) => b.epoch));
  expect(epochs.size).toBe(1);
  expect(seqs.slice(seqs.indexOf(0))).toEqual(seqs.slice(seqs.indexOf(0)).map((_, i) => i));
  expect(h.collector.batches.length - start).toBe(5);
  expect(h.collector.replica.state).toBe("synced");
  await world.stop();
});

test("a burst of changes inside one tick is one batch and an add that is undone emits nothing", async ({ page }) => {
  const { world, h } = await boot(page);
  const start = h.collector.batches.length;
  await page.evaluate(() => {
    const list = document.getElementById("list") as HTMLElement;
    for (let i = 0; i < 200; i++) {
      const li = document.createElement("li");
      li.textContent = `burst ${i}`;
      list.append(li);
    }
    const temp = document.createElement("li");
    temp.textContent = "ephemeral";
    list.append(temp);
    temp.remove();
  });
  await h.flush();
  const made = deltas(h, start);
  expect(made).toHaveLength(1);
  const texts = made[0]?.ops.flatMap((o) => (o.op === "add" ? [o.text] : [])) ?? [];
  expect(texts).toContain("burst 199");
  expect(texts).not.toContain("ephemeral");
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  await world.stop();
});

test("a small change reconciles by touching only that change, not by rescanning the page", async ({ page }) => {
  const { world, h, stats } = await boot(page, 3000);
  const full = await stats();
  await page.evaluate(() => {
    (document.getElementById("note") as HTMLElement).textContent = "changed";
  });
  await h.flush();
  const afterText = await stats();
  const last = h.collector.batches.at(-1);
  expect(last?.ops.map((o) => o.op)).toEqual(["set"]);
  expect(afterText.described - full.described).toBeLessThanOrEqual(2);
  await page.evaluate(() => {
    const li = document.createElement("li");
    li.id = "extra";
    li.textContent = "appended row";
    (document.getElementById("list") as HTMLElement).append(li);
  });
  await h.flush();
  const afterAdd = await stats();
  expect(h.collector.batches.at(-1)?.ops.map((o) => o.op)).toEqual(["add"]);
  expect(afterAdd.described - afterText.described).toBe(1);
  expect(afterAdd.scannedSiblings - afterText.scannedSiblings).toBeLessThanOrEqual(2);
  await page.evaluate(() => {
    (document.getElementById("r1500") as HTMLElement).remove();
  });
  await h.flush();
  const afterRemove = await stats();
  expect(h.collector.batches.at(-1)?.ops.map((o) => o.op)).toEqual(["remove"]);
  expect(afterRemove.described).toBe(afterAdd.described);
  expect(afterRemove.fullSyncs).toBe(1);
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  await world.stop();
});

test("recreated nodes are removed and added again and the replica still equals the page", async ({ page }) => {
  const { world, h } = await boot(page);
  const idsBefore = new Set(h.collector.replica.nodes.keys());
  await page.evaluate(() => {
    const list = document.getElementById("list") as HTMLElement;
    const markup = list.innerHTML;
    list.innerHTML = markup;
  });
  await h.settle();
  const truth = await truthOf(page);
  expect(h.collector.replica.canonical()).toEqual(truth);
  const idsAfter = [...h.collector.replica.nodes.keys()];
  const reused = idsAfter.filter((id) => idsBefore.has(id) && h.collector.replica.nodes.get(id)?.tag === "li");
  expect(reused).toHaveLength(0);
  await world.stop();
});

test("reordering and moving keep the order and the identities consistent", async ({ page }) => {
  const { world, h } = await boot(page);
  await page.evaluate(() => {
    const list = document.getElementById("list") as HTMLElement;
    list.insertBefore(document.getElementById("r10") as HTMLElement, document.getElementById("r2"));
    list.append(document.getElementById("r0") as HTMLElement);
    (document.getElementById("side") as HTMLElement).append(document.getElementById("r5") as HTMLElement);
  });
  await h.settle();
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  expect(h.collector.results.filter((r) => r !== "ok")).toEqual([]);
  await world.stop();
});

test("a lost batch is detected by its sequence gap and repaired by replay without a full rescan", async ({ page }) => {
  const { world, h, stats } = await boot(page);
  const before = await stats();
  h.collector.dropNext = 1;
  await page.evaluate(() => {
    (document.getElementById("note") as HTMLElement).textContent = "first";
  });
  await h.flush();
  await page.evaluate(() => {
    (document.getElementById("title") as HTMLElement).textContent = "second";
  });
  await h.flush();
  expect(h.collector.results.at(-1)).toBe("gap");
  expect(h.collector.replica.state).toBe("gap");
  await h.settle();
  const after = await stats();
  expect(h.collector.replica.state).toBe("synced");
  expect(after.replays).toBe(before.replays + 1);
  expect(after.fullSyncs).toBe(before.fullSyncs);
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  await world.stop();
});

test("when the lost range is no longer buffered the page falls back to one full resync in a new epoch", async ({
  page,
}) => {
  const { world, h, stats } = await boot(page, 20, { limits: { ringBatches: 2 } });
  const epoch = h.collector.replica.epoch;
  h.collector.dropNext = 4;
  for (let i = 0; i < 5; i++) {
    await page.evaluate((n) => {
      (document.getElementById("note") as HTMLElement).textContent = `n${n}`;
    }, i);
    await h.flush();
  }
  expect(h.collector.replica.state).toBe("gap");
  await h.settle();
  const s = await stats();
  expect(h.collector.replica.state).toBe("synced");
  expect(s.fullSyncs).toBe(2);
  expect(h.collector.replica.epoch).toBeGreaterThan(epoch);
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  await world.stop();
});

test("a corrupted batch is rejected as divergence and healed by a full resync", async ({ page }) => {
  const { world, h } = await boot(page);
  const good = h.collector.replica.canonical();
  await page.evaluate(() => {
    (document.getElementById("note") as HTMLElement).textContent = "real change";
  });
  await h.flush();
  const batch = structuredClone(h.collector.batches.at(-1)) as Batch;
  const replica = h.collector.replica;
  await page.evaluate(() => {
    (document.getElementById("note") as HTMLElement).textContent = "later";
  });
  await h.flush();
  expect(replica.canonical()).not.toEqual(good);
  const bad = { ...batch, seq: replica.seq + 1, epoch: replica.epoch, sum: batch.sum + 1 };
  expect(replica.apply(bad).status).toBe("diverged");
  expect(replica.state).toBe("diverged");
  await h.settle();
  expect(replica.state).toBe("synced");
  expect(replica.canonical()).toEqual(await truthOf(page));
  await world.stop();
});

test("a subtree resync rebuilds only that subtree inside the same epoch", async ({ page }) => {
  const { world, h } = await boot(page, 50);
  const epoch = h.collector.replica.epoch;
  const list = [...h.collector.replica.nodes.values()].find((n) => n.tag === "ul");
  const seq = h.collector.replica.seq;
  await h.resync({ kind: "subtree", id: list?.id ?? -1 });
  const b = h.collector.batches.at(-1);
  expect(b?.kind).toBe("delta");
  expect(b?.epoch).toBe(epoch);
  expect(b?.seq).toBe(seq + 1);
  expect(b?.ops.filter((o) => o.op === "remove").length).toBe(50);
  expect(b?.ops.filter((o) => o.op === "add").length).toBeGreaterThan(100);
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  await world.stop();
});

test("replacing the document starts a new epoch instead of patching the old one", async ({ page }) => {
  const { world, h } = await boot(page);
  const epoch = h.collector.replica.epoch;
  await page.evaluate(() => {
    document.open();
    document.write("<!doctype html><html lang='en'><body><p>replaced</p></body></html>");
    document.close();
  });
  await h.settle();
  expect(h.collector.replica.epoch).toBeGreaterThan(epoch);
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  const texts = [...h.collector.replica.nodes.values()].map((n) => n.text);
  expect(texts).toContain("replaced");
  expect(texts).not.toContain("note");
  await world.stop();
});

test("an input limit truncates the baseline visibly instead of silently", async ({ page }) => {
  const { world, h } = await boot(page, 200, { limits: { maxNodes: 50 } });
  expect(h.collector.replica.size).toBeLessThanOrEqual(50);
  expect(h.collector.replica.truncated).toBe(true);
  await world.stop();
});

async function rich(page: import("@playwright/test").Page) {
  const world = await startWorld();
  world.a.pages.set("/", richPage);
  const h = await attach(page, { allow: [world.a.url] });
  await page.goto(`${world.a.url}/`);
  await page.waitForLoadState("load");
  await h.settle();
  return { world, h };
}

test("changes inside shadow roots and same-origin frames are observed and reconciled", async ({ page }) => {
  const { world, h } = await rich(page);
  await page.evaluate(() => {
    const root = (document.getElementById("shadow-host") as HTMLElement).shadowRoot as ShadowRoot;
    (root.getElementById("s1") as HTMLElement).textContent = "shadow changed";
    (root.getElementById("sb") as HTMLElement).setAttribute("aria-label", "renamed shadow button");
    const p = document.createElement("p");
    p.textContent = "added in shadow";
    root.append(p);
    const doc = (document.getElementById("inner") as HTMLIFrameElement).contentDocument as Document;
    const li = doc.createElement("li");
    li.textContent = "b";
    (doc.getElementById("il") as HTMLElement).append(li);
    (doc.querySelector("li") as HTMLElement).textContent = "a changed";
  });
  await h.settle();
  const texts = [...h.collector.replica.nodes.values()].flatMap((n) => [n.text, n.name]);
  for (const want of ["shadow changed", "renamed shadow button", "added in shadow", "b", "a changed"])
    expect(texts).toContain(want);
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  expect(h.collector.results.filter((r) => r !== "ok")).toEqual([]);
  await world.stop();
});

test("removing a host drops its shadow tree and frame document and nothing keeps reporting for them", async ({
  page,
}) => {
  const { world, h } = await rich(page);
  const before = h.collector.replica.size;
  await page.evaluate(() => {
    document.getElementById("shadow-host")?.remove();
    document.getElementById("inner")?.remove();
  });
  await h.settle();
  expect(h.collector.replica.size).toBeLessThan(before - 5);
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  const batches = h.collector.batches.length;
  await page.evaluate(() => {
    const detached = document.createElement("div");
    detached.attachShadow({ mode: "open" }).innerHTML = "<p>never attached</p>";
  });
  await h.settle();
  expect(h.collector.batches.length).toBe(batches);
  await world.stop();
});

test("hiding a container clears visibility below it and showing it restores the same state", async ({ page }) => {
  const { world, h } = await rich(page);
  const visible = () => [...h.collector.replica.nodes.values()].filter((n) => n.tag === "p" && n.flags & 1).length;
  const start = visible();
  expect(start).toBeGreaterThan(1);
  await page.evaluate(() => (document.getElementById("box") as HTMLElement).setAttribute("hidden", ""));
  await h.settle();
  expect(visible()).toBe(start - 2);
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  await page.evaluate(() => (document.getElementById("box") as HTMLElement).removeAttribute("hidden"));
  await h.settle();
  expect(visible()).toBe(start);
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  await world.stop();
});

test("class and style churn that changes nothing observable publishes nothing", async ({ page }) => {
  const { world, h } = await rich(page);
  const batches = h.collector.batches.length;
  await page.evaluate(() => {
    const box = document.getElementById("box") as HTMLElement;
    for (let i = 0; i < 50; i++) {
      box.classList.toggle("hover");
      box.style.color = i % 2 ? "red" : "blue";
    }
  });
  await h.settle();
  expect(h.collector.batches.length).toBe(batches);
  await world.stop();
});

test("direct text-node edits update the owning element", async ({ page }) => {
  const { world, h } = await rich(page);
  await page.evaluate(() => {
    ((document.getElementById("editable") as HTMLElement).firstChild as Text).data = "edited text";
  });
  await h.settle();
  const n = [...h.collector.replica.nodes.values()].find((x) => x.text === "edited text");
  expect(n?.tag).toBe("p");
  expect(h.collector.batches.at(-1)?.ops.map((o) => o.op)).toEqual(["set"]);
  await world.stop();
});

test("a shadow root attached after the element was captured is invisible until rescan, one attached on insertion is not", async ({
  page,
}) => {
  const { world, h } = await rich(page);
  await page.evaluate(() => {
    const el = document.createElement("late-widget");
    el.id = "late";
    document.body.append(el);
  });
  await h.settle();
  await page.evaluate(() => {
    (document.getElementById("late") as HTMLElement).attachShadow({ mode: "open" }).innerHTML =
      "<b>late shadow content</b>";
  });
  await h.settle();
  expect([...h.collector.replica.nodes.values()].some((n) => n.text === "late shadow content")).toBe(false);
  await page.evaluate(() => (window as unknown as { __vsf: { session: { rescan(): void } } }).__vsf.session.rescan());
  await h.settle();
  expect([...h.collector.replica.nodes.values()].some((n) => n.text === "late shadow content")).toBe(true);
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  await page.evaluate(() => {
    const el = document.createElement("sync-widget");
    document.body.append(el);
    el.attachShadow({ mode: "open" }).innerHTML = "<b>attached on insertion</b>";
  });
  await h.settle();
  expect([...h.collector.replica.nodes.values()].some((n) => n.text === "attached on insertion")).toBe(true);
  await world.stop();
});

test("a stopped session publishes nothing more", async ({ page }) => {
  const { world, h } = await rich(page);
  await page.evaluate(() => (window as unknown as { __vsf: { session: { stop(): void } } }).__vsf.session.stop());
  const batches = h.collector.batches.length;
  await page.evaluate(() => {
    document.body.append(document.createElement("hr"));
    (document.getElementById("inner") as HTMLIFrameElement).src = "about:blank";
  });
  await page.waitForTimeout(100);
  await page.evaluate(() => (window as unknown as { __vsf: { session: { flush(): void } } }).__vsf.session.flush());
  await h.drain();
  expect(h.collector.batches.length).toBe(batches);
  await world.stop();
});
