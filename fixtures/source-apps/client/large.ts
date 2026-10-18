import { decodePack, generatePackBytes, NONE } from "@vsf/observer";

const params = new URLSearchParams(location.search);
const nodes = Math.min(Number(params.get("n") ?? 1000), 200_000);
const seed = Number(params.get("seed") ?? 1);
const snap = decodePack(generatePackBytes(seed, nodes));
const made: HTMLElement[] = [];
const root = document.getElementById("root") as HTMLElement;
snap.nodes.forEach((n, i) => {
  const tag = snap.strings[n.tag] as string;
  const el = i === 0 ? root : document.createElement(tag);
  if (n.role !== NONE) el.setAttribute("role", snap.strings[n.role] as string);
  if (n.name !== NONE) el.setAttribute("aria-label", snap.strings[n.name] as string);
  if (n.text !== NONE && tag !== "input") el.textContent = snap.strings[n.text] as string;
  made.push(el);
  if (i > 0) (made[n.parent] as HTMLElement).append(el);
});
document.documentElement.dataset.built = String(nodes);
