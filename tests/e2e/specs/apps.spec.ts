import { readFileSync } from "node:fs";
import { expect, test } from "@playwright/test";
import { compareGolden, domOutline } from "../support/snapshot.ts";

test.beforeEach(({ page, baseURL }) => {
  page.on("request", (req) => {
    expect(
      req.url().startsWith(baseURL as string) || req.url().startsWith("data:"),
      `external request ${req.url()}`,
    ).toBe(true);
  });
});

async function expectGolden(name: string, page: import("@playwright/test").Page): Promise<void> {
  const first = await domOutline(page);
  const second = await domOutline(page);
  expect(second, "two captures of the same state must be identical").toBe(first);
  const result = compareGolden(name, first);
  expect(result.ok, result.message).toBe(true);
}

test("server-rendered form: invalid then valid submission", async ({ page }) => {
  await page.goto("/forms/");
  await expectGolden("forms-initial", page);
  await page.getByLabel("Name").fill("Ada");
  await page.getByLabel("Email").fill("not-an-email");
  await page.getByLabel("Quantity").fill("0");
  await page.getByRole("button", { name: "Submit order" }).click();
  await expect(page.getByRole("alert")).toContainText("2 problems");
  await expect(page.getByLabel("Email")).toHaveAttribute("aria-invalid", "true");
  await page.getByLabel("Email").fill("ada@example.test");
  await page.getByLabel("Quantity").fill("3");
  await page.getByRole("button", { name: "Submit order" }).click();
  await expect(page.locator("#confirmation")).toHaveText("3 x order for Ada");
});

test("virtualized table renders a bounded window and scrolls deterministically", async ({ page }) => {
  await page.goto("/table/");
  await expect(page.locator(".row").first()).toBeVisible();
  const count = await page.locator(".row").count();
  expect(count).toBeGreaterThan(5);
  expect(count).toBeLessThan(40);
  await expectGolden("table-top", page);
  await page.locator("#viewport").evaluate((el) => {
    el.scrollTop = 5000 * 32;
  });
  await expect(page.locator("#summary")).toContainText("of 10000");
  await expect(page.locator(".row").first()).toContainText(/^\d+/);
  const ids = await page.locator(".row span:first-child").allTextContents();
  expect(Number(ids[0])).toBeGreaterThan(4900);
  expect(Number(ids.at(-1))).toBeLessThan(5200);
  await expectGolden("table-middle", page);
  expect(await page.locator(".row").count()).toBeLessThan(40);
});

test("terminal placeholder accepts keyboard input", async ({ page }) => {
  await page.goto("/terminal/");
  await expectGolden("terminal-initial", page);
  await page.locator("#term").focus();
  await page.keyboard.type("ls");
  await page.keyboard.press("Enter");
  await expect(page.locator("#term")).toContainText("echo: ls");
});

test("sequence editor flags invalid residues and reports stats", async ({ page }) => {
  await page.goto("/sequence/");
  await expect(page.locator("#stats")).toHaveText("Length 10, GC 50%");
  await expectGolden("sequence-initial", page);
  await page.locator("#seq").fill("ACGTXN");
  await expect(page.locator("#stats")).toHaveText("Invalid characters: N X");
  await expect(page.locator("#seq")).toHaveAttribute("aria-invalid", "true");
});

test("WASM smoke runs in the browser and matches the native golden digest", async ({ page }) => {
  await page.goto("/wasm-smoke/");
  await expect(page.locator("#status")).toHaveText("ok");
  const golden = readFileSync(new URL("../../../fixtures/golden/MANIFEST.json", import.meta.url), "utf8");
  const seed42 = (JSON.parse(golden) as { entries: { seed: number; compiled_sha256: string }[] }).entries.find(
    (e) => e.seed === 42,
  );
  await expect(page.locator("#digest")).toHaveText(seed42?.compiled_sha256 ?? "missing");
});

test("injected incompatible ABI makes the browser smoke fail closed", async ({ page }) => {
  await page.goto("/wasm-smoke/?wasm=injected");
  await expect(page.locator("#status")).toHaveText("error:wasm.abi_mismatch");
  await expect(page.locator("#digest")).toHaveText("");
});
