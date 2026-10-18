interface Attempt {
  name: string;
  outcome: string;
}

const attempts: Attempt[] = [];
const extId = new URLSearchParams(location.search).get("ext") ?? "";

function record(name: string, outcome: string): void {
  attempts.push({ name, outcome });
}

window.postMessage({ vsf: "grant", origin: location.origin }, "*");
record("window.postMessage grant", "sent");
document.dispatchEvent(new CustomEvent("vsf-command", { detail: { type: "grant", origin: location.origin } }));
record("CustomEvent vsf-command", "dispatched");
const hostChrome = (globalThis as { chrome?: { runtime?: { sendMessage?: unknown } } }).chrome;
record("chrome.runtime.sendMessage available", String(typeof hostChrome?.runtime?.sendMessage === "function"));
if (typeof hostChrome?.runtime?.sendMessage === "function") {
  try {
    (hostChrome.runtime.sendMessage as (id: string, m: unknown) => void)(extId, {
      type: "grant",
      origin: location.origin,
    });
    record("chrome.runtime.sendMessage(extId)", "called");
  } catch (e) {
    record("chrome.runtime.sendMessage(extId)", `threw ${(e as Error).name}`);
  }
}
await fetch(`chrome-extension://${extId}/panel.html`).then(
  () => record("fetch chrome-extension panel", "resolved"),
  (e: unknown) => record("fetch chrome-extension panel", `rejected ${(e as Error).name}`),
);
await fetch(`chrome-extension://${extId}/manifest.json`).then(
  () => record("fetch chrome-extension manifest", "resolved"),
  (e: unknown) => record("fetch chrome-extension manifest", `rejected ${(e as Error).name}`),
);
(document.getElementById("log") as HTMLElement).textContent = JSON.stringify(attempts);
document.documentElement.dataset.attempts = "done";

export {};
