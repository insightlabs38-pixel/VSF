import { type ChildProcess, spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { chromium } from "@playwright/test";

export interface RawChrome {
  proc: ChildProcess;
  send(method: string, params?: object): Promise<Record<string, unknown>>;
  close(): Promise<void>;
}

/** Launches Chromium with a bare browser-level DevTools socket and no per-target attach, so idle rules apply. */
export async function launchRaw(extDir: string): Promise<RawChrome> {
  const dir = mkdtempSync(join(tmpdir(), "vsf-raw-"));
  const proc = spawn(
    chromium.executablePath(),
    [
      "--headless=new",
      "--no-sandbox",
      "--remote-debugging-port=0",
      `--user-data-dir=${dir}/profile`,
      `--disable-extensions-except=${extDir}`,
      `--load-extension=${extDir}`,
      "about:blank",
    ],
    { stdio: ["ignore", "ignore", "pipe"] },
  );
  const wsUrl = await new Promise<string>((resolve, reject) => {
    let buf = "";
    const timer = setTimeout(() => reject(new Error("no devtools url")), 20_000);
    proc.stderr?.on("data", (d: Buffer) => {
      buf += d.toString();
      const m = /DevTools listening on (ws:\/\/\S+)/.exec(buf);
      if (m?.[1]) {
        clearTimeout(timer);
        resolve(m[1]);
      }
    });
  });
  const ws = new WebSocket(wsUrl);
  await new Promise<void>((resolve, reject) => {
    ws.onopen = () => resolve();
    ws.onerror = () => reject(new Error("ws error"));
  });
  let id = 0;
  const waiting = new Map<number, (v: Record<string, unknown>) => void>();
  ws.onmessage = (ev) => {
    const msg = JSON.parse(String(ev.data)) as { id?: number; result?: Record<string, unknown> };
    if (msg.id !== undefined) waiting.get(msg.id)?.(msg.result ?? {});
  };
  return {
    proc,
    send: (method, params = {}) =>
      new Promise((resolve) => {
        const n = ++id;
        waiting.set(n, resolve);
        ws.send(JSON.stringify({ id: n, method, params }));
      }),
    async close() {
      ws.close();
      const exited = new Promise<void>((resolve) => proc.once("exit", () => resolve()));
      proc.kill("SIGKILL");
      await exited;
      try {
        rmSync(dir, { recursive: true, force: true, maxRetries: 20, retryDelay: 250 });
      } catch {
        // Chrome helper processes may still hold the profile; a leftover temp directory must not fail a passed test.
      }
    },
  };
}
