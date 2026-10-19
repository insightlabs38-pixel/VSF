import { expect, test } from "@playwright/test";
import { ACCENTS, contrast, deltaE, PALETTE, parseHex, STATUS_GLYPH, STATUS_NAMES } from "@vsf/design-system";
import { formBoot, semantic } from "../support/config.ts";
import { open, type Site, startSite } from "../support/world.ts";

let site: Site;
test.beforeAll(async () => {
  site = await startSite();
});
test.afterAll(async () => {
  await site.stop();
});

const rgb = (hex: string): string => {
  const [r, g, b] = parseHex(hex);
  return `rgb(${r}, ${g}, ${b})`;
};

const vars = (page: import("@playwright/test").Page, names: string[]): Promise<Record<string, string>> =>
  page.evaluate((ns) => {
    const host = document.querySelector("#vsf-host") as HTMLElement;
    const cs = getComputedStyle(host);
    const probe = document.createElement("span");
    const out: Record<string, string> = {};
    for (const n of ns) {
      probe.style.color = `var(${n})`;
      host.shadowRoot?.append(probe);
      out[n] = getComputedStyle(probe).color;
      probe.remove();
    }
    void cs;
    return out;
  }, names);

for (const scheme of ["light", "dark"] as const) {
  for (const accent of ["none", "blue", "mint"] as const) {
    test(`tokens resolve for ${scheme} / ${accent}`, async ({ page }) => {
      await open(page, site, "/t", formBoot({ theme: { scheme, accent, density: "comfortable" } }));
      const v = await vars(page, [
        "--vsf-bg",
        "--vsf-text",
        "--vsf-accent",
        "--vsf-on-accent",
        "--vsf-danger",
        "--vsf-warning",
        "--vsf-caution",
        "--vsf-success",
      ]);
      expect(v["--vsf-bg"]).toBe(rgb(PALETTE[scheme].bg));
      expect(v["--vsf-text"]).toBe(rgb(PALETTE[scheme].text));
      expect(v["--vsf-accent"]).toBe(rgb(ACCENTS[scheme][accent].accent));
      expect(v["--vsf-on-accent"]).toBe(rgb(ACCENTS[scheme][accent].onAccent));
      for (const n of STATUS_NAMES) expect(v[`--vsf-${n}`]).toBe(rgb(PALETTE[scheme].status[n]));
    });
  }
}

test("system follows the operating system preference and updates live", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await open(page, site, "/sys", formBoot({ theme: { scheme: "system", accent: "mint", density: "comfortable" } }));
  expect((await vars(page, ["--vsf-bg"]))["--vsf-bg"]).toBe(rgb(PALETTE.dark.bg));
  expect((await vars(page, ["--vsf-accent"]))["--vsf-accent"]).toBe(rgb(ACCENTS.dark.mint.accent));
  await page.emulateMedia({ colorScheme: "light" });
  expect((await vars(page, ["--vsf-bg"]))["--vsf-bg"]).toBe(rgb(PALETTE.light.bg));
  expect((await vars(page, ["--vsf-accent"]))["--vsf-accent"]).toBe(rgb(ACCENTS.light.mint.accent));
});

test("scheme, accent and density change independently", async ({ page }) => {
  await open(page, site, "/ind", formBoot({ theme: { scheme: "light", accent: "blue", density: "comfortable" } }));
  const names = ["--vsf-bg", "--vsf-text", "--vsf-accent", "--vsf-danger", "--vsf-target"];
  const before = await vars(page, names.slice(0, 4));
  await page.evaluate(() =>
    (window as never as { app: { renderer: { setPreference(p: object): void } } }).app.renderer.setPreference({
      accent: "mint",
    }),
  );
  const mint = await vars(page, names.slice(0, 4));
  expect(mint["--vsf-accent"]).not.toBe(before["--vsf-accent"]);
  expect(mint["--vsf-bg"]).toBe(before["--vsf-bg"]);
  expect(mint["--vsf-danger"]).toBe(before["--vsf-danger"]);
  await page.evaluate(() =>
    (window as never as { app: { renderer: { setPreference(p: object): void } } }).app.renderer.setPreference({
      density: "compact",
    }),
  );
  const dense = await vars(page, names.slice(0, 4));
  expect(dense).toEqual(mint);
  await page.evaluate(() =>
    (window as never as { app: { renderer: { setPreference(p: object): void } } }).app.renderer.setPreference({
      scheme: "dark",
    }),
  );
  const dark = await vars(page, names.slice(0, 4));
  expect(dark["--vsf-bg"]).not.toBe(mint["--vsf-bg"]);
  expect(dark["--vsf-accent"]).toBe(rgb(ACCENTS.dark.mint.accent));
});

test("rendered status badges use four distinct tokens and a glyph, never colour alone", async ({ page }) => {
  const sem = semantic();
  const order = sem.entities.find((e) => e.id === "order");
  if (!order) throw new Error("fixture");
  (order.fields[0] as { certainty: string }).certainty = "unknown";
  (order.fields[1] as { certainty: string }).certainty = "conflicted";
  await open(
    page,
    site,
    "/badge",
    formBoot({ semantic: sem, theme: { scheme: "light", accent: "blue", density: "comfortable" } }),
  );
  const badges = await page.evaluate(() => {
    const host = document.querySelector("#vsf-host") as HTMLElement;
    return [...(host.shadowRoot?.querySelectorAll(".badge") ?? [])].map((b) => ({
      status: b.getAttribute("data-status"),
      text: b.textContent,
      color: getComputedStyle(b).color,
      bg: getComputedStyle(b).backgroundColor,
    }));
  });
  expect(badges.map((b) => b.status)).toEqual(["caution", "warning"]);
  expect(badges[0]?.text).toContain(STATUS_GLYPH.caution);
  expect(badges[0]?.text).toContain("Unverified");
  expect(badges[1]?.text).toContain(STATUS_GLYPH.warning);
  expect(badges[1]?.color).toBe(rgb(PALETTE.light.status.warning));
  expect(badges[0]?.color).toBe(rgb(PALETTE.light.status.caution));
  expect(badges[0]?.bg).toBe(rgb(PALETTE.light.statusBg.caution));
  expect(deltaE(PALETTE.light.status.caution, PALETTE.light.status.warning)).toBeGreaterThan(25);
  expect(contrast(PALETTE.light.status.warning, PALETTE.light.statusBg.warning)).toBeGreaterThan(4.5);
});

test("destructive operations are red and unavailable ones lose their emphasis", async ({ page }) => {
  await open(
    page,
    site,
    "/danger",
    formBoot({ availability: { submit_order: { state: "unknown", reason: "effect not demonstrated" } } }),
  );
  const s = await page.evaluate(() => {
    const host = document.querySelector("#vsf-host") as HTMLElement;
    const q = (e: string) => host.shadowRoot?.querySelector(`button[data-effect="${e}"]`) as HTMLElement;
    return {
      del: getComputedStyle(q("destructive")).color,
      submit: getComputedStyle(q("non_idempotent_write")).borderStyle,
    };
  });
  expect(s.del).toBe(rgb(PALETTE.light.status.danger));
  expect(s.submit).toBe("dashed");
});
