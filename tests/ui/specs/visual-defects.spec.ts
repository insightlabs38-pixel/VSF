import { expect, test } from "@playwright/test";
import { audit, type Kind, shiftAfterReady, watchShifts } from "../support/visual.ts";

const base = `<!doctype html><html lang=en><meta name=viewport content="width=device-width"><style>body{margin:0;font:16px sans-serif;background:#fff;color:#111}</style>`;

const DEFECTS: { name: string; kind: Kind; html: string }[] = [
  {
    name: "a wide element outside every scroll container",
    kind: "overflow",
    html: `${base}<body><div style="width:2400px;height:20px;background:#ccc">wide</div></body>`,
  },
  {
    name: "text cut off by overflow hidden",
    kind: "clipping",
    html: `${base}<body><div style="width:80px;overflow:hidden;white-space:nowrap">a long label that cannot fit in the box</div></body>`,
  },
  {
    name: "text truncated by an ellipsis",
    kind: "clipping",
    html: `${base}<body><div style="width:80px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">a long label that cannot fit</div></body>`,
  },
  {
    name: "two controls drawn over each other",
    kind: "overlap",
    html: `${base}<body><button style="position:absolute;left:10px;top:10px;width:100px;height:40px">One</button><button style="position:absolute;left:60px;top:20px;width:100px;height:40px">Two</button></body>`,
  },
  {
    name: "a control with no accessible name",
    kind: "unlabeled",
    html: `${base}<body><button><svg width=16 height=16 aria-hidden=true><circle cx=8 cy=8 r=6 /></svg></button></body>`,
  },
  {
    name: "an input with no label",
    kind: "unlabeled",
    html: `${base}<body><input type=text></body>`,
  },
  {
    name: "pale grey text on white",
    kind: "contrast",
    html: `${base}<body><p style="color:#bbb">Quarterly totals</p></body>`,
  },
  {
    name: "text on a translucent layer that lowers its contrast",
    kind: "contrast",
    html: `${base}<body><div style="background:#fff"><p style="color:#fff;background:rgba(0,0,0,.12)">Hidden in plain sight</p></div></body>`,
  },
];

const CLEAN = `${base}<body><h1>Orders</h1><label>Search <input type=text></label><button>Open</button><button aria-label="Close"><span aria-hidden=true>x</span></button><p style="color:#333">Readable text.</p><div style="width:120px;overflow:auto"><div style="width:400px">scrolls</div></div></body>`;

for (const d of DEFECTS) {
  test(`the audit catches ${d.name}`, async ({ page }) => {
    await page.setContent(d.html);
    const found = await audit(page);
    expect(found.map((f) => f.kind)).toContain(d.kind);
  });
}

test("the audit stays silent on a correct page and honours scroll containers", async ({ page }) => {
  await page.setContent(CLEAN);
  expect(await audit(page)).toEqual([]);
});

test("the audit looks inside open shadow roots", async ({ page }) => {
  await page.setContent(`${base}<body><div id=h></div></body>`);
  await page.evaluate(() => {
    const r = (document.getElementById("h") as HTMLElement).attachShadow({ mode: "open" });
    r.innerHTML = `<button><svg width=8 height=8></svg></button><p style="color:#ccc">faint</p>`;
  });
  const kinds = (await audit(page)).map((f) => f.kind);
  expect(kinds).toContain("unlabeled");
  expect(kinds).toContain("contrast");
});

test("a late insertion that pushes content is measured as a layout shift", async ({ page }) => {
  await watchShifts(page);
  const html = `${base}<body><p>Content that moves</p><div id=slot></div><p style="font-size:40px">More content below the insertion</p></body>`;
  await page.goto(`data:text/html,${encodeURIComponent(html)}`);
  await page.waitForTimeout(100);
  const quiet = await shiftAfterReady(page, 200);
  expect(quiet).toBeLessThan(0.001);
  await page.evaluate(() => {
    setTimeout(() => {
      const d = document.createElement("div");
      d.style.cssText = "height:300px;background:#eee";
      document.getElementById("slot")?.append(d);
    }, 50);
  });
  const moved = await shiftAfterReady(page, 400);
  expect(moved).toBeGreaterThan(0.005);
});

test("a control clipped away by its scroll container is not an overlap with its neighbour", async ({ page }) => {
  await page.setContent(
    `${base}<body><div style="position:relative;width:100px;height:40px;overflow:auto"><div style="width:300px"><button style="margin-left:150px">Far</button></div></div><button style="position:absolute;left:150px;top:0">Neighbour</button></body>`,
  );
  expect((await audit(page)).filter((f) => f.kind === "overlap")).toEqual([]);
});
