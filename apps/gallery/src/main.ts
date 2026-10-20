import { PALETTE } from "@vsf/design-system";
import { compositions } from "./compositions.ts";
import { type Density, type Look, mountComposition, type Scheme } from "./scene.ts";
import { mountShell, SECTIONS, type SectionId } from "./shell.ts";
import { mountStates } from "./states.ts";

export const SCENES = [
  "shell",
  "enterprise-grid",
  "grouped-form",
  "workbench-split",
  "master-detail",
  "states",
] as const;
export type SceneId = (typeof SCENES)[number];

const SCHEMES: Scheme[] = ["light", "dark"];
const DENSITIES: Density[] = ["compact", "comfortable", "spacious"];

function parse(): { scene: SceneId; section: SectionId; look: Look } {
  const hash = location.hash.replace(/^#\/?/, "");
  const [path = "shell", sub] = hash.split("/");
  const q = new URLSearchParams(location.search);
  const scene = (SCENES as readonly string[]).includes(path) ? (path as SceneId) : "shell";
  const section = SECTIONS.some((s) => s.id === sub) ? (sub as SectionId) : "interface";
  const scheme = SCHEMES.includes(q.get("scheme") as Scheme) ? (q.get("scheme") as Scheme) : "light";
  const density = DENSITIES.includes(q.get("density") as Density) ? (q.get("density") as Density) : "comfortable";
  return { scene, section, look: { scheme, density } };
}

async function render(): Promise<void> {
  const { scene, section, look } = parse();
  const root = document.getElementById("app") as HTMLElement;
  const p = PALETTE[look.scheme];
  document.documentElement.style.colorScheme = look.scheme;
  document.body.style.background = p.bg;
  document.body.style.color = p.text;
  document.title = `Varifold gallery: ${scene}`;
  root.replaceChildren();
  root.removeAttribute("data-ready");
  const host = document.createElement("div");
  host.id = "scene";
  root.append(host);
  if (scene === "shell") {
    mountShell(host, look, section, (s) => `#/shell/${s}${location.search}`);
  } else if (scene === "states") {
    host.setAttribute("data-scheme", look.scheme);
    await mountStates(host, look);
  } else {
    const c = compositions[scene];
    if (c) mountComposition(host, c, look, scene === "enterprise-grid" ? 60 : 12);
  }
  await document.fonts.ready;
  await new Promise<void>((r) => requestAnimationFrame(() => requestAnimationFrame(() => r())));
  root.setAttribute("data-ready", "1");
}

window.addEventListener("hashchange", () => void render());
void render();
