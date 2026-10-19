import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { expect, type Page, test } from "@playwright/test";
import { formBoot, gridBoot, semantic } from "../support/config.ts";
import type { Boot } from "../support/page-entry.ts";
import { open, type Site, startSite } from "../support/world.ts";

let site: Site;
test.beforeAll(async () => {
  site = await startSite();
});
test.afterAll(async () => {
  await site.stop();
});

const GOLDEN = new URL("../golden/visual-contract.json", import.meta.url);
const ev = <T>(page: Page, fn: string): Promise<T> =>
  page.evaluate(`(() => { const app = window.app; return (${fn})(app); })()`) as Promise<T>;

function flagged(): Boot["semantic"] {
  const sem = semantic();
  const order = sem.entities.find((e) => e.id === "order");
  if (!order) throw new Error("fixture");
  (order.fields[0] as { certainty: string }).certainty = "unknown";
  (order.fields[1] as { certainty: string }).certainty = "conflicted";
  return sem;
}

interface State {
  name: string;
  boot: (scheme: "light" | "dark") => Boot;
  drive?: (page: Page) => Promise<void>;
}

const theme = (scheme: "light" | "dark") => ({ scheme, accent: "blue" as const, density: "comfortable" as const });

const STATES: State[] = [
  { name: "normal", boot: (s) => formBoot({ theme: theme(s) }) },
  {
    name: "error",
    boot: (s) =>
      formBoot({
        theme: theme(s),
        results: { submit_order: { ok: false, message: "The source rejected the change" } },
      }),
    drive: async (page) => {
      await ev(page, "(a) => a.renderer.root.querySelector('[data-effect=non_idempotent_write]').focus()");
      await page.keyboard.press("Enter");
      await expect
        .poll(() => ev<string>(page, "(a) => a.renderer.root.querySelector('[role=alert]').textContent"))
        .toContain("rejected");
    },
  },
  { name: "empty", boot: (s) => gridBoot(0, { theme: theme(s) }) },
  {
    name: "empty-after-filter",
    boot: (s) => gridBoot(5, { theme: theme(s) }),
    drive: async (page) => {
      await page.getByRole("textbox", { name: "Filter rows" }).fill("zzz");
    },
  },
  {
    name: "unknown",
    boot: (s) =>
      formBoot({
        theme: theme(s),
        semantic: flagged(),
        availability: { submit_order: { state: "unknown", reason: "effect not yet verified" } },
      }),
  },
  {
    name: "conflicted",
    boot: (s) =>
      formBoot({
        theme: theme(s),
        semantic: flagged(),
        availability: { delete_order: { state: "conflicted", reason: "sources disagree" } },
      }),
  },
  {
    name: "disabled",
    boot: (s) =>
      formBoot({
        theme: theme(s),
        availability: {
          open_order: { state: "unusable", reason: "read-only mode" },
          submit_order: { state: "unusable", reason: "locked" },
        },
      }),
  },
];

const PROPS = [
  "color",
  "background-color",
  "border-top-color",
  "border-top-style",
  "border-top-width",
  "border-inline-start-color",
  "border-inline-start-width",
  "opacity",
  "font-weight",
  "cursor",
  "text-decoration-line",
  "display",
  "min-block-size",
];

async function contract(page: Page): Promise<unknown[]> {
  return page.evaluate(
    ({ props }) => {
      const w = window as unknown as { app: { renderer: { root: ShadowRoot } } };
      const root = w.app.renderer.root;
      const out: unknown[] = [];
      const sel = "button, .badge, .alerts, .status-line, .empty, input[type=text], label, h2, th button, .field, .app";
      for (const el of root.querySelectorAll(sel)) {
        if ((el as HTMLElement).closest("tbody") && el.tagName === "BUTTON") continue;
        const cs = getComputedStyle(el);
        const style: Record<string, string> = {};
        for (const p of props) style[p] = cs.getPropertyValue(p);
        out.push({
          el: `${el.tagName.toLowerCase()}${el.className ? `.${el.className}` : ""}${el.getAttribute("data-effect") ? `[${el.getAttribute("data-effect")}]` : ""}${el.getAttribute("data-state") ? `{${el.getAttribute("data-state")}}` : ""}`,
          text: (el.textContent ?? "").replace(/\s+/g, " ").trim().slice(0, 48),
          hidden: (el as HTMLElement).hidden,
          style,
        });
      }
      return out;
    },
    { props: PROPS },
  );
}

const golden: Record<string, unknown[]> = existsSync(GOLDEN) ? JSON.parse(readFileSync(GOLDEN, "utf8")) : {};
const fresh: Record<string, unknown[]> = {};

for (const state of STATES) {
  for (const scheme of ["light", "dark"] as const) {
    test(`visual contract: ${state.name} / ${scheme}`, async ({ page }) => {
      await open(page, site, `/v-${state.name}-${scheme}`, state.boot(scheme));
      await state.drive?.(page);
      const got = await contract(page);
      const key = `${state.name}/${scheme}`;
      fresh[key] = got;
      if (process.env.VSF_UPDATE_GOLDEN === "1") {
        const all = { ...golden, ...fresh };
        mkdirSync(new URL("../golden/", import.meta.url), { recursive: true });
        writeFileSync(
          GOLDEN,
          `${JSON.stringify(Object.fromEntries(Object.entries(all).sort(([a], [b]) => (a < b ? -1 : 1))), null, 1)}\n`,
        );
        return;
      }
      expect(golden[key], `no golden for ${key}; run with VSF_UPDATE_GOLDEN=1`).toBeDefined();
      expect(got).toEqual(golden[key]);
    });
  }
}

test("each state is distinguishable without colour: unavailable states carry text, shape and a reason", async ({
  page,
}) => {
  await open(page, site, "/v-text", STATES[6]?.boot("light") as Boot);
  const rows = await ev<
    { name: string; state: string | null; describedBy: string | null; reason: string; border: string }[]
  >(
    page,
    `(a) => [...a.renderer.root.querySelectorAll('button[data-effect]')].map((b) => ({ name: b.getAttribute('data-vsf-id'), state: b.getAttribute('data-state'), describedBy: b.getAttribute('aria-describedby'), reason: a.renderer.root.getElementById(b.getAttribute('aria-describedby') || 'x')?.textContent || '', border: getComputedStyle(b).borderTopStyle }))`,
  );
  const off = rows.filter((r) => r.state !== "available");
  expect(off.length).toBe(2);
  for (const r of off) {
    expect(r.reason).toContain("Unavailable");
    expect(r.border).toBe("dashed");
  }
  expect(rows.filter((r) => r.state === "available").every((r) => r.border === "solid")).toBe(true);
});

test("normal, error, empty and unknown states have the expected accessible structure", async ({ page }) => {
  await open(page, site, "/v-aria", gridBoot(0));
  await expect(page.locator("#vsf-host")).toMatchAriaSnapshot(`
    - link "Skip to content"
    - status
    - alert
    - main:
      - heading "Order" [level=2]
      - text: Filter rows
      - textbox "Filter rows"
      - grid "Order":
        - rowgroup:
          - row "Order number Total Placed on Actions":
            - columnheader "Order number"
            - columnheader "Total"
            - columnheader "Placed on"
            - columnheader "Actions"
        - rowgroup
      - paragraph: No rows
  `);
});
