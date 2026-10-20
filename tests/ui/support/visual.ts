import type { Page } from "@playwright/test";

export type Kind = "overflow" | "clipping" | "overlap" | "contrast" | "unlabeled" | "unresolved";

export interface Finding {
  kind: Kind;
  where: string;
  detail: string;
}

/** Runs entirely in the page, across open shadow roots, and reports every defect it can prove from computed layout. */
export async function audit(page: Page): Promise<Finding[]> {
  return page.evaluate((): Finding[] => {
    const out: { kind: string; where: string; detail: string }[] = [];
    const all: Element[] = [];
    const walk = (root: ParentNode): void => {
      for (const el of Array.from(root.querySelectorAll("*"))) {
        all.push(el);
        if (el.shadowRoot) walk(el.shadowRoot);
      }
    };
    walk(document);

    const name = (el: Element): string => {
      const tag = el.tagName.toLowerCase();
      const id = el.id ? `#${el.id}` : "";
      const cls = typeof el.className === "string" && el.className ? `.${el.className.split(/\s+/)[0]}` : "";
      const label = (el.getAttribute("aria-label") ?? el.textContent ?? "").trim().slice(0, 24);
      return `${tag}${id}${cls}${label ? `"${label}"` : ""}`;
    };
    const visible = (el: Element): boolean => {
      const r = el.getBoundingClientRect();
      if (r.width <= 1 && r.height <= 1) return false;
      for (let n: Element | null = el; n; n = n.parentElement ?? (n.getRootNode() as ShadowRoot).host ?? null) {
        const s = getComputedStyle(n);
        if (s.display === "none" || s.visibility === "hidden" || s.opacity === "0") return false;
      }
      return true;
    };
    const parent = (el: Element): Element | null => el.parentElement ?? (el.getRootNode() as ShadowRoot).host ?? null;

    const vw = document.documentElement.clientWidth;
    if (document.documentElement.scrollWidth > vw + 1) {
      out.push({
        kind: "overflow",
        where: "document",
        detail: `scrollWidth ${document.documentElement.scrollWidth} > ${vw}`,
      });
    }
    const scrolls = (el: Element): boolean => {
      const s = getComputedStyle(el);
      return /(auto|scroll)/.test(s.overflowX);
    };
    for (const el of all) {
      if (!visible(el)) continue;
      const r = el.getBoundingClientRect();
      const offscreen = r.right <= 0 || r.left >= vw;
      if (!offscreen && (r.right > vw + 1 || r.left < -1)) {
        let held = false;
        for (let p = parent(el); p; p = parent(p)) {
          if (scrolls(p)) {
            held = true;
            break;
          }
        }
        if (!held)
          out.push({
            kind: "overflow",
            where: name(el),
            detail: `x ${r.left.toFixed(0)}..${r.right.toFixed(0)} outside ${vw}`,
          });
      }
      const s = getComputedStyle(el);
      if ((s.overflowX === "hidden" || s.overflowX === "clip") && el.scrollWidth > el.clientWidth + 1) {
        out.push({ kind: "clipping", where: name(el), detail: `content ${el.scrollWidth} > box ${el.clientWidth}` });
      }
      if (s.textOverflow === "ellipsis" && el.scrollWidth > el.clientWidth + 1) {
        out.push({ kind: "clipping", where: name(el), detail: "text is truncated with an ellipsis" });
      }
    }

    const CONTROLS =
      "button, a[href], input:not([type=hidden]), select, textarea, [role=button], [role=tab], [role=menuitem]";
    const controls = all.filter((e) => e.matches(CONTROLS) && visible(e));
    const shown = (el: Element): { left: number; right: number; top: number; bottom: number } => {
      const r = el.getBoundingClientRect();
      let box = { left: r.left, right: r.right, top: r.top, bottom: r.bottom };
      for (let p = parent(el); p; p = parent(p)) {
        const s = getComputedStyle(p);
        if (s.overflowX === "visible" && s.overflowY === "visible") continue;
        const c = p.getBoundingClientRect();
        box = {
          left: Math.max(box.left, c.left),
          right: Math.min(box.right, c.right),
          top: Math.max(box.top, c.top),
          bottom: Math.min(box.bottom, c.bottom),
        };
      }
      return box;
    };
    for (let i = 0; i < controls.length; i++) {
      const a = controls[i] as Element;
      const ra = shown(a);
      for (let j = i + 1; j < controls.length; j++) {
        const b = controls[j] as Element;
        if (a.contains(b) || b.contains(a)) continue;
        const rb = shown(b);
        const w = Math.min(ra.right, rb.right) - Math.max(ra.left, rb.left);
        const h = Math.min(ra.bottom, rb.bottom) - Math.max(ra.top, rb.top);
        if (w > 2 && h > 2)
          out.push({ kind: "overlap", where: `${name(a)} / ${name(b)}`, detail: `${w.toFixed(0)}x${h.toFixed(0)} px` });
      }
    }

    const labelled = (el: Element): boolean => {
      if ((el.getAttribute("aria-label") ?? "").trim()) return true;
      const by = el.getAttribute("aria-labelledby");
      const root = el.getRootNode() as Document | ShadowRoot;
      if (by) {
        const text = by
          .split(/\s+/)
          .map((id) => root.getElementById(id)?.textContent ?? "")
          .join("")
          .trim();
        if (text) return true;
      }
      if ((el.getAttribute("title") ?? "").trim()) return true;
      if (el instanceof HTMLInputElement && ["button", "submit", "reset"].includes(el.type) && el.value.trim())
        return true;
      if (el instanceof HTMLInputElement || el instanceof HTMLSelectElement || el instanceof HTMLTextAreaElement) {
        if (el.labels && Array.from(el.labels).some((l) => (l.textContent ?? "").trim())) return true;
        return false;
      }
      const visibleText = Array.from(el.childNodes)
        .map((n) =>
          n.nodeType === 1 && (n as Element).getAttribute("aria-hidden") === "true" ? "" : (n.textContent ?? ""),
        )
        .join("")
        .trim();
      return visibleText.length > 0;
    };
    for (const el of controls)
      if (!labelled(el)) out.push({ kind: "unlabeled", where: name(el), detail: "no accessible name" });

    const parse = (c: string): [number, number, number, number] | null => {
      let m = /^rgba?\(([^)]+)\)$/.exec(c);
      if (m?.[1]) {
        const p = m[1]
          .split(/[ ,/]+/)
          .filter(Boolean)
          .map(Number);
        return [p[0] ?? 0, p[1] ?? 0, p[2] ?? 0, p[3] ?? 1];
      }
      m = /^color\(srgb ([^)]+)\)$/.exec(c);
      if (m?.[1]) {
        const p = m[1].split(/[ /]+/).filter(Boolean).map(Number);
        return [(p[0] ?? 0) * 255, (p[1] ?? 0) * 255, (p[2] ?? 0) * 255, p[3] ?? 1];
      }
      return null;
    };
    const lum = (c: number[]): number => {
      const f = (v: number): number => {
        const s = v / 255;
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * f(c[0] ?? 0) + 0.7152 * f(c[1] ?? 0) + 0.0722 * f(c[2] ?? 0);
    };
    const over = (top: number[], under: number[]): number[] => {
      const a = top[3] ?? 1;
      return [0, 1, 2].map((i) => (top[i] ?? 0) * a + (under[i] ?? 0) * (1 - a)).concat([1]);
    };
    const backdrop = (el: Element): number[] | null => {
      const layers: number[][] = [];
      for (let n: Element | null = el; n; n = parent(n)) {
        const bg = getComputedStyle(n).backgroundColor;
        const p = parse(bg);
        if (!p) return null;
        if (p[3] > 0) layers.push(p);
        if (p[3] >= 1) break;
      }
      let acc: number[] = parse(getComputedStyle(document.body).backgroundColor) ?? [255, 255, 255, 1];
      if ((acc[3] ?? 1) < 1) acc = over(acc, [255, 255, 255, 1]);
      for (const l of layers.reverse()) acc = over(l, acc);
      return acc;
    };
    for (const el of all) {
      if (!visible(el)) continue;
      const own = Array.from(el.childNodes)
        .filter((n) => n.nodeType === 3)
        .map((n) => n.textContent ?? "")
        .join("")
        .trim();
      if (!own) continue;
      const s = getComputedStyle(el);
      const fg = parse(s.color);
      const bg = backdrop(el);
      if (!fg || !bg) {
        out.push({ kind: "unresolved", where: name(el), detail: `color ${s.color}` });
        continue;
      }
      const f = over(fg, bg);
      const l1 = lum(f);
      const l2 = lum(bg);
      const ratio = (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
      const px = Number.parseFloat(s.fontSize);
      const large = px >= 24 || (px >= 18.66 && Number(s.fontWeight) >= 700);
      const need = large ? 3 : 4.5;
      if (ratio < need) out.push({ kind: "contrast", where: name(el), detail: `${ratio.toFixed(2)} < ${need}` });
    }
    return out as Finding[];
  });
}

declare global {
  interface Window {
    __shift?: number;
    __shiftObs?: PerformanceObserver;
  }
}

/** Must run before navigation; the browser reports shifts only to observers that exist when they happen. */
export async function watchShifts(page: Page): Promise<void> {
  await page.addInitScript(() => {
    window.__shift = 0;
    const o = new PerformanceObserver((list) => {
      for (const e of list.getEntries()) {
        const s = e as PerformanceEntry & { value: number; hadRecentInput: boolean };
        if (!s.hadRecentInput) window.__shift = (window.__shift ?? 0) + s.value;
      }
    });
    o.observe({ type: "layout-shift", buffered: true });
    window.__shiftObs = o;
  });
}

/** Cumulative shift from now until `settleMs` later, counted after what already happened is discarded. */
export async function shiftAfterReady(page: Page, settleMs = 400): Promise<number> {
  await page.evaluate(() => {
    window.__shiftObs?.takeRecords();
    window.__shift = 0;
  });
  await page.waitForTimeout(settleMs);
  return page.evaluate(() => {
    let sum = window.__shift ?? 0;
    for (const e of window.__shiftObs?.takeRecords() ?? []) {
      const s = e as PerformanceEntry & { value: number; hadRecentInput: boolean };
      if (!s.hadRecentInput) sum += s.value;
    }
    return sum;
  });
}

export function summarize(findings: Finding[]): string {
  return findings.map((f) => `${f.kind}: ${f.where} (${f.detail})`).join("\n");
}

/** Names every platform font the page actually painted with that is not one of its own embedded faces. */
export async function systemFontsUsed(page: Page): Promise<string[]> {
  const cdp = await page.context().newCDPSession(page);
  try {
    await cdp.send("DOM.enable");
    await cdp.send("CSS.enable");
    const { root } = await cdp.send("DOM.getDocument", { depth: -1, pierce: true });
    const used = new Set<string>();
    const stack = [root];
    while (stack.length) {
      const node = stack.pop();
      if (!node) continue;
      for (const c of node.children ?? []) stack.push(c);
      for (const r of node.shadowRoots ?? []) stack.push(r);
      if (node.nodeType !== 1 || node.nodeName === "STYLE" || node.nodeName === "SCRIPT" || node.nodeName === "TITLE")
        continue;
      const { fonts } = await cdp.send("CSS.getPlatformFontsForNode", { nodeId: node.nodeId });
      for (const f of fonts) if (!f.isCustomFont) used.add(`${f.familyName} in <${node.nodeName.toLowerCase()}>`);
    }
    return [...used].sort();
  } finally {
    await cdp.detach();
  }
}
