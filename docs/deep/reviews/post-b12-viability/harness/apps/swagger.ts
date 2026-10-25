import type { Oracle, Workflow } from "../spec.ts";

const state = async (origin: string): Promise<{ pets: any[]; requests: any[] }> => (await (await fetch(`${origin}/__audit/state`)).json()) as { pets: any[]; requests: any[] };
const responseText = (page: import("@playwright/test").Page): Promise<string> => page.locator(".responses-wrapper .live-responses-table .microlight").first().innerText({ timeout: 1500 }).catch(() => "");

export const swagger: Workflow[] = [
  {
    id: "SW-01",
    app: "swagger",
    title: "Add a pet with an edited request body",
    objective: "Expand POST /pet, enable Try it out, replace the request body with a new pet and execute it.",
    effect: "write",
    categories: ["developer", "form", "editor", "multi-step"],
    difficulty: 3,
    start: "/",
    settle: 1500,
    steps: [
      { do: "click", target: "POST /pet Add a new pet to the store.", role: "button" },
      { do: "click", target: "Try it out", role: "button", within: ".opblock.is-open" },
      { do: "fill", target: "Request body", sel: ".opblock.is-open .body-param__text", unnamed: true, value: '{"id": 77, "name": "audit-pet", "photoUrls": [], "status": "available"}' },
      { do: "click", target: "Execute", role: "button", within: ".opblock.is-open", wait: 800 },
    ],
    oracle: "sw01",
  },
  {
    id: "SW-02",
    app: "swagger",
    title: "Fetch a pet by id",
    objective: "Expand GET /pet/{petId}, enter id 2, execute and read the returned pet.",
    effect: "read",
    categories: ["developer", "form", "read-only"],
    difficulty: 2,
    start: "/",
    settle: 1500,
    steps: [
      { do: "click", target: "GET /pet/{petId} Find pet by ID.", role: "button" },
      { do: "click", target: "Try it out", role: "button", within: ".opblock.is-open" },
      { do: "fill", target: "petId", role: "textbox", within: ".opblock.is-open", value: "2" },
      { do: "click", target: "Execute", role: "button", within: ".opblock.is-open", wait: 800 },
    ],
    oracle: "sw02",
  },
  {
    id: "SW-03",
    app: "swagger",
    title: "Find pets by status with a select parameter",
    objective: "Run GET /pet/findByStatus for status sold and read the result.",
    effect: "read",
    categories: ["developer", "form", "custom-select", "read-only"],
    difficulty: 2,
    start: "/",
    settle: 1500,
    steps: [
      { do: "click", target: "GET /pet/findByStatus Finds Pets by status.", role: "button" },
      { do: "click", target: "Try it out", role: "button", within: ".opblock.is-open" },
      { do: "choose", target: "Status values that need to be considered for filter", sel: ".opblock.is-open .parameters select", unnamed: true, value: "sold" },
      { do: "click", target: "Execute", role: "button", within: ".opblock.is-open", wait: 800 },
    ],
    oracle: "sw03",
  },
  {
    id: "SW-04",
    app: "swagger",
    title: "Delete a pet",
    objective: "Run DELETE /pet/{petId} for id 3 so the pet is removed from the store.",
    effect: "destructive",
    categories: ["developer", "destructive", "form"],
    difficulty: 2,
    start: "/",
    settle: 1500,
    steps: [
      { do: "click", target: "DELETE /pet/{petId} Deletes a pet.", role: "button" },
      { do: "click", target: "Try it out", role: "button", within: ".opblock.is-open" },
      { do: "fill", target: "petId", role: "textbox", within: ".opblock.is-open", value: "3" },
      { do: "click", target: "Execute", role: "button", within: ".opblock.is-open", wait: 800 },
    ],
    oracle: "sw04",
  },
  {
    id: "SW-05",
    app: "swagger",
    title: "Authorize with an API key and call an operation",
    objective: "Store an API key in the Authorize dialog, then run GET /store/inventory so the key is sent.",
    effect: "read",
    categories: ["developer", "dialog", "auth", "multi-step"],
    difficulty: 3,
    start: "/",
    settle: 1500,
    steps: [
      { do: "click", target: "Authorize", role: "button", sel: ".auth-wrapper button.authorize" },
      { do: "fill", target: "Value:", role: "textbox", within: ".dialog-ux", value: "audit-key" },
      { do: "click", target: "Apply credentials", role: "button" },
      { do: "click", target: "Close", role: "button", within: ".dialog-ux", nth: 0 },
      { do: "click", target: "GET /store/inventory Returns pet inventories by status.", role: "button" },
      { do: "click", target: "Try it out", role: "button", within: ".opblock.is-open" },
      { do: "click", target: "Execute", role: "button", within: ".opblock.is-open", wait: 800 },
    ],
    oracle: "sw05",
  },
  {
    id: "SW-06",
    app: "swagger",
    title: "Read the store inventory",
    objective: "Run GET /store/inventory and read the counts per status.",
    effect: "read",
    categories: ["developer", "read-only", "disclosure"],
    difficulty: 1,
    start: "/",
    settle: 1500,
    steps: [
      { do: "click", target: "GET /store/inventory Returns pet inventories by status.", role: "button" },
      { do: "click", target: "Try it out", role: "button", within: ".opblock.is-open" },
      { do: "click", target: "Execute", role: "button", within: ".opblock.is-open", wait: 800 },
    ],
    oracle: "sw06",
  },
];

export const swaggerOracles: Record<string, Oracle> = {
  sw01: async (c) => {
    const s = await state(c.origin);
    const hit = s.pets.find((p) => p.id === 77);
    const req = s.requests.some((r) => r.method === "POST" && r.path === "/api/v3/pet");
    return { ok: hit?.name === "audit-pet" && req, detail: `pet=${JSON.stringify(hit ?? null)} post=${req}` };
  },
  sw02: async (c) => {
    const s = await state(c.origin);
    const req = s.requests.some((r) => r.method === "GET" && r.path === "/api/v3/pet/2");
    const shown = await responseText(c.page);
    return { ok: req && shown.includes("Tom"), detail: `request=${req} shown=${shown.slice(0, 40)}` };
  },
  sw03: async (c) => {
    const s = await state(c.origin);
    const req = s.requests.find((r) => r.path === "/api/v3/pet/findByStatus");
    const shown = await responseText(c.page);
    return { ok: req?.query.status === "sold" && shown.includes("Nemo") && !shown.includes("Rex"), detail: `query=${JSON.stringify(req?.query)} shown=${shown.slice(0, 40)}` };
  },
  sw04: async (c) => {
    const s = await state(c.origin);
    const req = s.requests.some((r) => r.method === "DELETE" && r.path === "/api/v3/pet/3");
    const gone = !s.pets.some((p) => p.id === 3);
    return { ok: req && gone && s.pets.length === 2, detail: `request=${req} pets=${s.pets.length}` };
  },
  sw05: async (c) => {
    const s = await state(c.origin);
    const req = s.requests.find((r) => r.path === "/api/v3/store/inventory");
    return { ok: req?.headers.api_key === "audit-key", detail: `key header ${req?.headers.api_key ?? null}` };
  },
  sw06: async (c) => {
    const s = await state(c.origin);
    const req = s.requests.some((r) => r.path === "/api/v3/store/inventory");
    const shown = await responseText(c.page);
    return { ok: req && shown.replace(/\s+/g, "").includes('"available":1'), detail: `shown=${shown.replace(/\s+/g, " ").slice(0, 60)}` };
  },
};
