import { STATUS_GLYPH } from "@vsf/design-system";
import type { PComponent } from "./model.ts";
import type { Value } from "./store.ts";
import type { Inst, RendererApi } from "./types.ts";

export function base(
  r: RendererApi,
  c: PComponent,
  el: HTMLElement,
  mount: HTMLElement = el,
): Inst & { off: (() => void)[] } {
  const off: (() => void)[] = [];
  const inst: Inst & { off: (() => void)[] } = {
    id: c.id,
    kind: c.kind,
    el,
    mount,
    kids: new Map(),
    off,
    update() {},
    dispose() {
      for (const k of inst.kids.values()) k.dispose();
      inst.kids.clear();
      for (const f of off.splice(0)) f();
    },
  };
  r.dom.attr(el, "data-vsf-component", c.id);
  return inst;
}

function entityOf(r: RendererApi, c: PComponent) {
  return r.rt.semantic.entities.find((e) => e.id === c.binding?.entity);
}

export function fieldOf(r: RendererApi, c: PComponent) {
  return entityOf(r, c)?.fields.find((f) => f.id === c.binding?.field);
}

function labelText(r: RendererApi, c: PComponent): string {
  if (c.accessibility.name_source === "field_label") return fieldOf(r, c)?.name ?? c.binding?.field ?? c.id;
  return c.accessibility.name ?? c.id;
}

const str = (v: Value): string => (v === null ? "" : String(v));

export function heading(r: RendererApi, c: PComponent): Inst {
  const el = r.dom.el("h2");
  const inst = base(r, c, el);
  inst.update = (n) => r.dom.text(el, labelText(r, n));
  inst.update(c);
  return inst;
}

export function textBlock(r: RendererApi, c: PComponent): Inst {
  const el = r.dom.el("p");
  const inst = base(r, c, el);
  inst.update = (n) => {
    r.dom.text(el, labelText(r, n));
    for (const f of inst.off.splice(0)) f();
    if (n.binding?.entity && n.binding.field) {
      const key = `${n.binding.entity}.${n.binding.field}`;
      const show = (v: Value): void => r.dom.text(el, `${labelText(r, n)}: ${str(v)}`);
      show(r.rt.store.get(key));
      inst.off.push(r.rt.store.subscribe(key, show));
    }
  };
  inst.update(c);
  return inst;
}

export function form(r: RendererApi, c: PComponent): Inst {
  const el = r.dom.el("form");
  r.dom.attr(el, "novalidate", "");
  const inst = base(r, c, el);
  // Pressing Enter in a field must never run an operation.
  const stop = (e: Event): void => e.preventDefault();
  el.addEventListener("submit", stop);
  inst.off.push(() => el.removeEventListener("submit", stop));
  inst.update = (n) => {
    r.dom.attr(el, "aria-label", n.accessibility.name ?? n.id);
    r.dom.attr(el, "data-vsf-id", `form:${n.binding?.entity ?? n.id}`);
  };
  inst.update(c);
  return inst;
}

export function group(r: RendererApi, c: PComponent): Inst {
  const el = r.dom.el("div", "group");
  r.dom.attr(el, "role", "group");
  const inst = base(r, c, el);
  inst.update = (n) => r.dom.attr(el, "aria-label", n.accessibility.name ?? n.id);
  inst.update(c);
  return inst;
}

export function list(r: RendererApi, c: PComponent): Inst {
  const nav = c.accessibility.role === "navigation";
  const el = r.dom.el(nav ? "nav" : "div");
  const ul = r.dom.el("ul");
  r.dom.append(el, ul);
  const inst = base(r, c, el, ul);
  inst.update = (n) => {
    if (nav) r.dom.attr(el, "aria-label", n.accessibility.name ?? n.id);
  };
  inst.update(c);
  return inst;
}

export function link(r: RendererApi, c: PComponent): Inst {
  const li = r.dom.el("li");
  const a = r.dom.el("a", "link");
  r.dom.append(li, a);
  const inst = base(r, c, a);
  inst.outer = li;
  const go = (e: Event): void => {
    e.preventDefault();
    if (inst.cur?.binding?.entity) r.navigate(inst.cur.binding.entity);
  };
  a.addEventListener("click", go);
  inst.off.push(() => a.removeEventListener("click", go));
  inst.update = (n) => {
    inst.cur = n;
    r.dom.text(a, n.accessibility.name ?? n.id);
    r.dom.attr(a, "href", `#${n.binding?.entity ?? n.id}`);
    r.dom.attr(a, "data-vsf-id", `nav:${n.binding?.entity ?? n.id}`);
    r.dom.attr(a, "aria-current", n.binding?.entity === r.activeEntity ? "page" : null);
  };
  inst.refresh = () => inst.cur && inst.update(inst.cur);
  inst.update(c);
  return inst;
}

function certaintyBadge(r: RendererApi, parent: HTMLElement, id: string, certainty: string): HTMLElement | null {
  if (certainty !== "unknown" && certainty !== "conflicted") return null;
  const status = certainty === "unknown" ? "caution" : "warning";
  const b = r.dom.el("span", "badge");
  r.dom.attr(b, "data-status", status);
  r.dom.attr(b, "id", id);
  r.dom.text(b, `${STATUS_GLYPH[status]} ${certainty === "unknown" ? "Unverified" : "Conflicting evidence"}`);
  r.dom.append(parent, b);
  return b;
}

let uid = 0;

export function textInput(r: RendererApi, c: PComponent): Inst {
  const wrap = r.dom.el("div", "field");
  const label = r.dom.el("label");
  const input = r.dom.el("input");
  const n = ++uid;
  const inputId = `vsf-in-${n}`;
  r.dom.attr(input, "id", inputId);
  r.dom.attr(label, "for", inputId);
  r.dom.attr(input, "type", "text");
  r.dom.append(wrap, label);
  r.dom.append(wrap, input);
  const inst = base(r, c, wrap);
  const f = fieldOf(r, c);
  const key = `${c.binding?.entity}.${c.binding?.field}`;
  if (f)
    certaintyBadge(r, wrap, `vsf-note-${n}`, f.certainty) && r.dom.attr(input, "aria-describedby", `vsf-note-${n}`);
  if (f?.nullable === "no") {
    r.dom.attr(input, "required", "");
    r.dom.attr(input, "aria-required", "true");
  }
  if (f?.value_type === "integer") r.dom.attr(input, "inputmode", "numeric");
  if (f?.value_type === "decimal") r.dom.attr(input, "inputmode", "decimal");
  r.dom.attr(input, "data-vsf-id", `fld:${c.binding?.entity}.${c.binding?.field}`);
  const onInput = (): void => {
    r.rt.store.set(key, input.value);
  };
  input.addEventListener("input", onInput);
  inst.off.push(() => input.removeEventListener("input", onInput));
  inst.off.push(r.rt.store.subscribe(key, (v) => r.dom.value(input, str(v))));
  r.dom.value(input, str(r.rt.store.get(key)));
  inst.update = (nc) => r.dom.text(label, labelText(r, nc));
  inst.update(c);
  return inst;
}

export function choice(r: RendererApi, c: PComponent): Inst {
  const wrap = r.dom.el("div", "field");
  const label = r.dom.el("label");
  const select = r.dom.el("select");
  const id = `vsf-sel-${++uid}`;
  r.dom.attr(select, "id", id);
  r.dom.attr(label, "for", id);
  r.dom.append(wrap, label);
  r.dom.append(wrap, select);
  const inst = base(r, c, wrap);
  const key = `${c.binding?.entity}.${c.binding?.field}`;
  const options = r.rt.choices?.(c.binding?.entity ?? "", c.binding?.field ?? "") ?? [];
  for (const o of ["", ...options]) {
    const opt = r.dom.el("option");
    r.dom.text(opt, o);
    opt.value = o;
    r.dom.append(select, opt);
  }
  r.dom.attr(select, "data-vsf-id", `fld:${c.binding?.entity}.${c.binding?.field}`);
  const onChange = (): void => {
    r.rt.store.set(key, select.value);
  };
  select.addEventListener("change", onChange);
  inst.off.push(() => select.removeEventListener("change", onChange));
  inst.off.push(r.rt.store.subscribe(key, (v) => r.dom.value(select, str(v))));
  r.dom.value(select, str(r.rt.store.get(key)));
  inst.update = (nc) => r.dom.text(label, labelText(r, nc));
  inst.update(c);
  return inst;
}

export function checkbox(r: RendererApi, c: PComponent): Inst {
  const wrap = r.dom.el("div", "check");
  const input = r.dom.el("input");
  const label = r.dom.el("label");
  const id = `vsf-chk-${++uid}`;
  r.dom.attr(input, "type", "checkbox");
  r.dom.attr(input, "id", id);
  r.dom.attr(label, "for", id);
  r.dom.append(wrap, input);
  r.dom.append(wrap, label);
  const inst = base(r, c, wrap);
  const key = `${c.binding?.entity}.${c.binding?.field}`;
  r.dom.attr(input, "data-vsf-id", `fld:${c.binding?.entity}.${c.binding?.field}`);
  const onChange = (): void => {
    r.rt.store.set(key, input.checked);
  };
  input.addEventListener("change", onChange);
  inst.off.push(() => input.removeEventListener("change", onChange));
  inst.off.push(r.rt.store.subscribe(key, (v) => r.dom.checked(input, v === true)));
  r.dom.checked(input, r.rt.store.get(key) === true);
  inst.update = (nc) => r.dom.text(label, labelText(r, nc));
  inst.update(c);
  return inst;
}

/** One operation button. In a grid it is bound to a row, which becomes part of its accessible name. */
export function button(r: RendererApi, c: PComponent, rowKey?: string, rowTitle?: () => string): Inst {
  const el = r.dom.el("button");
  r.dom.attr(el, "type", "button");
  const inst = base(r, c, el);
  const reason = r.dom.el("span", "sr-only");
  const rid = `vsf-why-${++uid}`;
  r.dom.attr(reason, "id", rid);
  r.dom.append(el, reason);
  const label = r.dom.doc.createTextNode("");
  r.dom.append(el, label);
  let pending = false;
  const action = c.binding?.action ?? "";
  const entity = c.binding?.entity ?? "";
  const def = r.rt.semantic.actions.find((a) => a.id === action);
  const labelOf = (): string => def?.label ?? (inst.cur as PComponent).accessibility.name ?? action;
  const paint = (): void => {
    const cur = inst.cur as PComponent;
    const name = def?.label ?? cur.accessibility.name ?? cur.id;
    r.dom.attr(el, "data-label-source", def ? "semantic" : "presentation");
    r.dom.attr(
      el,
      "data-label-mismatch",
      def && cur.accessibility.name && cur.accessibility.name !== def.label ? "true" : null,
    );
    const title = rowTitle?.();
    const a = r.rt.availability(action);
    if (label.textContent !== name) {
      r.dom.stats.writes++;
      label.textContent = name;
    }
    r.dom.attr(el, "aria-label", title ? `${name}: ${title}` : null);
    r.dom.attr(el, "data-effect", def?.effect.declared ?? "indeterminate");
    r.dom.attr(el, "data-state", a.state);
    if (a.state === "available") {
      r.dom.attr(el, "aria-disabled", null);
      r.dom.attr(el, "aria-describedby", null);
      r.dom.text(reason, "");
    } else {
      r.dom.attr(el, "aria-disabled", "true");
      r.dom.attr(el, "aria-describedby", rid);
      r.dom.text(reason, `Unavailable, ${a.state}${a.reason ? `: ${a.reason}` : ""}`);
    }
    r.dom.attr(el, "aria-keyshortcuts", cur.accessibility.shortcut ?? null);
    r.dom.attr(el, "data-vsf-id", rowKey ? `act:${action}@${rowKey}` : `act:${action}`);
  };
  const click = async (e: Event): Promise<void> => {
    if (pending) return;
    if (!e.isTrusted && !r.allowSynthetic && (def?.effect.declared ?? "indeterminate") !== "read_only") {
      r.announcer.say(`${labelOf()} ignored: activation did not come from the user`, "assertive");
      return;
    }
    pending = true;
    r.dom.attr(el, "aria-busy", "true");
    try {
      await r.press(action, entity, labelOf(), rowKey);
    } finally {
      pending = false;
      r.dom.attr(el, "aria-busy", null);
    }
  };
  el.addEventListener("click", click);
  inst.off.push(() => el.removeEventListener("click", click));
  inst.update = (n) => {
    inst.cur = n;
    paint();
  };
  inst.refresh = paint;
  inst.update(c);
  return inst;
}
