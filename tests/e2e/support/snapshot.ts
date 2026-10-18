import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";
import type { Page } from "@playwright/test";
import { redact } from "@vsf/redact";

const goldenDir = fileURLToPath(new URL("../../../fixtures/golden/dom/", import.meta.url));

/** Deterministic outline of the accessible structure under <main>; volatile attributes are excluded. */
export async function domOutline(page: Page): Promise<string> {
  const outline = await page.evaluate(() => {
    const KEEP = [
      "role",
      "aria-label",
      "aria-invalid",
      "aria-rowcount",
      "aria-rowindex",
      "type",
      "name",
      "for",
      "href",
    ];
    const lines: string[] = [];
    const walk = (el: Element, depth: number): void => {
      const attrs = KEEP.filter((a) => el.hasAttribute(a)).map((a) => `${a}=${JSON.stringify(el.getAttribute(a))}`);
      const own = [...el.childNodes]
        .filter((n) => n.nodeType === Node.TEXT_NODE)
        .map((n) => (n.textContent ?? "").trim())
        .filter(Boolean)
        .join(" ");
      lines.push(
        `${"  ".repeat(depth)}${el.tagName.toLowerCase()}${attrs.length ? ` [${attrs.join(" ")}]` : ""}${own ? ` "${own}"` : ""}`,
      );
      for (const child of el.children) walk(child, depth + 1);
    };
    walk(document.querySelector("main") as Element, 0);
    return lines.join("\n");
  });
  return `${redact(outline)}\n`;
}

export function compareGolden(name: string, actual: string): { ok: boolean; message: string } {
  const path = `${goldenDir}${name}.txt`;
  if (process.env.VSF_UPDATE_GOLDEN === "1") {
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, actual);
    return { ok: true, message: `updated ${name}` };
  }
  let expected: string;
  try {
    expected = readFileSync(path, "utf8");
  } catch {
    return { ok: false, message: `missing golden ${name}; run with VSF_UPDATE_GOLDEN=1` };
  }
  return { ok: expected === actual, message: `golden ${name} differs` };
}
