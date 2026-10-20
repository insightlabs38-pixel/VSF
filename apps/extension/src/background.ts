import { type Fabric, loadFabric } from "@vsf/wasm-host";
import { createCore } from "./core.ts";
import { chromeKV } from "./kv.ts";
import { BridgeLink } from "./link.ts";
import { indexedDbKeyStore, Vault } from "./vault.ts";

let fabric: Promise<Fabric> | undefined;

function getFabric(): Promise<Fabric> {
  fabric ??= fetch(chrome.runtime.getURL("vsf_wasm.wasm"))
    .then((r) => r.arrayBuffer())
    .then((bytes) => loadFabric(bytes));
  return fabric;
}

const vault = new Vault(indexedDbKeyStore(), chromeKV(chrome.storage.local));
const link = new BridgeLink({
  vault,
  origin: new URL(chrome.runtime.getURL("/")).origin,
  fetch: (url, init) => fetch(url, init),
  now: () => Date.now(),
});

const core = createCore({
  link,
  selfId: chrome.runtime.id,
  version: chrome.runtime.getManifest().version,
  panelUrl: chrome.runtime.getURL("panel.html"),
  local: chromeKV(chrome.storage.local),
  session: chromeKV(chrome.storage.session),
  async tab(tabId) {
    try {
      return await chrome.tabs.get(tabId);
    } catch {
      return null;
    }
  },
  permissionHeld: (pattern) => chrome.permissions.contains({ origins: [pattern] }),
  removePermission: (pattern) => chrome.permissions.remove({ origins: [pattern] }),
  async inject(tabId, files) {
    const results = await chrome.scripting.executeScript({ target: { tabId }, files });
    return results[0]?.result;
  },
  async call(tabId, fn, args) {
    const results = await chrome.scripting.executeScript({
      target: { tabId },
      func: (name: string, a: unknown[]) =>
        (globalThis as unknown as Record<string, (...x: unknown[]) => unknown>)[name]?.(...a),
      args: [fn, args],
    });
    const first = results[0];
    if (!first) return undefined;
    return first.documentId === undefined
      ? { result: first.result }
      : { result: first.result, documentId: first.documentId };
  },
  fabric: getFabric,
  async sha256Hex(text) {
    const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
    return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
  },
  now: () => Date.now(),
  perf: () => performance.now(),
  uuid: () => crypto.randomUUID(),
});

chrome.permissions.onRemoved.addListener((removed) => {
  void core.onPermissionsRemoved(removed.origins ?? []);
});

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  const envelope = typeof message === "object" && message !== null && "proto" in message;
  const run = envelope ? core.handlePage(message, sender) : core.handleCommand(message, sender);
  run.then(sendResponse, () => sendResponse({ ok: false, error: "internal" }));
  return true;
});
