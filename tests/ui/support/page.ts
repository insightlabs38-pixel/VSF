import type { Page } from "@playwright/test";
import type { Boot, Dispatched } from "./page-entry.ts";

type AppWin = Window &
  typeof globalThis & {
    app: {
      renderer: {
        root: ShadowRoot;
        stats: { writes: number; created: number; removed: number; moved: number };
        announcer: { log: { level: string; message: string }[] };
        prefs: object;
        setPreference(p: object): void;
        resetPreferences(): void;
        openPalette(): void;
        closePalette(): void;
        navigate(e: string): void;
      };
      store: {
        set(k: string, v: unknown): void;
        get(k: string): unknown;
        rows(e: string): RowsApi;
        subscriptionCount(): number;
      };
      dispatches: Dispatched[];
      host: HTMLElement;
      setAvailability(a: string, v: { state: string; reason?: string }): void;
      relayout(v: unknown): void;
      retheme(t: unknown): void;
      recompile(o: unknown, v?: unknown): void;
      styles: {
        apply(o: string, id: string, css: string): { code: string }[];
        remove(id: string): void;
        setEnabled(on: boolean): void;
        attached(): unknown[];
      };
      dispose(): void;
    };
  };

interface RowsApi {
  size: number;
  order(): string[];
  setCell(k: string, f: string, v: unknown): boolean;
  insert(k: string, v: Record<string, unknown>, at?: number): void;
  remove(k: string): void;
  replace(rows: unknown[]): void;
  sortBy(f: string, d?: 1 | -1): void;
  cell(k: string, f: string): unknown;
}

export type { AppWin, Boot };

/** Name and identity of whatever has focus inside the renderer, as an assistive technology would read it. */
export async function active(page: Page): Promise<{ tag: string; name: string; id: string | null } | null> {
  return page.evaluate(() => {
    const w = window as unknown as AppWin;
    const a = w.app.renderer.root.activeElement as HTMLElement | null;
    if (!a) return null;
    let name = a.getAttribute("aria-label") ?? "";
    if (!name && a.id)
      name = (a.getRootNode() as ShadowRoot).querySelector?.(`label[for="${a.id}"]`)?.textContent ?? "";
    if (!name) name = (a.textContent ?? "").trim();
    return { tag: a.tagName.toLowerCase(), name, id: a.getAttribute("data-vsf-id") };
  });
}

export async function tabNames(page: Page, n: number, shift = false): Promise<string[]> {
  const out: string[] = [];
  for (let i = 0; i < n; i++) {
    await page.keyboard.press(shift ? "Shift+Tab" : "Tab");
    const a = await active(page);
    out.push(a ? a.name.replace(/\s+/g, " ") : "(outside)");
  }
  return out;
}

export const live = (page: Page, role: "status" | "alert"): Promise<string> =>
  page.evaluate((r) => {
    const w = window as unknown as AppWin;
    return (w.app.renderer.root.querySelector(`[role=${r}]`)?.textContent ?? "").replace(/​/g, "");
  }, role);
