import { expect, type Page, test } from "@playwright/test";
import { formBoot } from "../support/config.ts";
import { type Site, sourcePage, startSite } from "../support/world.ts";

let site: Site;
test.beforeAll(async () => {
  site = await startSite();
});
test.afterAll(async () => {
  await site.stop();
});

const SOURCE = `<main id="src"><h1 id="title">Source page</h1><button id="b">Source button</button><input id="i" value="typed"><p class="note" data-note="kept">note</p></main>`;
const HEAD = `<style id="own">.note{color:rgb(1,2,3)}</style>`;

const ev = <T>(page: Page, fn: string): Promise<T> =>
  page.evaluate(`(() => { const app = window.app; return (${fn})(app); })()`) as Promise<T>;

async function start(page: Page, path: string): Promise<void> {
  site.pages.set(path, sourcePage(SOURCE, HEAD));
  await page.goto(`${site.url}${path}`);
  await page.evaluate((x) => (window as unknown as { VSF: { boot(c: unknown): void } }).VSF.boot(x), formBoot());
}

const sourceDom = (page: Page) => page.evaluate(() => document.getElementById("src")?.outerHTML ?? "");
const sheets = (page: Page) => page.evaluate(() => document.adoptedStyleSheets.length);
const apply = (page: Page, id: string, css: string, origin?: string) =>
  page.evaluate(
    ([i, c, o]) => {
      const w = window as unknown as {
        app: { styles: { apply(o: string, i: string, c: string): { code: string }[] } };
      };
      return w.app.styles.apply((o as string) || location.origin, i as string, c as string).map((p) => p.code);
    },
    [id, css, origin ?? ""],
  );
const color = (page: Page, sel: string) =>
  page.evaluate((s) => getComputedStyle(document.querySelector(s) as Element).color, sel);

test("a per-source style restyles the source page and leaves its DOM and state alone", async ({ page }) => {
  await start(page, "/a");
  const dom = await sourceDom(page);
  await page.locator("#i").fill("changed");
  const codes = await apply(page, "s1", "#title{color:rgb(10,20,30)} .note{color:rgb(200,0,0)}");
  expect(codes).toEqual([]);
  expect(await color(page, "#title")).toBe("rgb(10, 20, 30)");
  expect(await color(page, ".note")).toBe("rgb(200, 0, 0)");
  expect((await sourceDom(page)).replace('value="typed"', "")).toBe(dom.replace('value="typed"', ""));
  expect(await page.locator("#i").inputValue()).toBe("changed");
});

test("switching it off restores the exact previous cascade, the page's own sheets and the page state", async ({
  page,
}) => {
  await start(page, "/b");
  const before = { n: await sheets(page), note: await color(page, ".note"), dom: await sourceDom(page) };
  await page.locator("#i").fill("keep");
  await page.locator("#b").focus();
  await apply(page, "s1", ".note{color:rgb(200,0,0)}");
  expect(await sheets(page)).toBe(before.n + 2);
  await ev(page, "(a) => a.styles.setEnabled(false)");
  expect(await sheets(page)).toBe(before.n);
  expect(await color(page, ".note")).toBe(before.note);
  expect(await sourceDom(page)).toBe(before.dom.replace('value="typed"', 'value="typed"'));
  expect(await page.locator("#i").inputValue()).toBe("keep");
  expect(await page.evaluate(() => document.activeElement?.id)).toBe("b");
  await ev(page, "(a) => a.styles.setEnabled(true)");
  expect(await color(page, ".note")).toBe("rgb(200, 0, 0)");
  await ev(page, "(a) => a.styles.remove('s1')");
  expect(await sheets(page)).toBe(before.n);
  expect(await color(page, ".note")).toBe(before.note);
});

test("source CSS collisions cannot reach the renderer's controls or hide its host", async ({ page }) => {
  await start(page, "/c");
  const before = await page.getByRole("button", { name: "Submit order" }).boundingBox();
  const codes = await apply(
    page,
    "evil",
    `button, input, main { display: none !important; visibility: hidden !important; opacity: 0 !important }
     #vsf-host, [data-vsf-host] { display: none !important; visibility: hidden !important; opacity: 0 !important; transform: scale(0) !important }
     html body div#vsf-host[data-vsf-host] { position: fixed !important; filter: blur(50px) !important; clip-path: inset(100%) !important }`,
  );
  expect(codes).toEqual([]);
  await expect(page.locator("#b")).toBeHidden();
  const button = page.getByRole("button", { name: "Submit order" });
  await expect(button).toBeVisible();
  const after = await button.boundingBox();
  expect(after?.height).toBe(before?.height);
  const hostVisible = await page.evaluate(() => {
    const cs = getComputedStyle(document.getElementById("vsf-host") as Element);
    return [cs.display, cs.visibility, cs.opacity, cs.transform, cs.filter, cs.clipPath];
  });
  expect(hostVisible).toEqual(["block", "visible", "1", "none", "none", "none"]);
  await button.click();
  expect(await ev<number>(page, "(a) => a.dispatches.length")).toBe(1);
});

test("styles for another origin are kept but not applied", async ({ page }) => {
  await start(page, "/o");
  const n = await sheets(page);
  expect(await apply(page, "far", ".note{color:rgb(9,9,9)}", "https://other.example.com")).toEqual([]);
  expect(await sheets(page)).toBe(n);
  expect(await color(page, ".note")).toBe("rgb(1, 2, 3)");
});

const REFUSED: [string, string, string][] = [
  ["a remote image", ".a{background:url(https://evil.example/x.png)}", "style.external_url"],
  ["a data URL that is not an image", ".a{background:url(data:text/html,hi)}", "style.external_url"],
  ["an escaped function name", ".a{background:u\\72l(https://evil.example/x)}", "style.escape"],
  ["an escaped identifier", ".\\61 {color:red}", "style.escape"],
  ["image-set", '.a{background:image-set("https://evil.example/x.png" 1x)}', "style.external_url"],
  ["webkit image-set", '.a{background:-webkit-image-set("https://evil.example/x.png" 1x)}', "style.external_url"],
  ["cross-fade", '.a{background:cross-fade(url(a.png), "https://evil.example/x.png", 50%)}', "style.external_url"],
  ["src()", '.a{background:src("https://evil.example/x.png")}', "style.external_url"],
  ["attr() that can name a URL", ".a{content:attr(data-note type(<url>))}", "style.external_url"],
  ["an unknown function", ".a{width:frobnicate(3px)}", "style.unknown_function"],
  ["@import", '@import "https://evil.example/x.css";', "style.import"],
  ["@font-face", '@font-face{font-family:x;src:url("https://evil.example/f.woff2")}', "style.font_face"],
  ["@namespace", "@namespace url(http://evil.example/ns);", "style.at_rule"],
  ["a legacy script hook", ".a{width:expression(alert(1))}", "style.script_hook"],
  ["an unterminated comment", ".a{color:red} /* never closed", "style.unterminated"],
  ["an unterminated string", '.a{content:"open}', "style.unterminated"],
  ["unbalanced braces", ".a{color:red", "style.unbalanced"],
  ["a control character", ".a{color:red}\u0001", "style.control_character"],
];

for (const [name, css, code] of REFUSED) {
  test(`refused, and nothing attached: ${name}`, async ({ page }) => {
    await start(page, `/r${code}${name.length}`);
    const n = await sheets(page);
    const requests: string[] = [];
    page.on("request", (r) => {
      if (r.url().includes("evil.example")) requests.push(r.url());
    });
    const codes = await apply(page, "bad", css);
    expect(codes).toContain(code);
    expect(await sheets(page)).toBe(n);
    expect(requests).toEqual([]);
  });
}

test("ordinary modern CSS is accepted: colours, math, gradients, transforms, media and layers", async ({ page }) => {
  await start(page, "/ok");
  const css = `@layer base{.note{color:oklch(60% .2 30);margin:calc(1px + var(--x, 2px));background:linear-gradient(to right, rgb(0 0 0 / .1), transparent)}}
    @media (min-width: 10px){#title{transform:translateX(1px) rotate(1deg);width:clamp(1px, 2vw, 9px)}}
    #b:not(:disabled):is(:hover,:focus-visible){outline:2px solid color-mix(in srgb, red 50%, blue)}`;
  expect(await apply(page, "ok", css)).toEqual([]);
});

test("the page's own adopted sheets keep their order and survive every change", async ({ page }) => {
  await start(page, "/own");
  await page.evaluate(() => {
    const own = new CSSStyleSheet();
    own.replaceSync("#title{letter-spacing:3px}");
    document.adoptedStyleSheets = [own];
  });
  await apply(page, "s", ".note{color:rgb(5,5,5)}");
  const order = await page.evaluate(() => document.adoptedStyleSheets.map((s) => s.cssRules[0]?.cssText.slice(0, 20)));
  expect(order[0]).toContain("#title");
  await ev(page, "(a) => a.styles.setEnabled(false)");
  const after = await page.evaluate(() => document.adoptedStyleSheets.map((s) => s.cssRules[0]?.cssText.slice(0, 20)));
  expect(after).toEqual([order[0]]);
});
