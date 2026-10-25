import { launch, open } from "./session.ts";
import type { Workflow } from "./spec.ts";

const [app, path, ...rest] = process.argv.slice(2);
const variant = process.env.VARIANT;
const wf = { id: "x", app, ...(variant ? { variant } : {}), start: path } as unknown as Workflow;
const browser = await launch();
const live = await open(browser, wf);
const { page, origin } = live.ctx;
try {
await page.goto(`${origin}${path}`, { waitUntil: "load" });
await page.waitForLoadState("networkidle", { timeout: 6000 }).catch(() => {});
for (const r of rest) {
  const [k, ...v] = r.split("::");
  const arg = v.join("::");
  if (k === "click") await page.getByText(arg, { exact: true }).first().click();
  else if (k === "clicksel") await page.locator(arg).first().click();
  else if (k === "fill") { const [l, val] = arg.split("|"); await page.getByLabel(l, { exact: true }).fill(val); }
  else if (k === "goto") await page.goto(`${origin}${arg}`);
  else if (k === "wait") await page.waitForTimeout(Number(arg));
  else if (k === "role") { const [role, name] = arg.split("|"); await page.getByRole(role as never, { name, exact: true }).first().click(); }
  await page.waitForTimeout(400);
}
console.log(await page.locator("body").ariaSnapshot());
if (process.env.SHOT) await page.screenshot({ path: process.env.SHOT });
} finally {
await live.close();
await browser.close();
}
