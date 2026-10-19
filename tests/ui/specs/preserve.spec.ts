import { expect, type Page, test } from "@playwright/test";
import { formBoot } from "../support/config.ts";
import type { Boot } from "../support/page-entry.ts";
import { type Site, sourcePage, startSite } from "../support/world.ts";

let site: Site;
test.beforeAll(async () => {
  site = await startSite();
});
test.afterAll(async () => {
  await site.stop();
});

const ev = <T>(page: Page, fn: string): Promise<T> =>
  page.evaluate(`(() => { const app = window.app; return (${fn})(app); })()`) as Promise<T>;

const SOURCE = `<section id="wrap">before<div id="rte" contenteditable="true" data-kind="unknown-editor">hello</div><input id="plain" value="keep me"><!-- note -->after</section><script>
window.clicks = 0; document.getElementById('rte').addEventListener('click', () => window.clicks++);
</script>`;

const wrapHtml = (page: Page) => page.evaluate(() => document.getElementById("wrap")?.outerHTML ?? "");

type Perm = { inPlace: boolean; sideBySide: boolean };

function cfg(perm: Perm, over: Partial<Boot["surfaces"] & object> = {}, origin = ""): Boot {
  return formBoot({
    views: [{ entity: "order", layout: "form", preserved: [{ id: "rte", name: "Rich editor" }] }],
    surfaces: {
      surf_rte: { name: "Rich editor", origin: origin || site.url, selector: "#rte", permitted: perm },
      ...over,
    },
  });
}

async function start(page: Page, path: string, c: Boot, source = SOURCE): Promise<string> {
  site.pages.set(path, sourcePage(source));
  await page.goto(`${site.url}${path}`);
  const before = await wrapHtml(page);
  await page.evaluate((x) => (window as unknown as { VSF: { boot(c: unknown): void } }).VSF.boot(x), c);
  return before;
}

const noEmbeds = (page: Page) =>
  page.evaluate(() => {
    const w = window as unknown as { app: { renderer: { root: ShadowRoot } } };
    return [
      w.app.renderer.root.querySelectorAll("iframe,frame,embed,object,portal").length,
      document.querySelectorAll("iframe,frame,embed,object,portal").length,
    ];
  });

test("in place: the very same node is shown, stays editable and keeps its listeners, then returns exactly", async ({
  page,
}) => {
  const before = await start(page, "/a", cfg({ inPlace: true, sideBySide: true }));
  await page.evaluate(() => {
    (document.getElementById("rte") as unknown as { __id: number }).__id = 42;
  });
  expect(
    await ev<string>(page, "(a) => a.renderer.root.querySelector('section.surface').getAttribute('data-mode')"),
  ).toBe("in_place");
  const moved = await page.evaluate(() => {
    const el = document.getElementById("rte") as HTMLElement & { __id: number };
    return {
      id: el.__id,
      parent: el.parentElement?.getAttribute("data-vsf-surface"),
      inWrap: !!document.querySelector("#wrap #rte"),
    };
  });
  expect(moved).toEqual({ id: 42, parent: "surf_rte", inWrap: false });
  await page.locator("#rte").click();
  await page.keyboard.type(" world");
  expect(await page.locator("#rte").innerText()).toBe("hello world");
  expect(await page.evaluate(() => (window as unknown as { clicks: number }).clicks)).toBe(1);
  expect(await noEmbeds(page)).toEqual([0, 0]);
  await ev(page, "(a) => a.relayout([{ entity: 'order', layout: 'form' }])");
  const after = await wrapHtml(page);
  expect(after).toBe(before.replace(">hello<", ">hello world<"));
  expect(after).not.toContain("vsf-original-position");
  expect(await page.evaluate(() => (document.getElementById("rte") as unknown as { __id: number }).__id)).toBe(42);
  await page.locator("#rte").click();
  expect(await page.evaluate(() => (window as unknown as { clicks: number }).clicks)).toBe(2);
  expect(await page.locator("#plain").inputValue()).toBe("keep me");
});

test("side by side: the original stays where it is; the view can find it and then lets go", async ({ page }) => {
  const before = await start(page, "/b", cfg({ inPlace: false, sideBySide: true }));
  expect(await wrapHtml(page)).toBe(before);
  const mode = await ev<string>(
    page,
    "(a) => a.renderer.root.querySelector('section.surface').getAttribute('data-mode')",
  );
  expect(mode).toBe("side_by_side");
  const style = await page.evaluate(() => document.querySelector("#vsf-host")?.getAttribute("style") ?? "");
  expect(style).toContain("position: fixed");
  await ev(
    page,
    "(a) => [...a.renderer.root.querySelectorAll('button')].find((b) => b.textContent === 'Locate original').click()",
  );
  expect(await page.evaluate(() => document.activeElement?.id)).toBe("rte");
  await ev(page, "(a) => a.relayout([{ entity: 'order', layout: 'form' }])");
  expect(await page.evaluate(() => document.querySelector("#vsf-host")?.getAttribute("style"))).toBeNull();
  expect(await wrapHtml(page)).toBe(before);
});

test("an unsupported widget the source does not permit stays usable in its own page with the limits stated", async ({
  page,
}) => {
  const before = await start(page, "/c", cfg({ inPlace: false, sideBySide: false }));
  expect(
    await ev<string>(page, "(a) => a.renderer.root.querySelector('section.surface').getAttribute('data-mode')"),
  ).toBe("limited");
  const text = await ev<string>(page, "(a) => a.renderer.root.querySelector('section.surface').textContent");
  expect(text).toContain("not permitted");
  expect(await wrapHtml(page)).toBe(before);
  await page.locator("#rte").click();
  await page.keyboard.type("!");
  expect(await page.locator("#rte").innerText()).toBe("hello!");
  expect(await noEmbeds(page)).toEqual([0, 0]);
});

test("another origin is never embedded: no frame, no move, and the link opens it on its own terms", async ({
  page,
}) => {
  const before = await start(page, "/o", cfg({ inPlace: true, sideBySide: true }, {}, "https://other.example.com"));
  expect(
    await ev<string>(page, "(a) => a.renderer.root.querySelector('section.surface').getAttribute('data-mode')"),
  ).toBe("limited");
  expect(await wrapHtml(page)).toBe(before);
  expect(await noEmbeds(page)).toEqual([0, 0]);
  const link = await ev<{ href: string | null; rel: string | null; target: string | null }>(
    page,
    "(a) => { const l = a.renderer.root.querySelector('section.surface a'); return { href: l.getAttribute('href'), rel: l.getAttribute('rel'), target: l.getAttribute('target') }; }",
  );
  expect(link).toEqual({ href: "https://other.example.com", rel: "noopener noreferrer", target: "_blank" });
  const text = await ev<string>(page, "(a) => a.renderer.root.querySelector('section.surface').textContent");
  expect(text).toContain("https://other.example.com");
});

test("a surface with no element, or an element already shown elsewhere, is limited and nothing is moved", async ({
  page,
}) => {
  const c = cfg({ inPlace: true, sideBySide: true });
  c.views = [
    {
      entity: "order",
      layout: "form",
      preserved: [
        { id: "rte", name: "Rich editor" },
        { id: "twin", name: "Same editor again" },
        { id: "ghost", name: "Missing" },
      ],
    },
  ];
  c.surfaces = {
    surf_rte: {
      name: "Rich editor",
      origin: site.url,
      selector: "#rte",
      permitted: { inPlace: true, sideBySide: true },
    },
    surf_twin: {
      name: "Same editor again",
      origin: site.url,
      selector: "#rte",
      permitted: { inPlace: true, sideBySide: true },
    },
    surf_ghost: {
      name: "Missing",
      origin: site.url,
      selector: "#nope",
      permitted: { inPlace: true, sideBySide: true },
    },
  };
  await start(page, "/t", c);
  const modes = await ev<Record<string, string>>(
    page,
    "(a) => Object.fromEntries([...a.renderer.root.querySelectorAll('section.surface')].map((s) => [s.getAttribute('data-vsf-id'), s.getAttribute('data-mode')]))",
  );
  expect(modes).toEqual({
    "surface:surf_rte": "in_place",
    "surface:surf_twin": "limited",
    "surface:surf_ghost": "limited",
  });
  expect(await page.evaluate(() => document.querySelectorAll("#rte").length)).toBe(1);
});

test("destroying the renderer returns every adopted widget to its exact original place", async ({ page }) => {
  const before = await start(page, "/z", cfg({ inPlace: true, sideBySide: true }));
  await ev(page, "(a) => a.dispose()");
  expect(await wrapHtml(page)).toBe(before);
  expect(await page.evaluate(() => document.querySelector("#vsf-host")?.getAttribute("style"))).toBeNull();
});

test("if the source removes the widget's old surroundings while it is shown, letting go leaves no stray node", async ({
  page,
}) => {
  await start(page, "/r", cfg({ inPlace: true, sideBySide: true }));
  await page.evaluate(() => document.getElementById("wrap")?.remove());
  await ev(page, "(a) => a.relayout([{ entity: 'order', layout: 'form' }])");
  expect(await page.evaluate(() => document.querySelectorAll("#rte, [data-vsf-surface]").length)).toBe(0);
  expect(await ev<number>(page, "(a) => a.renderer.root.querySelectorAll('section.surface').length")).toBe(0);
});
