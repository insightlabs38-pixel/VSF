import type { Page } from "@playwright/test";
import type { Env } from "./env.ts";

/** The source document without anything Varifold added or toggled. */
export const sourceHtml = (page: Page): Promise<string> =>
  page.evaluate(() => {
    const clone = document.documentElement.cloneNode(true) as HTMLElement;
    clone.querySelector("[data-vsf-overlay]")?.remove();
    const body = clone.querySelector("body");
    body?.removeAttribute("inert");
    body?.removeAttribute("aria-hidden");
    return clone.outerHTML;
  });

export const overlays = (page: Page): Promise<number> =>
  page.evaluate(() => document.querySelectorAll("[data-vsf-overlay]").length);

export interface StatusReply {
  ok: boolean;
  grants: string[];
  stats: { captures: number; denied: number; transforms: number; executeCalls: Record<string, number> };
  sessions: {
    id: string;
    tabId: number;
    origin: string;
    phase: string;
    lastSeq: number;
    bridge?: unknown;
    ops: { id: string; kind: string; state: string }[];
  }[];
}

export const status = async (env: Env): Promise<StatusReply> =>
  (await env.send({ type: "status" })) as unknown as StatusReply;

/** What a script running in the extension's isolated world of a tab can say to the worker. */
export async function sendFromTab(env: Env, tabId: number, message: unknown): Promise<Record<string, unknown>> {
  const worker = await env.worker();
  return worker.evaluate(
    async ({ id, m }) => {
      const [r] = await chrome.scripting.executeScript({
        target: { tabId: id },
        func: (x: unknown) => chrome.runtime.sendMessage(x),
        args: [m],
      });
      return r?.result as Record<string, unknown>;
    },
    { id: tabId, m: message },
  );
}

export const envelope = (
  kind: string,
  session: string,
  seq: number,
  body: object = {},
  id: string = crypto.randomUUID(),
) => ({
  proto: "vsf-ext-v1",
  kind,
  id,
  session,
  seq,
  body,
});
