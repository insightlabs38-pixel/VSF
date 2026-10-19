import { base } from "./components.ts";
import type { PComponent } from "./model.ts";
import type { SourceSurface } from "./runtime.ts";
import type { Inst, RendererApi } from "./types.ts";

type Mode = "in_place" | "side_by_side" | "limited";

function sameDocument(r: RendererApi, el: Element | undefined): el is Element {
  return !!el && el.isConnected && el.ownerDocument === r.dom.doc;
}

function originOf(r: RendererApi, s: SourceSurface): boolean {
  const here = r.dom.doc.defaultView?.location.origin ?? "";
  return s.origin === here;
}

export function chooseMode(r: RendererApi, s: SourceSurface | undefined): Mode {
  if (!s || !sameDocument(r, s.element) || !originOf(r, s)) return "limited";
  if (r.adopted.has(s.element)) return "limited";
  if (s.permitted.inPlace) return "in_place";
  if (s.permitted.sideBySide) return "side_by_side";
  return "limited";
}

let n = 0;

/** Moves (never clones) a permitted same-origin widget in and restores it exactly; otherwise shows a limitation. */
export function preservedSurface(r: RendererApi, c: PComponent): Inst {
  const card = r.dom.el("section", "surface");
  const inst = base(r, c, card);
  const surface = r.rt.surface?.(c.id);
  const mode = chooseMode(r, surface);
  const name = surface?.name ?? c.accessibility.name ?? c.id;
  r.dom.attr(card, "aria-label", name);
  r.dom.attr(card, "data-mode", mode);
  r.dom.attr(card, "data-vsf-id", `surface:${c.id}`);
  const title = r.dom.el("p");
  r.dom.text(title, name);
  r.dom.append(card, title);

  const note = r.dom.el("p");
  r.dom.append(card, note);

  if (mode === "in_place" && surface?.element) {
    const el = surface.element;
    const slotName = `vsf-surface-${++n}`;
    const holder = r.dom.el("div");
    r.dom.attr(holder, "slot", slotName);
    r.dom.attr(holder, "data-vsf-surface", c.id);
    const slot = r.dom.el("slot");
    r.dom.attr(slot, "name", slotName);
    r.dom.append(card, slot);
    const marker = r.dom.doc.createComment("vsf-original-position");
    el.before(marker);
    holder.append(el);
    r.root.host.append(holder);
    r.adopted.add(el);
    r.dom.text(
      note,
      "The original control is shown here as the source built it. Its own styling may differ slightly outside its page.",
    );
    inst.off.push(() => {
      // Put the very same node back where it was; the marker is the only thing we ever added there.
      marker.replaceWith(el);
      holder.remove();
      r.adopted.delete(el);
    });
  } else if (mode === "side_by_side" && surface?.element) {
    const el = surface.element;
    r.adopted.add(el);
    r.dock(true);
    r.dom.text(note, "The original control stays where it is, next to this view.");
    const b = r.dom.el("button");
    r.dom.attr(b, "type", "button");
    r.dom.text(b, "Locate original");
    const go = (): void => {
      el.scrollIntoView({ block: "center" });
      if (el instanceof HTMLElement) el.focus({ preventScroll: true });
      r.announcer.say(`${name} is beside this view`);
    };
    b.addEventListener("click", go);
    r.dom.append(card, b);
    inst.off.push(() => {
      b.removeEventListener("click", go);
      r.dock(false);
      r.adopted.delete(el);
    });
  } else {
    const why = !surface
      ? "No permitted view of the original is available."
      : !originOf(r, surface)
        ? `The original comes from ${surface.origin} and cannot be shown inside this page.`
        : "Showing the original here is not permitted.";
    r.dom.text(note, `${why}${surface?.note ? ` ${surface.note}` : ""}`);
    if (surface && /^https?:\/\//.test(surface.origin)) {
      const a = r.dom.el("a", "link");
      r.dom.attr(a, "href", surface.origin);
      r.dom.attr(a, "target", "_blank");
      r.dom.attr(a, "rel", "noopener noreferrer");
      r.dom.text(a, `Open original in ${surface.origin}`);
      r.dom.append(card, a);
    }
  }
  inst.update = () => {};
  return inst;
}
