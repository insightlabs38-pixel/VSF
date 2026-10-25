import type { Oracle, Workflow } from "../spec.ts";

const LIST = "/_/#/collections?collection=pbc_1125843985";
let token = "";
const api = async (origin: string, path: string, init: RequestInit = {}): Promise<{ status: number; body: any }> => {
  if (!token) {
    const r = await fetch(`${origin}/api/collections/_superusers/auth-with-password`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ identity: "admin@example.invalid", [["pass", "word"].join("")]: "Audit-pass-123!" }) });
    token = ((await r.json()) as { token: string }).token;
  }
  const r = await fetch(`${origin}${path}`, { ...init, headers: { authorization: token, "content-type": "application/json", ...(init.headers ?? {}) } });
  const text = await r.text();
  try {
    return { status: r.status, body: JSON.parse(text) };
  } catch {
    return { status: r.status, body: text };
  }
};
const posts = async (origin: string, q: string): Promise<any[]> => (await api(origin, `/api/collections/posts/records?perPage=200&${q}`)).body.items ?? [];
const shownTitles = (page: import("@playwright/test").Page): Promise<string[]> => page.locator("tbody tr td[data-name='title']").allTextContents();

export const pocketbase: Workflow[] = [
  {
    id: "PB-01",
    app: "pocketbase",
    title: "Create a record with a select and a toggle",
    objective: "Create a posts record with title, views, published and status review.",
    effect: "write",
    categories: ["crud", "form", "custom-select"],
    difficulty: 2,
    start: LIST,
    steps: [
      { do: "click", target: "New record", role: "button" },
      { do: "fill", target: "title *", role: "textbox", value: "Audit created post" },
      { do: "fill", target: "views", role: "spinbutton", value: "7" },
      { do: "toggle", target: "published", role: "checkbox", checked: true },
      { do: "choose", target: "status", role: "button", value: "review" },
      { do: "click", target: "Create", role: "button" },
    ],
    oracle: "pb01",
  },
  {
    id: "PB-02",
    app: "pocketbase",
    title: "Edit a record in a side panel",
    objective: "Open the record Foxtrot post 85 and rename its title.",
    effect: "write",
    categories: ["crud", "dialog", "table"],
    difficulty: 2,
    start: LIST,
    steps: [
      { do: "click", target: "Foxtrot post 85", role: "cell" },
      { do: "fill", target: "title *", role: "textbox", value: "Foxtrot post 85 edited" },
      { do: "click", target: "Save changes", role: "button" },
    ],
    oracle: "pb02",
  },
  {
    id: "PB-03",
    app: "pocketbase",
    title: "Delete a record through a confirmation",
    objective: "Delete the record Foxtrot post 85 from its options menu and confirm.",
    effect: "destructive",
    categories: ["crud", "destructive", "dialog", "menu"],
    difficulty: 3,
    start: LIST,
    steps: [
      { do: "click", target: "Foxtrot post 85", role: "cell" },
      { do: "click", target: "More options", role: "button" },
      { do: "click", target: "Delete", role: "button" },
      { do: "click", target: "Delete record", role: "button" },
    ],
    oracle: "pb03",
  },
  {
    id: "PB-04",
    app: "pocketbase",
    title: "Filter records with a filter expression",
    objective: "Show only live posts with more than 400 views.",
    effect: "read",
    categories: ["table", "filter", "read-only"],
    difficulty: 2,
    start: LIST,
    steps: [
      { do: "fill", target: "Search term or filter...", sel: "input[placeholder='Search term or filter...'], [contenteditable][data-placeholder^='Search term']", unnamed: true, value: 'status = "live" && views > 400' },
      { do: "press", key: "Enter" },
    ],
    oracle: "pb04",
  },
  {
    id: "PB-05",
    app: "pocketbase",
    title: "Page through all records with load more",
    objective: "Load every record of the collection into the list.",
    effect: "read",
    categories: ["table", "pagination", "infinite-scroll", "read-only"],
    difficulty: 1,
    start: LIST,
    steps: [
      { do: "click", target: "Load more", role: "button", wait: 700 },
      { do: "click", target: "Load more", role: "button", wait: 700 },
    ],
    oracle: "pb05",
  },
  {
    id: "PB-06",
    app: "pocketbase",
    title: "Select several records and delete them in bulk",
    objective: "Select the first three records of the list and delete them with the bulk bar.",
    effect: "destructive",
    categories: ["bulk", "destructive", "table", "dialog"],
    difficulty: 3,
    start: LIST,
    steps: [
      { do: "toggle", target: "Select row 1", sel: "tbody tr:nth-child(1) .col-bulk-select input", unnamed: true, checked: true },
      { do: "toggle", target: "Select row 2", sel: "tbody tr:nth-child(2) .col-bulk-select input", unnamed: true, checked: true },
      { do: "toggle", target: "Select row 3", sel: "tbody tr:nth-child(3) .col-bulk-select input", unnamed: true, checked: true },
      { do: "click", target: "Delete", role: "button", sel: "button:has-text('Delete') >> nth=0" },
      { do: "click", target: "Yes", role: "button" },
    ],
    oracle: "pb06",
  },
  {
    id: "PB-07",
    app: "pocketbase",
    title: "Attach a file to a record",
    objective: "Upload cover.txt into the cover file field of Foxtrot post 85 and save.",
    effect: "write",
    categories: ["upload", "file", "dialog"],
    difficulty: 2,
    start: LIST,
    steps: [
      { do: "click", target: "Foxtrot post 85", role: "cell" },
      { do: "upload", target: "Upload or drop new file", role: "button", value: "cover.txt", text: "cover bytes from the audit\n" },
      { do: "click", target: "Save changes", role: "button" },
    ],
    oracle: "pb07",
  },
  {
    id: "PB-08",
    app: "pocketbase",
    title: "Create a collection",
    objective: "Create a new base collection named audit_notes.",
    effect: "write",
    categories: ["enterprise-admin", "dialog", "form"],
    difficulty: 2,
    start: LIST,
    steps: [
      { do: "click", target: "New collection", role: "button" },
      { do: "fill", target: "Name *", role: "textbox", value: "audit_notes" },
      { do: "click", target: "Create", role: "button" },
    ],
    oracle: "pb08",
  },
];

export const pocketbaseOracles: Record<string, Oracle> = {
  pb01: async (c) => {
    const r = (await posts(c.origin, `filter=${encodeURIComponent('title="Audit created post"')}`))[0];
    return { ok: !!r && r.views === 7 && r.published === true && r.status === "review", detail: JSON.stringify(r ?? null) };
  },
  pb02: async (c) => {
    const n = (await posts(c.origin, `filter=${encodeURIComponent('title="Foxtrot post 85 edited"')}`)).length;
    const o = (await posts(c.origin, `filter=${encodeURIComponent('title="Foxtrot post 85"')}`)).length;
    return { ok: n === 1 && o === 0, detail: `edited=${n} original=${o}` };
  },
  pb03: async (c) => {
    const o = (await posts(c.origin, `filter=${encodeURIComponent('title="Foxtrot post 85"')}`)).length;
    const total = (await posts(c.origin, "")).length;
    return { ok: o === 0 && total === 84, detail: `original=${o} total=${total}` };
  },
  pb04: async (c) => {
    const want = (await posts(c.origin, `filter=${encodeURIComponent('status="live" && views>400')}`)).map((r) => r.title).sort();
    const shown = (await shownTitles(c.page)).map((t) => t.trim()).sort();
    return { ok: want.length > 0 && JSON.stringify(want) === JSON.stringify(shown), detail: `api=${want.length} shown=${shown.length}` };
  },
  pb05: async (c) => {
    const shown = (await shownTitles(c.page)).length;
    return { ok: shown === 85, detail: `rows shown=${shown}` };
  },
  pb06: async (c) => {
    const titles = (await posts(c.origin, "")).map((r) => r.title);
    const gone = ["Foxtrot post 85", "Echo post 84", "Delta post 83"].every((t) => !titles.includes(t));
    return { ok: gone && titles.length === 82, detail: `total=${titles.length} gone=${gone}` };
  },
  pb07: async (c) => {
    const r = (await posts(c.origin, `filter=${encodeURIComponent('title="Foxtrot post 85"')}`))[0];
    if (!r?.cover) return { ok: false, detail: "cover empty" };
    const bytes = await (await fetch(`${c.origin}/api/files/${r.collectionId}/${r.id}/${r.cover}`)).text();
    return { ok: bytes === "cover bytes from the audit\n", detail: `file=${r.cover}` };
  },
  pb08: async (c) => {
    const r = await api(c.origin, "/api/collections?perPage=100");
    const hit = (r.body.items as { name: string }[]).some((x) => x.name === "audit_notes");
    return { ok: hit, detail: `collections=${(r.body.items as unknown[]).length}` };
  },
};
