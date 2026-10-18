import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, test } from "@playwright/test";
import { buildExtension } from "@vsf/extension/build";
import { launchRaw } from "../support/rawchrome.ts";

interface Target {
  type: string;
  url: string;
}

// biome-ignore lint/correctness/noEmptyPattern: Playwright requires a destructuring first argument
test("an idle extension worker is terminated by the browser at about 30 s", async ({}, info) => {
  test.setTimeout(150_000);
  const extDir = await buildExtension(
    { name: "test-idle", hostPermissions: [] },
    join(mkdtempSync(join(tmpdir(), "vsf-idle-")), "ext"),
  );
  const chrome = await launchRaw(extDir);
  try {
    const workers = async (): Promise<string[]> => {
      const r = (await chrome.send("Target.getTargets")) as { targetInfos: Target[] };
      return r.targetInfos
        .filter((t) => t.type === "service_worker" && t.url.startsWith("chrome-extension://"))
        .map((t) => t.url);
    };
    const t0 = Date.now();
    let started = false;
    while (Date.now() - t0 < 15_000 && !started) {
      started = (await workers()).length > 0;
      if (!started) await new Promise((r) => setTimeout(r, 200));
    }
    expect(started, "extension worker should start on load").toBe(true);
    const upAt = Date.now();
    let gone = false;
    while (Date.now() - upAt < 120_000 && !gone) {
      await new Promise((r) => setTimeout(r, 1000));
      gone = (await workers()).length === 0;
    }
    const idleSeconds = (Date.now() - upAt) / 1000;
    info.annotations.push({
      type: "observed",
      description: `natural idle termination: ${gone ? "yes" : "no"} after ${idleSeconds.toFixed(1)} s`,
    });
    expect(gone).toBe(true);
    expect(idleSeconds).toBeGreaterThan(20);
    expect(idleSeconds).toBeLessThan(90);
  } finally {
    await chrome.close();
  }
});
