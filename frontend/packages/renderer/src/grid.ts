import { base, button, fieldOf } from "./components.ts";
import type { PComponent } from "./model.ts";
import type { Value } from "./store.ts";
import type { Inst, RendererApi } from "./types.ts";
import { placeChildren } from "./writes.ts";

const str = (v: Value): string => (v === null ? "" : String(v));

interface RowInst {
  key: string;
  tr: HTMLTableRowElement;
  cells: HTMLTableCellElement[];
  actions: Inst[];
  off: (() => void)[];
  title: string;
}

/** Roving-focus grid. Every cell and every row action is one navigation stop; exactly one stop is in the tab order. */
export function table(r: RendererApi, c: PComponent): Inst {
  const entity = c.binding?.entity ?? "";
  const rows = r.rt.store.rows(entity);
  const wrap = r.dom.el("div");
  const toolbar = r.dom.el("div", "toolbar");
  const filterLabel = r.dom.el("label");
  const filter = r.dom.el("input");
  const fid = `vsf-filter-${entity}`;
  r.dom.attr(filter, "id", fid);
  r.dom.attr(filter, "type", "text");
  r.dom.attr(filterLabel, "for", fid);
  r.dom.text(filterLabel, "Filter rows");
  r.dom.append(toolbar, filterLabel);
  r.dom.append(toolbar, filter);
  const scroller = r.dom.el("div", "grid-wrap");
  const tbl = r.dom.el("table");
  r.dom.attr(tbl, "role", "grid");
  const thead = r.dom.el("thead");
  const headRow = r.dom.el("tr");
  const tbody = r.dom.el("tbody");
  r.dom.append(thead, headRow);
  r.dom.append(tbl, thead);
  r.dom.append(tbl, tbody);
  r.dom.append(scroller, tbl);
  r.dom.append(wrap, toolbar);
  r.dom.append(wrap, scroller);
  const inst = base(r, c, wrap);

  let cols: PComponent[] = [];
  let acts: PComponent[] = [];
  const live = new Map<string, RowInst>();
  let needle = "";
  let sort: { field: string; dir: 1 | -1 } | null = null;
  let active: { row: string | null; stop: number } = { row: null, stop: 0 };
  let lastVisible: string[] = [];

  const headStops = (): HTMLElement[] => [...headRow.querySelectorAll<HTMLElement>("button[data-col]")];
  const stopsOf = (row: string | null): HTMLElement[] => {
    if (row === null) return headStops();
    const ri = live.get(row);
    if (!ri) return [];
    const out: HTMLElement[] = [...ri.cells];
    for (const a of ri.actions) out.push(a.el);
    return out;
  };
  const visible = (): string[] => rows.order().filter((k) => !live.get(k)?.tr.hidden);

  const setActive = (row: string | null, stop: number, focus: boolean): void => {
    const stops = stopsOf(row);
    if (!stops.length) return;
    const i = Math.max(0, Math.min(stops.length - 1, stop));
    for (const s of tbl.querySelectorAll<HTMLElement>('[tabindex="0"][data-stop]')) r.dom.attr(s, "tabindex", "-1");
    const target = stops[i] as HTMLElement;
    r.dom.attr(target, "tabindex", "0");
    active = { row, stop: i };
    if (focus) target.focus({ preventScroll: false });
  };

  const rowText = (key: string): string => {
    const first = cols[0]?.binding?.field;
    return first ? str(rows.cell(key, first)) : key;
  };

  const rowTextMatches = (key: string): boolean => {
    if (!needle) return true;
    const row = rows.row(key);
    return row ? Object.values(row).some((v) => str(v).toLowerCase().includes(needle)) : false;
  };

  const buildRow = (key: string): RowInst => {
    const tr = r.dom.el("tr");
    const ri: RowInst = { key, tr, cells: [], actions: [], off: [], title: rowText(key) };
    r.dom.attr(tr, "data-vsf-row", key);
    cols.forEach((col, i) => {
      const td = r.dom.el("td");
      r.dom.attr(td, "role", "gridcell");
      r.dom.attr(td, "tabindex", "-1");
      r.dom.attr(td, "data-stop", "");
      r.dom.attr(td, "data-vsf-id", `cell:${key}/${col.binding?.field}`);
      const field = col.binding?.field ?? "";
      r.dom.text(td, str(rows.cell(key, field)));
      ri.off.push(
        rows.onCell(key, field, (v) => {
          r.dom.text(td, str(v));
          if (i === 0) {
            ri.title = str(v);
            for (const a of ri.actions) a.refresh?.();
          }
        }),
      );
      r.dom.append(tr, td);
      ri.cells.push(td);
    });
    if (acts.length) {
      const td = r.dom.el("td");
      r.dom.attr(td, "role", "gridcell");
      const holder = r.dom.el("div", "row-actions");
      r.dom.append(td, holder);
      r.dom.append(tr, td);
      for (const a of acts) {
        const b = button(r, a, key, () => ri.title);
        r.dom.attr(b.el, "tabindex", "-1");
        r.dom.attr(b.el, "data-stop", "");
        r.dom.append(holder, b.el);
        ri.actions.push(b);
      }
    }
    return ri;
  };

  const disposeRow = (ri: RowInst): void => {
    for (const f of ri.off) f();
    for (const a of ri.actions) a.dispose();
  };

  const applyFilter = (): void => {
    for (const [k, ri] of live) {
      const show = rowTextMatches(k);
      if (ri.tr.hidden === !show) continue;
      r.dom.stats.writes++;
      ri.tr.hidden = !show;
    }
  };

  const syncRows = (): void => {
    const mem = active;
    const hadFocus = !!r.root.activeElement && tbl.contains(r.root.activeElement);
    const order = rows.order();
    const idx = mem.row ? lastVisible.indexOf(mem.row) : -1;
    for (const [k, ri] of live) {
      if (!rows.row(k)) {
        disposeRow(ri);
        r.dom.remove(ri.tr);
        live.delete(k);
      }
    }
    const trs: HTMLElement[] = [];
    for (const k of order) {
      let ri = live.get(k);
      if (!ri) {
        ri = buildRow(k);
        live.set(k, ri);
        ri.tr.hidden = !rowTextMatches(k);
      }
      trs.push(ri.tr);
    }
    placeChildren(r.dom, tbody, trs);
    lastVisible = visible();
    if (mem.row !== null && !live.has(mem.row)) {
      const now = visible();
      const next = now[Math.min(Math.max(idx, 0), now.length - 1)] ?? null;
      setActive(next, mem.stop, hadFocus);
    }
  };

  const buildHead = (): void => {
    for (const ch of [...headRow.children]) r.dom.remove(ch as ChildNode);
    for (const col of cols) {
      const th = r.dom.el("th");
      r.dom.attr(th, "scope", "col");
      r.dom.attr(th, "role", "columnheader");
      const field = col.binding?.field ?? "";
      const b = r.dom.el("button");
      r.dom.attr(b, "type", "button");
      r.dom.attr(b, "data-col", field);
      r.dom.attr(b, "data-stop", "");
      r.dom.attr(b, "tabindex", "-1");
      r.dom.attr(b, "data-vsf-id", `head:${field}`);
      r.dom.text(b, fieldOf(r, col)?.name ?? field);
      b.addEventListener("click", () => {
        sort = sort?.field === field ? { field, dir: sort.dir === 1 ? -1 : 1 } : { field, dir: 1 };
        rows.sortBy(field, sort.dir);
        for (const h of headRow.querySelectorAll("th")) {
          const f = h.querySelector("button")?.getAttribute("data-col");
          r.dom.attr(h, "aria-sort", f === sort.field ? (sort.dir === 1 ? "ascending" : "descending") : null);
        }
        r.announcer.say(`Sorted by ${fieldOf(r, col)?.name ?? field}, ${sort.dir === 1 ? "ascending" : "descending"}`);
      });
      r.dom.append(th, b);
      r.dom.append(headRow, th);
    }
    if (acts.length) {
      const th = r.dom.el("th");
      r.dom.attr(th, "scope", "col");
      r.dom.attr(th, "role", "columnheader");
      r.dom.text(th, "Actions");
      r.dom.append(headRow, th);
    }
  };

  const move = (row: string | null, stop: number): void => setActive(row, stop, true);

  const onKey = (e: KeyboardEvent): void => {
    const target = e.target as HTMLElement;
    if (!target.hasAttribute("data-stop")) return;
    const rtl = r.rtl;
    const vis = visible();
    const cur = active.row;
    const ri = cur === null ? -1 : vis.indexOf(cur);
    const stops = stopsOf(cur).length;
    const last = Math.max(0, stops - 1);
    let handled = true;
    switch (e.key) {
      case "ArrowDown":
        if (cur === null) move(vis[0] ?? null, active.stop);
        else if (ri < vis.length - 1) move(vis[ri + 1] as string, active.stop);
        break;
      case "ArrowUp":
        if (cur !== null) move(ri > 0 ? (vis[ri - 1] as string) : null, active.stop);
        break;
      case "ArrowRight":
        move(cur, active.stop + (rtl ? -1 : 1));
        break;
      case "ArrowLeft":
        move(cur, active.stop + (rtl ? 1 : -1));
        break;
      case "Home":
        if (e.ctrlKey) move(vis[0] ?? null, 0);
        else move(cur, 0);
        break;
      case "End":
        if (e.ctrlKey) move(vis[vis.length - 1] ?? null, last);
        else move(cur, last);
        break;
      case "PageDown":
        move(vis[Math.min(vis.length - 1, Math.max(0, ri) + 10)] ?? cur, active.stop);
        break;
      case "PageUp":
        move(ri - 10 <= 0 ? (vis[0] ?? null) : (vis[ri - 10] as string), active.stop);
        break;
      default:
        handled = false;
    }
    if (handled) e.preventDefault();
  };
  const onFocusIn = (e: FocusEvent): void => {
    const t = (e.target as HTMLElement).closest<HTMLElement>("[data-stop]");
    if (!t) return;
    const tr = t.closest("tr");
    const row = tr && tr.parentElement === tbody ? (tr.getAttribute("data-vsf-row") as string) : null;
    const stops = stopsOf(row);
    const i = stops.indexOf(t);
    if (i >= 0 && (active.row !== row || active.stop !== i)) setActive(row, i, false);
  };
  tbl.addEventListener("keydown", onKey);
  tbl.addEventListener("focusin", onFocusIn);
  const onFilter = (): void => {
    needle = filter.value.trim().toLowerCase();
    applyFilter();
    const vis = visible();
    lastVisible = vis;
    if (active.row !== null && !vis.includes(active.row)) setActive(vis[0] ?? null, active.stop, false);
    r.announcer.say(`${vis.length} of ${rows.size} rows shown`);
  };
  filter.addEventListener("input", onFilter);
  inst.off.push(() => {
    tbl.removeEventListener("keydown", onKey);
    tbl.removeEventListener("focusin", onFocusIn);
    filter.removeEventListener("input", onFilter);
  });
  inst.off.push(rows.onList(syncRows));
  inst.off.push(() => {
    for (const ri of live.values()) disposeRow(ri);
    live.clear();
  });

  inst.update = (n) => {
    r.dom.attr(tbl, "aria-label", n.accessibility.name ?? n.id);
    r.dom.attr(tbl, "data-vsf-id", `grid:${entity}`);
    const kids = n.children.map((id) => r.component(id)).filter((x): x is PComponent => !!x);
    const nc = kids.filter((k) => k.kind === "cell");
    const na = kids.filter((k) => k.kind === "button");
    const same = JSON.stringify([nc, na]) === JSON.stringify([cols, acts]);
    if (!same) {
      for (const ri of live.values()) {
        disposeRow(ri);
        r.dom.remove(ri.tr);
      }
      live.clear();
      cols = nc;
      acts = na;
      buildHead();
      syncRows();
      setActive(null, 0, false);
    }
    for (const ri of live.values()) for (const a of ri.actions) a.refresh?.();
  };
  inst.refresh = () => {
    for (const ri of live.values()) for (const a of ri.actions) a.refresh?.();
  };
  inst.activeRow = () => active.row;
  inst.update(c);
  return inst;
}
