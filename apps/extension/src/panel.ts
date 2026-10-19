import type { Reply } from "./protocol.ts";

const send = (message: unknown): Promise<Reply> => chrome.runtime.sendMessage(message);
const result = document.getElementById("result") as HTMLOutputElement;

async function activeTab(): Promise<chrome.tabs.Tab | undefined> {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  return tab;
}

/** Host access is requested only inside a user click, one origin at a time; the browser shows its own prompt. */
export async function authorize(tab: chrome.tabs.Tab | undefined): Promise<string> {
  if (!navigator.userActivation.isActive) return "needs a click";
  if (!tab?.url) return "no page";
  const origin = new URL(tab.url).origin;
  let granted = false;
  try {
    granted = await chrome.permissions.request({ origins: [`${origin}/*`] });
  } catch (e) {
    return e instanceof Error ? e.message : "permission error";
  }
  return granted ? JSON.stringify(await send({ type: "grant", origin })) : "permission denied";
}

document.getElementById("authorize")?.addEventListener("click", async () => {
  result.textContent = await authorize(await activeTab());
});

document.getElementById("capture")?.addEventListener("click", async () => {
  const tab = await activeTab();
  if (tab?.id === undefined) return;
  result.textContent = JSON.stringify(await send({ type: "capture", tabId: tab.id }));
});

for (const type of ["transform", "restore"] as const) {
  document.getElementById(type)?.addEventListener("click", async () => {
    const tab = await activeTab();
    if (tab?.id === undefined) return;
    result.textContent = JSON.stringify(await send({ type, tabId: tab.id }));
  });
}
