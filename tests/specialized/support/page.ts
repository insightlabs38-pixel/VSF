import { appendFileSync, mkdirSync } from "node:fs";
import type { Page } from "@playwright/test";
import type { EditorAdapter, TerminalAdapter } from "@vsf/specialized";
import { adapterPage, type World } from "./world.ts";

declare global {
  interface Window {
    term?: TerminalAdapter;
    editor?: EditorAdapter;
    inputs: string[];
    changes: { value: string; userEdit: boolean }[];
  }
}

export async function ready(page: Page, w: World, path = "/adapters"): Promise<void> {
  w.pages.set(path, adapterPage);
  await page.goto(`${w.url}${path}`);
  await page.waitForFunction(() => "vsf" in window);
}

export async function openTerminal(page: Page, options: { cols?: number; rows?: number; scrollback?: number } = {}) {
  await page.evaluate(async (o) => {
    window.inputs = [];
    const host = document.getElementById("host") as HTMLElement;
    const t = await window.vsf.openAdapter("terminal", host, o);
    t.onInput((d) => window.inputs.push(d));
    t.fit();
    window.term = t;
  }, options);
}

export async function openEditor(page: Page, options: { value?: string; readOnly?: boolean } = {}) {
  await page.evaluate(async (o) => {
    window.changes = [];
    const host = document.getElementById("host") as HTMLElement;
    const e = await window.vsf.openAdapter("editor", host, o);
    e.onChange((value, userEdit) => window.changes.push({ value, userEdit }));
    window.editor = e;
  }, options);
}

export async function heapBytes(page: Page): Promise<number> {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("HeapProfiler.collectGarbage");
  await cdp.send("Performance.enable");
  const { metrics } = await cdp.send("Performance.getMetrics");
  await cdp.detach();
  return metrics.find((m) => m.name === "JSHeapUsedSize")?.value ?? 0;
}

/** Measured values go to a file so a run's numbers can be kept as evidence. */
export function record(name: string, values: Record<string, number>): void {
  const dir = new URL("../../../artifacts/logs/", import.meta.url);
  mkdirSync(dir, { recursive: true });
  appendFileSync(new URL("specialized-measurements.jsonl", dir), `${JSON.stringify({ name, ...values })}\n`);
}
