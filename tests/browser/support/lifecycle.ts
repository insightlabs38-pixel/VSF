import type { Page } from "@playwright/test";

/** Stops every running extension service worker the way the browser does on idle termination. */
export async function stopWorkers(page: Page): Promise<number> {
  const session = await page.context().newCDPSession(page);
  const status = new Map<string, string>();
  session.on("ServiceWorker.workerVersionUpdated", (e) => {
    for (const v of (e as { versions: { versionId: string; runningStatus: string }[] }).versions)
      status.set(v.versionId, v.runningStatus);
  });
  await session.send("ServiceWorker.enable");
  await new Promise((r) => setTimeout(r, 300));
  const running = [...status].filter(([, s]) => s === "running").map(([id]) => id);
  for (const versionId of running) await session.send("ServiceWorker.stopWorker", { versionId });
  const deadline = Date.now() + 10_000;
  while (running.some((id) => status.get(id) !== "stopped") && Date.now() < deadline)
    await new Promise((r) => setTimeout(r, 100));
  const stopped = running.filter((id) => status.get(id) === "stopped").length;
  await session.detach();
  return stopped;
}
