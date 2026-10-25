import type { Oracle, Workflow } from "../spec.ts";

const AUTH = `Basic ${Buffer.from("auditor:Audit-pass-123!").toString("base64")}`;
const api = async (origin: string, path: string, init: RequestInit = {}): Promise<{ status: number; body: unknown }> => {
  const r = await fetch(`${origin}/api/v1${path}`, { ...init, headers: { authorization: AUTH, "content-type": "application/json", ...(init.headers ?? {}) } });
  const text = await r.text();
  let body: unknown = text;
  try {
    body = JSON.parse(text);
  } catch {
    /* plain text */
  }
  return { status: r.status, body };
};
const titles = (body: unknown): string[] => (body as { title: string }[]).map((i) => i.title);

export const gitea: Workflow[] = [
  {
    id: "GITEA-01",
    app: "gitea",
    title: "Create an initialized repository",
    objective: "Create a repository named audit-new with a description and an initial README.",
    effect: "write",
    categories: ["crud", "form", "conditional"],
    difficulty: 2,
    start: "/repo/create",
    steps: [
      { do: "fill", target: "Repository Name *", role: "textbox", value: "audit-new" },
      { do: "fill", target: "Description", role: "textbox", value: "Created during the audit" },
      { do: "toggle", target: "Initialize Repository (Adds .gitignore, License and README)", role: "checkbox", checked: true },
      { do: "click", target: "Create Repository", role: "button" },
    ],
    oracle: "gitea01",
  },
  {
    id: "GITEA-02",
    app: "gitea",
    title: "Open an issue",
    objective: "Open an issue with a title and a body in the seeded repository.",
    effect: "write",
    categories: ["crud", "form", "editor"],
    difficulty: 1,
    start: "/auditor/seed-repo/issues/new",
    steps: [
      { do: "fill", target: "Title", role: "textbox", value: "Audit issue title" },
      { do: "fill", target: "Leave a comment", role: "textbox", value: "Audit issue body" },
      { do: "click", target: "Create Issue", role: "button" },
    ],
    oracle: "gitea02",
  },
  {
    id: "GITEA-03",
    app: "gitea",
    title: "Comment on an issue and close it",
    objective: "Add a comment to issue 3 and close the issue in the same action.",
    effect: "write",
    categories: ["crud", "conditional", "state-transition"],
    difficulty: 2,
    start: "/auditor/seed-repo/issues/3",
    steps: [
      { do: "fill", target: "Leave a comment", role: "textbox", value: "Closing from the audit" },
      { do: "click", target: "Close with Comment", role: "button" },
    ],
    oracle: "gitea03",
  },
  {
    id: "GITEA-04",
    app: "gitea",
    title: "Filter issues by label",
    objective: "Show only the issues carrying the label bug.",
    effect: "read",
    categories: ["table", "filter", "read-only"],
    difficulty: 1,
    start: "/auditor/seed-repo/issues",
    steps: [
      { do: "choose", target: "Label", sel: "div[role='combobox']:has(> .text:text-is('Label'))", value: "bug", unnamed: true },
    ],
    oracle: "gitea04",
  },
  {
    id: "GITEA-05",
    app: "gitea",
    title: "Create a label in a dialog",
    objective: "Create the label audit-label with a description from the label dialog.",
    effect: "write",
    categories: ["dialog", "form", "crud"],
    difficulty: 2,
    start: "/auditor/seed-repo/labels",
    steps: [
      { do: "click", target: "New Label", role: "button" },
      { do: "fill", target: "Label name", role: "textbox", value: "audit-label" },
      { do: "fill", target: "Description", role: "textbox", within: ".ui.modal", value: "Label from the audit" },
      { do: "fill", target: "Color", sel: ".ui.modal input[placeholder='#c320f6']", value: "#0e8a16" },
      { do: "click", target: "Save", role: "button" },
    ],
    oracle: "gitea05",
  },
  {
    id: "GITEA-06",
    app: "gitea",
    title: "Create a user as site administrator",
    objective: "Create the account audituser with an email and a password without a forced password change.",
    effect: "write",
    categories: ["enterprise-admin", "form", "conditional"],
    difficulty: 2,
    start: "/-/admin/users/new",
    steps: [
      { do: "fill", target: "Username *", role: "textbox", value: "audituser" },
      { do: "fill", target: "Email Address *", role: "textbox", value: "audituser@example.invalid" },
      { do: "fill", target: "Password *", role: "textbox", value: "Audit-pass-456!" },
      { do: "toggle", target: "Require user to change password (recommended)", role: "checkbox", checked: false },
      { do: "click", target: "Create User Account", role: "button" },
    ],
    oracle: "gitea06",
  },
  {
    id: "GITEA-07",
    app: "gitea",
    title: "Delete a repository through a confirmation dialog",
    objective: "Delete the seeded repository after confirming its name in the danger-zone dialog.",
    effect: "destructive",
    categories: ["dialog", "destructive", "conditional"],
    difficulty: 3,
    start: "/auditor/seed-repo/settings",
    steps: [
      { do: "click", target: "Delete This Repository", role: "button" },
      { do: "fill", target: "Repository Name *", role: "textbox", within: ".ui.modal.visible", value: "auditor/seed-repo" },
      { do: "click", target: "Delete Repository", role: "button" },
    ],
    oracle: "gitea07",
  },
  {
    id: "GITEA-08",
    app: "gitea",
    title: "Upload a file to a repository",
    objective: "Upload audit-upload.txt through the repository upload page and commit it to main.",
    effect: "write",
    categories: ["upload", "file", "multi-step"],
    difficulty: 3,
    start: "/auditor/seed-repo/_upload/main/",
    steps: [
      { do: "upload", target: "Drop files here to upload", role: "button", value: "audit-upload.txt", text: "uploaded by the audit\n" },
      { do: "click", target: "Commit Changes", role: "button", wait: 800 },
    ],
    oracle: "gitea08",
  },
];

export const giteaOracles: Record<string, Oracle> = {
  gitea01: async (c) => {
    const r = await api(c.origin, "/repos/auditor/audit-new");
    if (r.status !== 200) return { ok: false, detail: `repo status ${r.status}` };
    const d = (r.body as { description: string }).description;
    const f = await api(c.origin, "/repos/auditor/audit-new/contents/README.md");
    return { ok: d === "Created during the audit" && f.status === 200, detail: `description=${JSON.stringify(d)} readme=${f.status}` };
  },
  gitea02: async (c) => {
    const r = await api(c.origin, "/repos/auditor/seed-repo/issues?state=all&type=issues");
    const hit = (r.body as { title: string; body: string }[]).find((i) => i.title === "Audit issue title");
    return { ok: hit?.body === "Audit issue body", detail: hit ? `body=${JSON.stringify(hit.body)}` : "issue absent" };
  },
  gitea03: async (c) => {
    const i = (await api(c.origin, "/repos/auditor/seed-repo/issues/3")).body as { state: string; comments: number };
    const cm = (await api(c.origin, "/repos/auditor/seed-repo/issues/3/comments")).body as { body: string }[];
    return { ok: i.state === "closed" && cm.some((x) => x.body === "Closing from the audit"), detail: `state=${i.state} comments=${cm.length}` };
  },
  gitea04: async (c) => {
    const want = titles((await api(c.origin, "/repos/auditor/seed-repo/issues?state=open&type=issues&labels=bug")).body).sort();
    const shown = (await c.page.locator("a.list-item-large-title").allTextContents()).map((t) => t.trim()).sort();
    return { ok: want.length > 0 && JSON.stringify(want) === JSON.stringify(shown), detail: `api=${JSON.stringify(want)} shown=${JSON.stringify(shown)}` };
  },
  gitea05: async (c) => {
    const ls = (await api(c.origin, "/repos/auditor/seed-repo/labels")).body as { name: string; description: string }[];
    const hit = ls.find((l) => l.name === "audit-label");
    return { ok: hit?.description === "Label from the audit", detail: hit ? `description=${JSON.stringify(hit.description)}` : "label absent" };
  },
  gitea06: async (c) => {
    const r = await api(c.origin, "/users/audituser");
    const login = await api(c.origin, "/user", { headers: { authorization: `Basic ${Buffer.from("audituser:Audit-pass-456!").toString("base64")}` } });
    return { ok: r.status === 200 && login.status === 200, detail: `user=${r.status} login=${login.status}` };
  },
  gitea07: async (c) => {
    const r = await api(c.origin, "/repos/auditor/seed-repo");
    return { ok: r.status === 404, detail: `repo status ${r.status}` };
  },
  gitea08: async (c) => {
    const r = await api(c.origin, "/repos/auditor/seed-repo/contents/audit-upload.txt");
    if (r.status !== 200) return { ok: false, detail: `contents status ${r.status}` };
    const txt = Buffer.from((r.body as { content: string }).content, "base64").toString("utf8");
    return { ok: txt === "uploaded by the audit\n", detail: `content=${JSON.stringify(txt)}` };
  },
};
