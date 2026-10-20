import { originPattern, type Reply } from "./protocol.ts";

const send = (message: unknown): Promise<Reply> => chrome.runtime.sendMessage(message);
const result = document.getElementById("result") as HTMLOutputElement;

async function activeTab(): Promise<chrome.tabs.Tab | undefined> {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  return tab;
}

document.getElementById("authorize")?.addEventListener("click", async () => {
  const tab = await activeTab();
  if (!tab?.url) return;
  const origin = new URL(tab.url).origin;
  if (!navigator.userActivation.isActive) return;
  const granted = await chrome.permissions.request({ origins: [originPattern(origin)] });
  result.textContent = granted ? JSON.stringify(await send({ type: "grant", origin })) : "permission denied";
});

document.getElementById("capture")?.addEventListener("click", async () => {
  const tab = await activeTab();
  if (tab?.id === undefined) return;
  result.textContent = JSON.stringify(await send({ type: "capture", tabId: tab.id }));
});
