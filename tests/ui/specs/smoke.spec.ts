import { chromium, expect, test } from "@playwright/test";
import { formBoot, gridBoot } from "../support/config.ts";
import { open, type Site, startSite } from "../support/world.ts";

let site: Site;
test.beforeAll(async () => {
  site = await startSite();
});
test.afterAll(async () => {
  await site.stop();
});

test("form renders with labelled fields and operations", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => m.type() === "error" && errors.push(m.text()));
  await open(page, site, "/form", formBoot());
  await expect(page.getByRole("textbox", { name: "Total" })).toHaveValue("129.95");
  await expect(page.getByRole("button", { name: "Delete order" })).toBeVisible();
  expect(errors).toEqual([]);
  void chromium;
});

test("grid renders", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await open(page, site, "/grid", gridBoot(50));
  await expect(page.getByRole("grid", { name: "Order" })).toBeVisible();
  expect(await page.locator("tbody tr").count()).toBe(50);
  expect(errors).toEqual([]);
});
