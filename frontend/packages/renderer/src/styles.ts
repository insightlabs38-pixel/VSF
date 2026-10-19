import { tokensCss } from "@vsf/design-system";

/** Component CSS: logical properties only, so one stylesheet serves both directions. */
export function componentCss(): string {
  return `${tokensCss()}
:host{display:block;color:var(--vsf-text);background:var(--vsf-bg);font:var(--vsf-font)/1.45 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;color-scheme:light dark;text-align:start}
:host([data-scheme="light"]){color-scheme:light}
:host([data-scheme="dark"]){color-scheme:dark}
*,*::before,*::after{box-sizing:border-box}
.app{display:grid;gap:var(--vsf-gap);padding:var(--vsf-gap);grid-template-columns:minmax(0,1fr)}
.app[data-nav="side"]{grid-template-columns:max-content minmax(0,1fr);align-items:start}
.app[data-nav="side"] .nav-host{grid-column:1}
.app[data-nav="side"] main{grid-column:2}
.app[data-nav="hidden"] .nav-host{display:none}
.sr-only{position:absolute;inline-size:1px;block-size:1px;margin:-1px;padding:0;overflow:hidden;clip-path:inset(50%);white-space:nowrap;border:0}
.skip{position:absolute;inset-inline-start:-9999px}
.skip:focus{position:static}
main:focus{outline:none}
h2{margin:0 0 var(--vsf-gap);font-size:1.25em}
nav ul{display:flex;flex-wrap:wrap;gap:var(--vsf-gap);list-style:none;margin:0;padding:0}
.app[data-nav="side"] nav ul{flex-direction:column}
a.link{display:inline-flex;align-items:center;min-block-size:var(--vsf-target);padding-inline:var(--vsf-pad-x);color:var(--vsf-accent);text-decoration:underline;border-radius:6px}
a.link[aria-current="page"]{background:var(--vsf-accent-soft);font-weight:600}
form{display:grid;gap:var(--vsf-gap);max-inline-size:44em}
.field{display:grid;gap:4px}
.field label{font-weight:600}
input[type=text],select{min-block-size:var(--vsf-target);padding:var(--vsf-pad-y) var(--vsf-pad-x);border:1px solid var(--vsf-control-border);border-radius:6px;background:var(--vsf-surface);color:var(--vsf-text);font:inherit;inline-size:100%}
.check{display:flex;align-items:center;gap:8px;min-block-size:var(--vsf-target)}
.check input{inline-size:20px;block-size:20px;accent-color:var(--vsf-accent)}
button{min-block-size:var(--vsf-target);min-inline-size:var(--vsf-target);padding:var(--vsf-pad-y) var(--vsf-pad-x);border:1px solid var(--vsf-accent);border-radius:6px;background:var(--vsf-accent);color:var(--vsf-on-accent);font:inherit;font-weight:600;cursor:pointer}
button[data-effect="read_only"]{background:transparent;color:var(--vsf-accent)}
button[data-effect="destructive"]{background:transparent;border-color:var(--vsf-danger);color:var(--vsf-danger)}
button[aria-disabled="true"]{background:var(--vsf-surface-2);border:1px dashed var(--vsf-control-border);color:var(--vsf-muted);cursor:not-allowed}
button[aria-busy="true"]{opacity:.7}
:is(button,a,input,select,[tabindex]):focus-visible{outline:3px solid var(--vsf-focus);outline-offset:2px}
.group{display:flex;flex-wrap:wrap;gap:var(--vsf-gap)}
.badge{display:inline-flex;align-items:center;gap:4px;padding:1px 8px;border-radius:999px;font-size:.85em;font-weight:600}
.badge[data-status="caution"]{color:var(--vsf-caution);background:var(--vsf-caution-bg)}
.badge[data-status="warning"]{color:var(--vsf-warning);background:var(--vsf-warning-bg)}
.badge[data-status="danger"]{color:var(--vsf-danger);background:var(--vsf-danger-bg)}
.badge[data-status="success"]{color:var(--vsf-success);background:var(--vsf-success-bg)}
.alerts{min-block-size:0}
.alerts:not(:empty){padding:var(--vsf-pad-y) var(--vsf-pad-x);border-inline-start:4px solid var(--vsf-danger);background:var(--vsf-danger-bg);color:var(--vsf-danger);font-weight:600}
.status-line:not(:empty){padding:var(--vsf-pad-y) var(--vsf-pad-x);border-inline-start:4px solid var(--vsf-success);background:var(--vsf-success-bg);color:var(--vsf-success);font-weight:600}
.grid-wrap{position:relative;overflow:auto;max-block-size:70vh;border:1px solid var(--vsf-border);border-radius:6px}
table{border-collapse:separate;border-spacing:0;inline-size:100%}
th,td{padding:0 var(--vsf-pad-x);text-align:start;border-block-end:1px solid var(--vsf-border);block-size:var(--vsf-row);white-space:nowrap}
th{position:sticky;inset-block-start:0;background:var(--vsf-surface-2);z-index:1}
th button{all:unset;cursor:pointer;display:inline-flex;align-items:center;justify-content:center;gap:6px;min-block-size:var(--vsf-target);min-inline-size:var(--vsf-target);font-weight:700}
tbody tr{content-visibility:auto;contain-intrinsic-size:auto var(--vsf-row)}
td:focus-visible,th button:focus-visible{outline:3px solid var(--vsf-focus);outline-offset:-3px}
td .row-actions{display:inline-flex;gap:var(--vsf-gap)}
.empty{margin:0;padding:var(--vsf-pad-y) var(--vsf-pad-x);color:var(--vsf-muted);border:1px dashed var(--vsf-border);border-radius:6px}
.empty[hidden]{display:none}
.toolbar{display:flex;gap:var(--vsf-gap);align-items:center;margin-block-end:var(--vsf-gap)}
.palette-scrim{position:fixed;inset:0;background:rgba(0,0,0,.45);display:grid;place-items:start center;padding-block-start:12vh;z-index:10}
.palette{inline-size:min(36em,92vw);background:var(--vsf-bg);color:var(--vsf-text);border:1px solid var(--vsf-control-border);border-radius:10px;padding:var(--vsf-gap);display:grid;gap:var(--vsf-gap)}
.palette ul{list-style:none;margin:0;padding:0;max-block-size:50vh;overflow:auto}
.palette li{padding:var(--vsf-pad-y) var(--vsf-pad-x);border-radius:6px;min-block-size:var(--vsf-target);display:flex;align-items:center;justify-content:space-between;gap:var(--vsf-gap)}
.palette li[aria-selected="true"]{background:var(--vsf-accent);color:var(--vsf-on-accent)}
.palette li[aria-disabled="true"]{color:var(--vsf-muted)}
.palette li[aria-selected="true"][aria-disabled="true"]{background:var(--vsf-surface-2);color:var(--vsf-text)}
.surface{border:1px dashed var(--vsf-control-border);border-radius:6px;padding:var(--vsf-gap);background:var(--vsf-surface)}
.surface[data-mode="in_place"]{border-style:solid}
.split{display:grid;grid-template-columns:1fr 1fr;gap:var(--vsf-gap)}
.layout-frame{container-type:inline-size;min-inline-size:0}
.layout{display:grid;grid-template-columns:auto minmax(0,1fr) auto;grid-template-areas:"top top top" "start center end" "bottom bottom bottom";gap:var(--vsf-gap);align-items:start;min-inline-size:0}
.place{display:grid;grid-template-columns:minmax(0,1fr);gap:var(--vsf-gap);align-content:start;min-inline-size:0}
.place:empty{display:none}
.place[data-place="top"]{grid-area:top}
.place[data-place="start"]{grid-area:start}
.place[data-place="center"]{grid-area:center}
.place[data-place="end"]{grid-area:end}
.place[data-place="bottom"]{grid-area:bottom}
.ch-probe{position:absolute;visibility:hidden;pointer-events:none;inline-size:10ch;block-size:0;overflow:hidden}
.region{display:grid;grid-template-columns:minmax(0,1fr);gap:var(--vsf-gap);align-content:start;min-inline-size:0}
.place[data-place="start"]>.region,.place[data-place="end"]>.region{inline-size:clamp(var(--vsf-min,0ch),var(--vsf-basis,var(--vsf-min,auto)),var(--vsf-max,100%))}
.region-body{display:grid;grid-template-columns:minmax(0,1fr);gap:var(--vsf-gap);min-inline-size:0;align-content:start}
.region-body>*,.parts>*,.place>*{min-inline-size:0}
.region-body[hidden]{display:none}
.region[data-arrange="row"]>.region-body{display:flex;flex-wrap:wrap;align-items:flex-start}
.region[data-arrange="grid"]>.region-body{grid-template-columns:repeat(auto-fit,minmax(min(100%,var(--vsf-min,20ch)),1fr))}
.parts{display:contents}
.region[data-arrange="split"]>.parts{display:grid;grid-template-columns:repeat(var(--vsf-parts,2),minmax(0,1fr));gap:var(--vsf-gap);align-items:start}
.region[data-arrange="stack"]>.parts,.region[data-arrange="row"]>.parts,.region[data-arrange="grid"]>.parts{display:grid;gap:var(--vsf-gap)}
.region[data-arrange="tabs"]>.parts>[data-part]:not([data-selected]){display:none}
.tablist{display:none;gap:4px;flex-wrap:wrap;border-block-end:1px solid var(--vsf-border)}
.tablist[data-on]{display:flex}
.tab{background:transparent;color:var(--vsf-text);border:1px solid transparent;border-block-end:3px solid transparent;border-radius:6px 6px 0 0}
.tab[aria-selected="true"]{border-block-end-color:var(--vsf-accent);font-weight:700}
.region-toggle{justify-self:start;background:transparent;color:var(--vsf-accent)}
.region[data-collapsed]{align-content:start}
.place[data-place="start"]>.region[data-collapsed],.place[data-place="end"]>.region[data-collapsed]{inline-size:auto}
@container (max-width:60ch){.layout{grid-template-columns:minmax(0,1fr);grid-template-areas:"top" "start" "center" "end" "bottom"}.place[data-place="start"]>.region,.place[data-place="end"]>.region{inline-size:auto}.region[data-arrange="split"]>.parts{grid-template-columns:minmax(0,1fr)}}
@media (forced-colors:active){button,.badge{border:1px solid ButtonText}:is(button,a,input,select,[tabindex]):focus-visible{outline-color:Highlight}}
@media (prefers-reduced-motion:no-preference){button{transition:background-color .12s}}
`;
}
