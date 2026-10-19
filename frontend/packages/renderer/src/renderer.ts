import { Announcer, captureFocus, resolveDirection, restoreFocus } from "./a11y.ts";
import { button, checkbox, choice, form, group, heading, link, list, textBlock, textInput } from "./components.ts";
import { table } from "./grid.ts";
import type { PComponent, PresentationBody } from "./model.ts";
import { type Command, Palette } from "./palette.ts";
import { preservedSurface } from "./preserve.ts";
import type { DispatchResult, Runtime } from "./runtime.ts";
import { componentCss } from "./styles.ts";
import type { Inst, RendererApi } from "./types.ts";
import { Dom, newStats, placeChildren, type Stats } from "./writes.ts";

export interface Prefs {
  scheme?: "light" | "dark" | "system";
  accent?: "none" | "mint" | "blue";
  density?: "compact" | "comfortable" | "spacious";
  nav?: "top" | "side" | "hidden";
}

export interface RendererOptions {
  hotkeys?: boolean;
  onPrefs?(prefs: Prefs): void;
}

const SELF_MANAGED = new Set(["table", "preserved_surface"]);

function create(r: RendererApi, c: PComponent): Inst {
  switch (c.kind) {
    case "heading":
      return heading(r, c);
    case "text":
      return textBlock(r, c);
    case "form":
      return form(r, c);
    case "group":
      return group(r, c);
    case "list":
      return list(r, c);
    case "link":
      return link(r, c);
    case "text_input":
      return textInput(r, c);
    case "choice":
      return choice(r, c);
    case "checkbox":
      return checkbox(r, c);
    case "button":
      return button(r, c);
    case "table":
      return table(r, c);
    case "preserved_surface":
      return preservedSurface(r, c);
    default:
      return group(r, c);
  }
}

function pseudo(el: HTMLElement): Inst {
  return { id: "", kind: "group", el, mount: el, kids: new Map(), update() {}, dispose() {} };
}

export class Renderer implements RendererApi {
  readonly host: HTMLElement;
  readonly root: ShadowRoot;
  readonly dom: Dom;
  readonly stats: Stats = newStats();
  readonly rt: Runtime;
  readonly announcer: Announcer;
  readonly adopted = new WeakSet<Element>();
  body: PresentationBody;
  rtl = false;
  activeEntity = "";
  prefs: Prefs = {};

  private app: HTMLElement;
  private navHost: HTMLElement;
  private main: HTMLElement;
  private navRoot: Inst;
  private mainRoot: Inst;
  private palette: Palette;
  private opts: RendererOptions;
  private unsubs: (() => void)[] = [];
  private docked = 0;
  private savedHostStyle: string | null = null;
  private destroyed = false;

  constructor(host: HTMLElement, rt: Runtime, body: PresentationBody, opts: RendererOptions = {}) {
    this.host = host;
    this.rt = rt;
    this.body = body;
    this.opts = opts;
    this.root = host.shadowRoot ?? host.attachShadow({ mode: "open" });
    this.dom = new Dom(host.ownerDocument, this.stats);
    const style = this.dom.el("style");
    style.textContent = componentCss();
    this.root.append(style);
    this.app = this.dom.el("div", "app");
    const skip = this.dom.el("a", "skip");
    this.dom.attr(skip, "href", "#main");
    this.dom.text(skip, "Skip to content");
    skip.addEventListener("click", (e) => {
      e.preventDefault();
      this.main.focus();
    });
    const status = this.dom.el("div", "status-line");
    this.dom.attr(status, "role", "status");
    this.dom.attr(status, "aria-live", "polite");
    this.dom.attr(status, "aria-atomic", "true");
    const alerts = this.dom.el("div", "alerts");
    this.dom.attr(alerts, "role", "alert");
    this.navHost = this.dom.el("div", "nav-host");
    this.main = this.dom.el("main");
    this.dom.attr(this.main, "id", "main");
    this.dom.attr(this.main, "tabindex", "-1");
    this.dom.attr(this.main, "data-vsf-id", "main");
    for (const n of [skip, status, alerts, this.navHost, this.main]) this.app.append(n);
    this.root.append(this.app);
    this.announcer = new Announcer(this.dom, status, alerts);
    this.navRoot = pseudo(this.navHost);
    this.mainRoot = pseudo(this.main);
    this.palette = new Palette({
      dom: this.dom,
      root: this.root,
      app: this.app,
      main: this.main,
      commands: () => this.commands(),
      say: (m, l) => this.announcer.say(m, l === "assertive" ? "assertive" : "polite"),
    });
    if (opts.hotkeys !== false) {
      const doc = host.ownerDocument;
      const onKey = (e: KeyboardEvent): void => {
        if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
          e.preventDefault();
          if (this.palette.isOpen) this.palette.close();
          else this.palette.open();
        }
      };
      doc.addEventListener("keydown", onKey, true);
      this.unsubs.push(() => doc.removeEventListener("keydown", onKey, true));
    }
    if (rt.onAvailability) this.unsubs.push(rt.onAvailability(() => this.refreshAll()));
    this.activeEntity = this.firstEntity();
    this.apply();
  }

  get currentBody(): PresentationBody {
    return this.body;
  }

  private viewEntities(): { entity: string; region: string }[] {
    const out: { entity: string; region: string }[] = [];
    for (const rg of this.body.regions) {
      if (!rg.id.startsWith("view_")) continue;
      for (const id of rg.components) {
        const c = this.body.components.find((x) => x.id === id);
        if (c && (c.kind === "form" || c.kind === "table") && c.binding?.entity) {
          out.push({ entity: c.binding.entity, region: rg.id });
          break;
        }
      }
    }
    return out;
  }

  private firstEntity(): string {
    return this.viewEntities()[0]?.entity ?? "";
  }

  private effective(): PresentationBody["theme"] {
    const t = this.body.theme;
    return {
      scheme: this.prefs.scheme ?? t.scheme,
      accent: this.prefs.accent ?? t.accent,
      density: this.prefs.density ?? t.density,
    };
  }

  private apply(): void {
    const dom = this.dom;
    this.rtl = resolveDirection(this.body.direction, this.body.locale) === "rtl";
    dom.attr(this.host, "dir", this.rtl ? "rtl" : "ltr");
    dom.attr(this.host, "lang", this.body.locale);
    const t = this.effective();
    dom.attr(this.host, "data-scheme", t.scheme);
    dom.attr(this.host, "data-accent", t.accent);
    dom.attr(this.host, "data-density", t.density);
    dom.attr(this.app, "data-nav", this.prefs.nav ?? "top");
    const views = this.viewEntities();
    if (!views.some((v) => v.entity === this.activeEntity)) this.activeEntity = views[0]?.entity ?? "";
    const navRegion = this.body.regions.find((x) => x.id === "nav");
    this.syncKids(this.navRoot, navRegion?.components ?? []);
    const region = views.find((v) => v.entity === this.activeEntity)?.region;
    const ids = this.body.regions.find((x) => x.id === region)?.components ?? [];
    this.syncKids(this.mainRoot, ids);
  }

  syncKids(parent: Inst, ids: string[]): void {
    const wanted: Node[] = [];
    const keep = new Set<string>();
    for (const id of ids) {
      const comp = this.body.components.find((c) => c.id === id);
      if (!comp) continue;
      keep.add(id);
      let k = parent.kids.get(id);
      if (k && k.kind !== comp.kind) {
        k.dispose();
        (k.outer ?? k.el).remove();
        parent.kids.delete(id);
        k = undefined;
      }
      if (!k) {
        k = create(this, comp);
        parent.kids.set(id, k);
        if (parent.kind === "list" && !k.outer) {
          const li = this.dom.el("li");
          li.append(k.el);
          k.outer = li;
        }
      } else k.update(comp);
      if (!SELF_MANAGED.has(comp.kind)) this.syncKids(k, comp.children);
      wanted.push(k.outer ?? k.el);
    }
    for (const [id, k] of [...parent.kids]) {
      if (keep.has(id)) continue;
      k.dispose();
      this.dom.remove(k.outer ?? k.el);
      parent.kids.delete(id);
    }
    placeChildren(this.dom, parent.mount, wanted);
  }

  /** Replaces the presentation. Components with the same id are updated in place and focus follows the same operation. */
  update(body: PresentationBody): void {
    const mem = captureFocus(this.root);
    this.body = body;
    this.apply();
    restoreFocus(this.root, mem, this.main);
  }

  navigate(entity: string): void {
    if (entity === this.activeEntity) return;
    this.activeEntity = entity;
    const mem = captureFocus(this.root);
    this.apply();
    this.refreshAll();
    if (!restoreFocus(this.root, mem, this.main)) this.main.focus({ preventScroll: true });
    const label = this.rt.semantic.entities.find((e) => e.id === entity)?.label ?? entity;
    this.announcer.say(`Showing ${label}`);
  }

  async press(action: string, entity: string, label: string, rowKey?: string): Promise<void> {
    const a = this.rt.availability(action);
    if (a.state !== "available") {
      this.announcer.say(`${label} is unavailable: ${a.reason ?? a.state}`, "assertive");
      return;
    }
    let result: undefined | DispatchResult;
    try {
      result = await this.rt.dispatch(action, rowKey === undefined ? { entity } : { entity, rowKey });
    } catch (e) {
      this.announcer.say(`${label} failed: ${e instanceof Error ? e.message : "unknown error"}`, "assertive");
      return;
    }
    if (result && !result.ok) this.announcer.say(result.message ?? `${label} did not complete`, "assertive");
    else this.announcer.say(result?.message ?? `${label} done`);
  }

  dock(on: boolean): void {
    this.docked += on ? 1 : -1;
    const style = this.host.style;
    if (this.docked > 0 && this.savedHostStyle === null) {
      this.savedHostStyle = this.host.getAttribute("style") ?? "";
      style.cssText = `${this.savedHostStyle};position:fixed;inset-block:0;inset-inline-end:0;inline-size:50vw;overflow:auto;z-index:2147483000;border-inline-start:2px solid var(--vsf-control-border)`;
      this.stats.writes++;
    } else if (this.docked <= 0 && this.savedHostStyle !== null) {
      if (this.savedHostStyle) this.host.setAttribute("style", this.savedHostStyle);
      else this.host.removeAttribute("style");
      this.savedHostStyle = null;
      this.docked = 0;
      this.stats.writes++;
    }
  }

  private walk(inst: Inst, fn: (i: Inst) => void): void {
    for (const k of inst.kids.values()) {
      fn(k);
      this.walk(k, fn);
    }
  }

  refreshAll(): void {
    for (const root of [this.navRoot, this.mainRoot]) this.walk(root, (i) => i.refresh?.());
  }

  setPreference(p: Prefs): void {
    this.prefs = { ...this.prefs, ...p };
    this.apply();
    this.opts.onPrefs?.({ ...this.prefs });
  }

  resetPreferences(): void {
    this.prefs = {};
    this.apply();
    this.opts.onPrefs?.({});
  }

  openPalette(): void {
    this.palette.open();
  }

  closePalette(): void {
    this.palette.close();
  }

  private activeRowKey(): string | null {
    let row: string | null = null;
    this.walk(this.mainRoot, (i) => {
      if (i.activeRow) row = i.activeRow();
    });
    return row;
  }

  commands(): Command[] {
    const out: Command[] = [];
    for (const v of this.viewEntities()) {
      const label = this.rt.semantic.entities.find((e) => e.id === v.entity)?.label ?? v.entity;
      out.push({
        id: `view:${v.entity}`,
        label: `Go to ${label}`,
        availability: { state: "available" },
        run: () => this.navigate(v.entity),
      });
    }
    const seen = new Set<string>();
    const row = this.activeRowKey();
    const inGrid = this.body.components.some((c) => c.kind === "table" && c.binding?.entity === this.activeEntity);
    const visit = (inst: Inst): void => {
      const c = inst.cur;
      if (c?.kind !== "button" || !c.binding?.action || seen.has(c.binding.action)) return;
      const action = c.binding.action;
      seen.add(action);
      const entity = c.binding.entity ?? this.activeEntity;
      const name = c.accessibility.name ?? action;
      const base = this.rt.availability(action);
      const needsRow = inGrid && row === null;
      out.push({
        id: `act:${action}`,
        label: name,
        availability: needsRow ? { state: "unusable", reason: "select a row first" } : base,
        run: () => this.press(action, entity, name, inGrid ? (row ?? undefined) : undefined),
      });
    };
    this.walk(this.mainRoot, visit);
    for (const c of this.body.components) {
      if (c.kind !== "button" || !c.binding?.action || seen.has(c.binding.action)) continue;
      if (inGrid && c.binding.entity === this.activeEntity) {
        seen.add(c.binding.action);
        const action = c.binding.action;
        const name = c.accessibility.name ?? action;
        out.push({
          id: `act:${action}`,
          label: name,
          availability:
            row === null ? { state: "unusable", reason: "select a row first" } : this.rt.availability(action),
          run: () => this.press(action, c.binding?.entity ?? this.activeEntity, name, row ?? undefined),
        });
      }
    }
    return out;
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.palette.close();
    for (const f of this.unsubs.splice(0)) f();
    for (const root of [this.navRoot, this.mainRoot]) {
      for (const k of root.kids.values()) k.dispose();
      root.kids.clear();
    }
    this.dock(false);
    this.host.shadowRoot?.replaceChildren();
    for (const a of ["dir", "lang", "data-scheme", "data-accent", "data-density"]) this.host.removeAttribute(a);
  }
}
