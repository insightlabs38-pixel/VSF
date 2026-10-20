import { expect, test } from "@playwright/test";
import { type GalleryServer, startGallery } from "../support/gallery.ts";
import { audit, shiftAfterReady, summarize, systemFontsUsed, watchShifts } from "../support/visual.ts";

let gallery: GalleryServer;
test.beforeAll(async () => {
  gallery = await startGallery();
});
test.afterAll(async () => {
  await gallery.stop();
});

const VIEWPORTS = {
  desktop: { width: 1280, height: 800 },
  tablet: { width: 768, height: 1024 },
  narrow: { width: 390, height: 844 },
} as const;
type Viewport = keyof typeof VIEWPORTS;
const SCHEMES = ["light", "dark"] as const;
const DENSITIES = ["compact", "comfortable"] as const;
const SECTIONS = ["interface", "versions", "permissions", "environments", "storage", "activity"] as const;

interface Case {
  route: string;
  scene: string;
  viewport: Viewport;
  scheme: (typeof SCHEMES)[number];
  density: (typeof DENSITIES)[number];
}

const cases: Case[] = [];
for (const viewport of Object.keys(VIEWPORTS) as Viewport[]) {
  for (const scheme of SCHEMES) {
    for (const density of DENSITIES) {
      for (const scene of ["enterprise-grid", "grouped-form", "workbench-split", "master-detail"]) {
        cases.push({ route: scene, scene, viewport, scheme, density });
      }
      cases.push({ route: "states", scene: "states", viewport, scheme, density });
      cases.push({ route: "shell/interface", scene: "shell-interface", viewport, scheme, density });
    }
    for (const section of SECTIONS.filter((s) => s !== "interface")) {
      cases.push({ route: `shell/${section}`, scene: `shell-${section}`, viewport, scheme, density: "comfortable" });
    }
  }
}

async function load(page: import("@playwright/test").Page, c: Case): Promise<void> {
  await watchShifts(page);
  await page.goto(`${gallery.url}/?scheme=${c.scheme}&density=${c.density}#/${c.route}`);
  await page.waitForSelector("#app[data-ready]");
}

for (const c of cases) {
  const id = `${c.scene}-${c.scheme}-${c.density}-${c.viewport}`;
  test(`gallery ${id}`, async ({ browser }) => {
    const context = await browser.newContext({ viewport: VIEWPORTS[c.viewport], reducedMotion: "reduce" });
    const page = await context.newPage();
    await load(page, c);

    const findings = await audit(page);
    expect(summarize(findings), "layout, clipping, overlap, contrast and naming defects").toBe("");
    expect(await systemFontsUsed(page), "text painted with a font that is not embedded").toEqual([]);
    expect(await shiftAfterReady(page), "layout shift after the scene is ready").toBeLessThan(0.001);

    await expect(page).toHaveScreenshot(`${id}.png`, { fullPage: true, animations: "disabled", caret: "hide" });
    await context.close();
  });
}
