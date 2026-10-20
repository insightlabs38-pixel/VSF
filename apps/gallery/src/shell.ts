import { STATUS_GLYPH, type StatusName, tokensCss } from "@vsf/design-system";
import type { Look } from "./scene.ts";

export type SectionId = "interface" | "versions" | "permissions" | "environments" | "storage" | "activity";

export const SECTIONS: { id: SectionId; label: string }[] = [
  { id: "interface", label: "Interface" },
  { id: "versions", label: "Versions" },
  { id: "permissions", label: "Permissions" },
  { id: "environments", label: "Execution environments" },
  { id: "storage", label: "Storage and cache" },
  { id: "activity", label: "Activity" },
];

const APP = { name: "Orders console", origin: "http://127.0.0.1:4173", variant: "default, en-US" };

const css = `
:host{display:block;min-block-size:100vh;background:var(--vsf-bg);color:var(--vsf-text);font-size:var(--vsf-font);line-height:1.45}
*,*::before,*::after{box-sizing:border-box}
a,button,select{font:inherit;color:inherit}
.skip{position:absolute;inset-inline-start:-999px;inset-block-start:0;background:var(--vsf-accent);color:var(--vsf-on-accent);padding:var(--vsf-pad-y) var(--vsf-pad-x);z-index:3}
.skip:focus{inset-inline-start:0}
:focus-visible{outline:3px solid var(--vsf-focus);outline-offset:2px}
header.top{display:flex;flex-wrap:wrap;align-items:center;gap:var(--vsf-gap);padding:var(--vsf-pad-y) var(--vsf-pad-x);background:var(--vsf-surface);border-block-end:1px solid var(--vsf-border)}
.brand{font-weight:600;letter-spacing:.02em;padding-inline-end:var(--vsf-gap);border-inline-end:1px solid var(--vsf-border)}
nav.crumbs ol{display:flex;flex-wrap:wrap;gap:.35em;list-style:none;margin:0;padding:0;align-items:center}
nav.crumbs li:last-child{display:flex;flex-wrap:wrap;align-items:center;gap:.5em}
nav.crumbs li+li::before{content:"›";margin-inline-end:.35em;color:var(--vsf-muted)}
nav.crumbs [aria-current]{font-weight:600}
.spacer{flex:1 1 auto}
.layout{display:grid;grid-template-columns:minmax(16ch,22ch) minmax(0,1fr);min-block-size:calc(100vh - 3rem)}
nav.side{background:var(--vsf-surface);border-inline-end:1px solid var(--vsf-border);padding:var(--vsf-gap)}
nav.side ul{list-style:none;margin:0;padding:0;display:grid;gap:4px}
nav.side a{display:flex;align-items:center;min-block-size:var(--vsf-target);padding:var(--vsf-pad-y) var(--vsf-pad-x);border-radius:6px;text-decoration:none;border:1px solid transparent}
nav.side a:hover{background:var(--vsf-surface-2)}
nav.side a[aria-current="page"]{background:var(--vsf-accent-soft);border-color:var(--vsf-accent);font-weight:600}
main{padding:calc(var(--vsf-gap) * 1.5);display:grid;gap:calc(var(--vsf-gap) * 1.5);align-content:start;min-inline-size:0}
h1{font-size:1.5em;margin:0}
h2{font-size:1.15em;margin:0 0 var(--vsf-gap)}
p{margin:0}
.note{color:var(--vsf-muted);margin-block-start:var(--vsf-gap)}
.card{background:var(--vsf-surface);border:1px solid var(--vsf-border);border-radius:8px;padding:calc(var(--vsf-gap) * 1.25)}
.banner{display:flex;flex-wrap:wrap;align-items:center;gap:var(--vsf-gap);border-radius:8px;padding:var(--vsf-pad-y) var(--vsf-pad-x);border:1px solid var(--vsf-border)}
.chip{display:inline-flex;align-items:center;gap:.4em;border-radius:999px;padding:2px .75em;font-weight:600;font-size:.9em;white-space:nowrap;border:1px solid currentColor}
.chip[data-status]{color:var(--chip);background:var(--chip-bg)}
.chip[data-status="success"]{--chip:var(--vsf-success);--chip-bg:var(--vsf-success-bg)}
.chip[data-status="caution"]{--chip:var(--vsf-caution);--chip-bg:var(--vsf-caution-bg)}
.chip[data-status="warning"]{--chip:var(--vsf-warning);--chip-bg:var(--vsf-warning-bg)}
.chip[data-status="danger"]{--chip:var(--vsf-danger);--chip-bg:var(--vsf-danger-bg)}
.chip[data-status="neutral"]{--chip:var(--vsf-text);--chip-bg:var(--vsf-accent-soft)}
.banner[data-status="success"]{background:var(--vsf-success-bg)}
.banner[data-status="danger"]{background:var(--vsf-danger-bg)}
button.btn{min-block-size:var(--vsf-target);min-inline-size:var(--vsf-target);padding:var(--vsf-pad-y) var(--vsf-pad-x);background:var(--vsf-bg);color:var(--vsf-text);border:1px solid var(--vsf-control-border);border-radius:6px;cursor:pointer}
button.btn[aria-pressed="true"]{background:var(--vsf-accent);color:var(--vsf-on-accent);border-color:var(--vsf-accent)}
button.btn[disabled]{opacity:1;color:var(--vsf-muted);border-style:dashed;cursor:not-allowed}
.scroll{overflow-x:auto}
table{border-collapse:collapse;inline-size:100%;min-inline-size:36ch}
caption{text-align:start;font-weight:600;padding-block-end:var(--vsf-pad-y)}
th,td{text-align:start;padding:var(--vsf-pad-y) var(--vsf-pad-x);border-block-end:1px solid var(--vsf-border);vertical-align:top}
th{color:var(--vsf-muted);font-weight:600;font-size:.9em}
code{font-family:"VSF Mono",monospace;font-size:.88em;overflow-wrap:anywhere}
.grid2{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,28ch),1fr));gap:var(--vsf-gap)}
dl{margin:0;display:grid;grid-template-columns:max-content 1fr;gap:4px var(--vsf-gap)}
dt{color:var(--vsf-muted)}
dd{margin:0;min-inline-size:0;overflow-wrap:anywhere}
.reference{font-size:.9em;color:var(--vsf-muted)}
@media (max-width:760px){.layout{grid-template-columns:minmax(0,1fr)}nav.side{border-inline-end:0;border-block-end:1px solid var(--vsf-border)}nav.side ul{grid-template-columns:repeat(auto-fit,minmax(16ch,1fr))}.brand{border-inline-end:0}}
`;

type Tone = StatusName | "neutral";

type Child = Node | string | null | undefined | false;

function h<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Record<string, string | boolean | undefined> = {},
  ...kids: Child[]
): HTMLElementTagNameMap[K] {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v === undefined || v === false) continue;
    el.setAttribute(k, v === true ? "" : v);
  }
  for (const k of kids) if (k) el.append(k);
  return el;
}

function chip(status: Tone, text: string): HTMLElement {
  const glyph = status === "neutral" ? "◆" : STATUS_GLYPH[status];
  return h("span", { class: "chip", "data-status": status }, h("span", { "aria-hidden": "true" }, glyph), text);
}

function table(caption: string, head: string[], rows: Child[][]): HTMLElement {
  return h(
    "div",
    { class: "scroll", role: "region", "aria-label": caption, tabindex: "0" },
    h(
      "table",
      {},
      h("caption", {}, caption),
      h("thead", {}, h("tr", {}, ...head.map((x) => h("th", { scope: "col" }, x)))),
      h("tbody", {}, ...rows.map((r) => h("tr", {}, ...r.map((c) => h("td", {}, c))))),
    ),
  );
}

const code = (s: string): HTMLElement => h("code", {}, s);

function interfaceSection(): Node[] {
  return [
    h(
      "div",
      { class: "banner", "data-status": "success", role: "status" },
      chip("success", "Compatible"),
      "The source looks the way this interface was built for. Last checked two minutes ago.",
    ),
    h(
      "div",
      { class: "card" },
      h("h2", {}, "This interface"),
      h(
        "dl",
        {},
        h("dt", {}, "Application"),
        h("dd", {}, `${APP.name} (${APP.origin})`),
        h("dt", {}, "Variant"),
        h("dd", {}, APP.variant),
        h("dt", {}, "Version"),
        h("dd", {}, "1.2.0, stable channel"),
        h("dt", {}, "Compiled from"),
        h("dd", {}, code("sha256:29292929…2929")),
      ),
      h(
        "p",
        { class: "note" },
        "A pin keeps this exact version until you unpin it; a revoked version is never revived by a pin.",
      ),
      h(
        "div",
        { style: "margin-block-start:var(--vsf-gap)" },
        h("button", { class: "btn", type: "button", "aria-pressed": "true" }, "Pinned to 1.2.0"),
      ),
    ),
    table(
      "Operations in this interface",
      ["Operation", "Effect", "Availability"],
      [
        ["Open order", "Read only", chip("success", "Available")],
        ["Submit order", "State change, not demonstrated", chip("caution", "Needs review: effect unverified")],
        ["Delete order", "Destructive", chip("warning", "Conflict: witnesses disagree")],
        ["Export orders", "Read only", chip("danger", "Unusable: disabled by the source")],
      ],
    ),
  ];
}

function versionsSection(): Node[] {
  return [
    table(
      "Version history",
      ["Version", "Channel", "Compatibility", "Released", ""],
      [
        [
          "1.2.0",
          "stable",
          chip("success", "Compatible"),
          "2026-10-02",
          h("button", { class: "btn", type: "button" }, "View changes"),
        ],
        [
          "1.1.0",
          "stable",
          chip("caution", "Degraded: one field unreadable"),
          "2026-08-19",
          h("button", { class: "btn", type: "button" }, "View changes"),
        ],
        [
          "1.0.1",
          "retired",
          chip("danger", "Blocked: layout changed"),
          "2026-06-30",
          h("button", { class: "btn", type: "button", disabled: true }, "Pin unavailable"),
        ],
        [
          "1.0.0",
          "revoked",
          chip("danger", "Revoked: security fix"),
          "2026-06-01",
          h("button", { class: "btn", type: "button", disabled: true }, "Pin unavailable"),
        ],
      ],
    ),
    h(
      "div",
      { class: "card" },
      h("h2", {}, "Changes from 1.1.0 to 1.2.0"),
      h(
        "dl",
        {},
        h("dt", {}, "Additive"),
        h("dd", {}, "2 changes"),
        h("dt", {}, "Breaking"),
        h("dd", {}, "none"),
        h("dt", {}, "Review needed"),
        h("dd", {}, "no"),
        h("dt", {}, "Tests selected"),
        h("dd", {}, "14 of 220"),
      ),
    ),
  ];
}

function permissionsSection(): Node[] {
  return [
    table(
      "Sites this extension may read",
      ["Origin", "Scopes", "Granted"],
      [[code(APP.origin), "page.observe, page.present", "2026-10-18"]],
    ),
    h(
      "p",
      { class: "note" },
      "Pairing with the local companion lets it receive your requests. It does not give the companion, or anything else, access to your account on a site.",
    ),
  ];
}

function environmentsSection(): Node[] {
  return [
    table(
      "Execution environments",
      ["Environment", "State", "Authorization"],
      [
        ["Browser extension", chip("success", "Available"), "Granted for the sites above"],
        ["Local companion", chip("success", "Paired"), "Pairing only; no site access"],
        ["Hosted sandbox", chip("danger", "Unavailable: not signed in"), "None"],
        ["Headless worker", chip("danger", "Unavailable: not configured"), "None"],
        ["Private deployment", chip("danger", "Unavailable: not configured"), "None"],
      ],
    ),
  ];
}

function storageSection(): Node[] {
  return [
    table(
      "Storage and cache",
      ["Item", "Used", "Retention"],
      [
        ["Compiled interfaces", "1.8 MB", "Last 3 versions"],
        ["Pinned versions", "1 of 1", "Until unpinned"],
        ["Extended retention", chip("danger", "Unavailable: needs a plan"), "Not enabled"],
      ],
    ),
  ];
}

function activitySection(): Node[] {
  return [
    table(
      "Recent activity",
      ["When", "What", "Outcome"],
      [
        ["10:42", "Refresh from source", chip("success", "Verified")],
        ["10:31", "Submit order SO-00013", chip("caution", "Unknown: no receipt from the source")],
        ["10:12", "Open order SO-00007", chip("success", "Verified")],
      ],
    ),
  ];
}

const BODY: Record<SectionId, () => Node[]> = {
  interface: interfaceSection,
  versions: versionsSection,
  permissions: permissionsSection,
  environments: environmentsSection,
  storage: storageSection,
  activity: activitySection,
};

export function mountShell(host: HTMLElement, look: Look, section: SectionId, href: (s: SectionId) => string): void {
  host.setAttribute("data-scheme", look.scheme);
  host.setAttribute("data-accent", "blue");
  host.setAttribute("data-density", look.density);
  const root = host.shadowRoot ?? host.attachShadow({ mode: "open" });
  root.replaceChildren();
  const style = h("style");
  style.textContent = `${tokensCss()}\n${css}`;
  const label = SECTIONS.find((s) => s.id === section)?.label ?? "";
  const main = h(
    "main",
    { id: "main", tabindex: "-1" },
    h("h1", {}, label),
    h("p", { class: "reference" }, "Reference data. Nothing here is connected to a site."),
    ...BODY[section](),
  );
  root.append(
    style,
    h("a", { class: "skip", href: "#main" }, "Skip to content"),
    h(
      "header",
      { class: "top" },
      h("span", { class: "brand" }, "Varifold"),
      h(
        "nav",
        { class: "crumbs", "aria-label": "Location" },
        h(
          "ol",
          {},
          h("li", {}, "Workspace"),
          h("li", {}, `${APP.name} (${APP.origin})`),
          h(
            "li",
            {},
            h("span", { "aria-current": "location" }, "Interface 1.2.0"),
            " ",
            chip("success", "stable"),
            " ",
            chip("neutral", "pinned"),
          ),
        ),
      ),
    ),
    h(
      "div",
      { class: "layout" },
      h(
        "nav",
        { class: "side", "aria-label": "Sections" },
        h(
          "ul",
          {},
          ...SECTIONS.map((s) =>
            h("li", {}, h("a", { href: href(s.id), ...(s.id === section ? { "aria-current": "page" } : {}) }, s.label)),
          ),
        ),
      ),
      main,
    ),
  );
}
