import { type Effective, effectiveArrange, hideable, type Place, placeOf } from "./layout.ts";
import type { PRegion, PresentationBody } from "./model.ts";
import type { Inst, RendererApi } from "./types.ts";

const PLACES: Place[] = ["top", "start", "center", "end", "bottom"];
const LANDMARK: Partial<Record<NonNullable<PRegion["role"]>, [keyof HTMLElementTagNameMap, string | null]>> = {
  header: ["header", null],
  footer: ["footer", null],
  aside: ["aside", "Details"],
};
const ROLE_NAME: Record<NonNullable<PRegion["role"]>, string> = {
  navigation: "Navigation",
  header: "Header",
  main: "Main",
  aside: "Details",
  toolbar: "Toolbar",
  footer: "Footer",
};

interface Node {
  region: PRegion;
  el: HTMLElement;
  frame: HTMLElement;
  body: HTMLElement;
  parts: HTMLElement;
  tablist: HTMLElement;
  toggle: HTMLButtonElement;
  mount: Inst;
  kids: Map<string, Node>;
  selected: string;
}

export const humanize = (id: string): string => {
  const t = id.replace(/^(view|reg|region)_/, "").replace(/[_-]+/g, " ");
  return t.charAt(0).toUpperCase() + t.slice(1);
};

/** Draws the region tree around the components and re-arranges it as the available width changes. */
export class RegionHost {
  readonly el: HTMLElement;
  private grid: HTMLElement;
  private places = new Map<Place, HTMLElement>();
  private nodes = new Map<string, Node>();
  private probe: HTMLElement;
  private watcher: ResizeObserver | null = null;
  private widthCh = Number.POSITIVE_INFINITY;
  private userCollapsed = new Map<string, boolean>();
  private current: Effective | null = null;

  private r: RendererApi;
  private main: HTMLElement;
  private parent: HTMLElement;

  constructor(r: RendererApi, main: HTMLElement, parent: HTMLElement) {
    this.r = r;
    this.main = main;
    this.parent = parent;
    const dom = r.dom;
    this.el = dom.el("div", "layout-frame");
    this.grid = dom.el("div", "layout");
    dom.attr(this.el, "data-vsf-layout", "");
    dom.append(this.el, this.grid);
    for (const p of PLACES) {
      const w = dom.el("div", "place");
      dom.attr(w, "data-place", p);
      this.places.set(p, w);
      dom.append(this.grid, w);
    }
    this.probe = dom.el("div", "ch-probe");
    dom.attr(this.probe, "aria-hidden", "true");
    dom.append(this.grid, this.probe);
    dom.append(parent, this.el);
    if (typeof ResizeObserver !== "undefined") {
      this.watcher = new ResizeObserver(() => this.measure());
      this.watcher.observe(this.el);
    }
  }

  private measure(): void {
    const chPx = this.probe.getBoundingClientRect().width / 10;
    const w = this.el.getBoundingClientRect().width;
    const next = chPx > 0 && w > 0 ? w / chPx : Number.POSITIVE_INFINITY;
    if (Math.abs(next - this.widthCh) < 0.01) return;
    this.widthCh = next;
    this.arrange();
  }

  private label(region: PRegion): string {
    const heading = region.components.map((id) => this.r.component(id)).find((c) => c?.kind === "heading");
    return heading?.accessibility.name ?? (region.role ? ROLE_NAME[region.role] : humanize(region.id));
  }

  private make(region: PRegion, isMain: boolean): Node {
    const dom = this.r.dom;
    const landmark = region.role ? LANDMARK[region.role] : undefined;
    const frame = isMain ? this.main : dom.el(landmark ? landmark[0] : "div", "region");
    if (isMain) frame.classList.add("region");
    if (landmark?.[1]) dom.attr(frame, "aria-label", landmark[1]);
    dom.attr(frame, "data-region", region.id);
    if (region.role) dom.attr(frame, "data-role", region.role);
    const toggle = dom.el("button");
    toggle.type = "button";
    toggle.className = "region-toggle";
    const body = dom.el("div", "region-body");
    body.id = `rb-${region.id}`;
    const parts = dom.el("div", "parts");
    const tablist = dom.el("div", "tablist");
    dom.attr(tablist, "role", "tablist");
    const mount: Inst = {
      id: region.id,
      kind: "group",
      el: body,
      mount: body,
      kids: new Map(),
      update() {},
      dispose: () => {},
    };
    return { region, el: frame, frame, body, parts, tablist, toggle, mount, kids: new Map(), selected: "" };
  }

  /** Brings the tree in line with the presentation and the allowed preferences. */
  sync(_body: PresentationBody, eff: Effective): void {
    this.current = eff;
    const live = new Set(eff.order.map((r) => r.id));
    for (const [id, n] of [...this.nodes]) {
      if (live.has(id)) continue;
      n.mount.dispose();
      n.mount.kids.clear();
      if (n.el !== this.main) n.el.remove();
      this.nodes.delete(id);
    }
    const first = eff.order.find((r) => r.role === "main" && r.parent === undefined);
    for (const region of eff.order) {
      let n = this.nodes.get(region.id);
      if (!n) {
        n = this.make(region, region === first);
        this.nodes.set(region.id, n);
      }
      n.region = region;
      this.r.syncKids(n.mount, region.components);
    }
    for (const n of this.nodes.values()) n.kids.clear();
    for (const region of eff.order) {
      const n = this.nodes.get(region.id) as Node;
      if (region.parent) this.nodes.get(region.parent)?.kids.set(region.id, n);
    }
    for (const p of PLACES) {
      const want = eff.order
        .filter((r) => r.parent === undefined && placeOf(r) === p)
        .map((r) => (this.nodes.get(r.id) as Node).el);
      this.place(this.places.get(p) as HTMLElement, want);
    }
    for (const region of eff.order) this.frameOf(this.nodes.get(region.id) as Node, eff);
    this.measure();
    this.arrange();
  }

  private place(host: HTMLElement, want: HTMLElement[]): void {
    const same = host.children.length === want.length && want.every((e, i) => host.children[i] === e);
    if (same) return;
    this.r.dom.stats.moved += want.length;
    this.r.dom.stats.writes++;
    host.replaceChildren(...want);
  }

  private frameOf(n: Node, eff: Effective): void {
    const dom = this.r.dom;
    const r = n.region;
    const label = this.label(r);
    const kids = [...n.kids.values()].sort((a, b) => eff.order.indexOf(a.region) - eff.order.indexOf(b.region));
    const canToggle = hideable(r);
    const parts: HTMLElement[] = [];
    for (const k of kids) parts.push(k.el);
    for (const k of kids) {
      dom.attr(k.el, "data-part", k.region.id);
    }
    dom.attr(n.parts, "data-parts", String(kids.length));
    if (kids.length > 0) n.parts.style.setProperty("--vsf-parts", String(kids.length));
    if (n.parts.children.length !== parts.length || parts.some((e, i) => n.parts.children[i] !== e)) {
      this.r.dom.stats.writes++;
      n.parts.replaceChildren(...parts);
    }
    const sized = eff.sizes.get(r.id) ?? r.size?.basis ?? r.size?.min;
    const set = (name: string, v: number | undefined): void => {
      if (v === undefined) n.frame.style.removeProperty(name);
      else n.frame.style.setProperty(name, `${v}ch`);
    };
    set("--vsf-min", r.size?.min);
    set("--vsf-max", r.size?.max);
    set("--vsf-basis", sized);
    const content: HTMLElement[] = [];
    if (canToggle) content.push(n.toggle);
    if (kids.length > 0 && this.anyTabs(r)) content.push(n.tablist);
    content.push(n.body);
    if (kids.length > 0) content.push(n.parts);
    if (n.frame.children.length !== content.length || content.some((e, i) => n.frame.children[i] !== e)) {
      dom.stats.writes++;
      n.frame.replaceChildren(...content);
    }
    if (canToggle && !n.toggle.onclick) {
      n.toggle.onclick = (): void => {
        const now = n.frame.hasAttribute("data-collapsed");
        this.userCollapsed.set(r.id, !now);
        this.arrange();
        if (now) n.body.focus?.();
      };
    }
    dom.attr(n.toggle, "aria-controls", n.body.id);
    dom.attr(n.toggle, "data-label", label);
  }

  private anyTabs(r: PRegion): boolean {
    return r.arrange === "tabs" || (r.responsive ?? []).some((x) => x.arrange === "tabs");
  }

  /** Applies the arrangement and collapse state for the current width. */
  private arrange(): void {
    const eff = this.current;
    if (!eff) return;
    const dom = this.r.dom;
    for (const n of this.nodes.values()) {
      const r = n.region;
      const now = effectiveArrange(r, this.widthCh);
      dom.attr(n.frame, "data-arrange", now.arrange);
      const canToggle = hideable(r);
      const collapsed = canToggle && (this.userCollapsed.get(r.id) ?? (now.collapse || eff.collapsed.has(r.id)));
      dom.attr(n.frame, "data-collapsed", collapsed ? "" : null);
      n.body.hidden = collapsed;
      if (canToggle) {
        const label = n.toggle.getAttribute("data-label") ?? humanize(r.id);
        dom.attr(n.toggle, "aria-expanded", collapsed ? "false" : "true");
        dom.text(n.toggle, `${collapsed ? "Show" : "Hide"} ${label}`);
      }
      this.tabs(n, now.arrange === "tabs");
    }
  }

  private tabs(n: Node, on: boolean): void {
    const dom = this.r.dom;
    const kids = [...n.kids.values()];
    if (!this.anyTabs(n.region) || kids.length === 0) return;
    if (!kids.some((k) => k.region.id === n.selected)) n.selected = kids[0]?.region.id ?? "";
    dom.attr(n.parts, "role", on ? "presentation" : null);
    const buttons: HTMLElement[] = [];
    for (const k of kids) {
      const id = k.region.id;
      const selected = id === n.selected;
      dom.attr(k.el, "role", on ? "tabpanel" : null);
      dom.attr(k.el, "aria-labelledby", on ? `tab-${id}` : null);
      dom.attr(k.el, "data-selected", on && selected ? "" : null);
      if (!on) continue;
      let b = n.tablist.querySelector<HTMLButtonElement>(`#tab-${CSS.escape(id)}`);
      if (!b) {
        b = dom.el("button");
        b.type = "button";
        b.id = `tab-${id}`;
        b.className = "tab";
        dom.attr(b, "role", "tab");
        b.addEventListener("click", () => this.select(n, id));
        b.addEventListener("keydown", (e) => this.tabKey(e, n, id));
      }
      dom.attr(b, "aria-selected", selected ? "true" : "false");
      dom.attr(b, "aria-controls", k.el.id || `rb-${id}`);
      dom.attr(b, "tabindex", selected ? "0" : "-1");
      dom.text(b, this.label(k.region));
      buttons.push(b);
    }
    if (on) {
      const same = n.tablist.children.length === buttons.length && buttons.every((b, i) => n.tablist.children[i] === b);
      if (!same) n.tablist.replaceChildren(...buttons);
    }
    dom.attr(n.tablist, "data-on", on ? "" : null);
  }

  private tabKey(e: KeyboardEvent, n: Node, id: string): void {
    const ids = [...n.kids.keys()];
    const at = ids.indexOf(id);
    const rtl = this.r.rtl;
    const move = e.key === "ArrowRight" ? (rtl ? -1 : 1) : e.key === "ArrowLeft" ? (rtl ? 1 : -1) : 0;
    let next = -1;
    if (move !== 0) next = (at + move + ids.length) % ids.length;
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = ids.length - 1;
    if (next < 0) return;
    e.preventDefault();
    const target = ids[next] as string;
    this.select(n, target);
    n.tablist.querySelector<HTMLElement>(`#tab-${CSS.escape(target)}`)?.focus();
  }

  private select(n: Node, id: string): void {
    n.selected = id;
    this.arrange();
  }

  /** Visits every component instance, for availability refreshes. */
  walk(fn: (i: Inst) => void): void {
    const visit = (i: Inst): void => {
      for (const k of i.kids.values()) {
        fn(k);
        visit(k);
      }
    };
    for (const n of this.nodes.values()) visit(n.mount);
  }

  /** Instances that can name the row an operation would apply to. */
  mounts(): Inst[] {
    return [...this.nodes.values()].map((n) => n.mount);
  }

  dispose(): void {
    this.watcher?.disconnect();
    for (const n of this.nodes.values()) {
      for (const k of n.mount.kids.values()) k.dispose();
      n.mount.kids.clear();
    }
    this.nodes.clear();
    if (this.main.parentElement !== this.parent) this.parent.append(this.main);
    this.main.replaceChildren();
    this.main.classList.remove("region");
    for (const a of ["data-region", "data-role", "data-arrange", "data-collapsed"]) this.main.removeAttribute(a);
    this.main.removeAttribute("style");
    this.el.remove();
  }
}
