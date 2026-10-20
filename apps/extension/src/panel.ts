import { originPattern, type Reply } from "./protocol.ts";

const send = (message: unknown): Promise<Reply> => chrome.runtime.sendMessage(message);
const result = document.getElementById("result") as HTMLOutputElement;

async function activeTab(): Promise<chrome.tabs.Tab | undefined> {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  return tab;
}

const show = (r: unknown): void => {
  result.textContent = JSON.stringify(r);
};

function command(id: string, type: "capture" | "transform" | "restore"): void {
  document.getElementById(id)?.addEventListener("click", async () => {
    const tab = await activeTab();
    if (tab?.id === undefined) return;
    show(await send({ type, tabId: tab.id }));
  });
}

document.getElementById("authorize")?.addEventListener("click", async () => {
  if (!navigator.userActivation.isActive) {
    result.textContent = "click required";
    return;
  }
  const tab = await activeTab();
  if (!tab?.url) {
    result.textContent = "this page cannot be authorized";
    return;
  }
  const origin = new URL(tab.url).origin;
  const granted = await chrome.permissions.request({ origins: [originPattern(origin)] });
  show(granted ? await send({ type: "grant", origin }) : "permission denied");
});

command("capture", "capture");
command("transform", "transform");
command("restore", "restore");

document.getElementById("pair")?.addEventListener("click", async () => {
  const endpoint = (document.getElementById("endpoint") as HTMLInputElement).value.trim();
  const codeInput = document.getElementById("code") as HTMLInputElement;
  const reply = await send({ type: "pair", endpoint, code: codeInput.value });
  codeInput.value = "";
  show(reply);
});

document.getElementById("unpair")?.addEventListener("click", async () => show(await send({ type: "unpair" })));
