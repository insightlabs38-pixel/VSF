import assert from "node:assert/strict";
import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, test } from "node:test";
import { buildExtension } from "../build.ts";
import { auditPackage, type Finding } from "../src/audit.ts";

let work: string;
let product: string;
let testBuild: string;

before(async () => {
  work = mkdtempSync(join(tmpdir(), "vsf-audit-"));
  product = await buildExtension({ name: "product", hostPermissions: [] }, join(work, "out"));
  testBuild = await buildExtension({ name: "test", hostPermissions: ["http://127.0.0.1/*"] }, join(work, "out"));
});

after(() => rmSync(work, { recursive: true, force: true }));

const rules = (f: Finding[]): string[] => f.map((x) => x.rule).sort();

/** Copies the product package, lets `mutate` damage it, and returns the rules the audit reports. */
function damaged(mutate: (dir: string) => void, variant: "product" | "test" = "product"): string[] {
  const dir = mkdtempSync(join(work, "case-"));
  cpSync(variant === "product" ? product : testBuild, dir, { recursive: true });
  mutate(dir);
  return rules(auditPackage(dir, variant));
}

function editManifest(dir: string, edit: (m: Record<string, unknown>) => void): void {
  const p = join(dir, "manifest.json");
  const m = JSON.parse(readFileSync(p, "utf8")) as Record<string, unknown>;
  edit(m);
  writeFileSync(p, JSON.stringify(m));
}

function append(dir: string, file: string, text: string): void {
  const p = join(dir, file);
  writeFileSync(p, `${readFileSync(p, "utf8")}\n${text}\n`);
}

test("the product and test packages pass the audit", () => {
  assert.deepEqual(auditPackage(product, "product"), []);
  assert.deepEqual(auditPackage(testBuild, "test"), []);
});

test("the product package requests no host access at install time", () => {
  const m = JSON.parse(readFileSync(join(product, "manifest.json"), "utf8")) as Record<string, unknown>;
  assert.equal("host_permissions" in m, false);
  assert.deepEqual(m.permissions, ["scripting", "storage"]);
  assert.deepEqual(m.optional_host_permissions, ["http://*/*", "https://*/*"]);
});

test("install-time or broad host access is refused", () => {
  const hosts = (h: string[]) => (d: string) => editManifest(d, (m) => (m.host_permissions = h));
  assert.deepEqual(damaged(hosts(["http://127.0.0.1/*"])), ["manifest.install_time_hosts"]);
  assert.deepEqual(damaged(hosts(["<all_urls>"]), "test"), ["manifest.all_hosts"]);
  assert.deepEqual(damaged(hosts(["*://*/*"]), "test"), ["manifest.all_hosts"]);
  assert.deepEqual(damaged(hosts(["https://*/*"]), "test"), ["manifest.all_hosts"]);
});

test("extra permissions and optional hosts are refused", () => {
  const perms = (p: string[]) => (d: string) => editManifest(d, (m) => (m.permissions = p));
  assert.deepEqual(damaged(perms(["scripting", "storage", "tabs"])), ["manifest.forbidden_permission"]);
  assert.deepEqual(damaged(perms(["scripting", "storage", "alarms"])), ["manifest.unexpected_permission"]);
  assert.deepEqual(damaged(perms(["storage"])), ["manifest.missing_permission"]);
  assert.deepEqual(
    damaged((d) => editManifest(d, (m) => (m.optional_host_permissions = ["https://example.test/*"]))),
    ["manifest.optional_hosts"],
  );
  assert.deepEqual(
    damaged((d) => editManifest(d, (m) => delete m.optional_host_permissions)),
    ["manifest.no_runtime_request"],
  );
  assert.deepEqual(
    damaged((d) => editManifest(d, (m) => (m.optional_permissions = ["tabs"]))),
    ["manifest.optional_permissions"],
  );
});

test("surfaces that let pages or other extensions in are refused", () => {
  for (const key of ["content_scripts", "externally_connectable", "web_accessible_resources", "sandbox", "update_url"])
    assert.deepEqual(
      damaged((d) => editManifest(d, (m) => (m[key] = key === "update_url" ? "https://x.test/u" : []))),
      ["manifest.forbidden_key"],
      key,
    );
});

test("the content security policy and background are pinned", () => {
  assert.deepEqual(
    damaged((d) =>
      editManifest(d, (m) => (m.content_security_policy = { extension_pages: "script-src 'self' https://cdn.test" })),
    ),
    ["manifest.csp"],
  );
  assert.deepEqual(
    damaged((d) => editManifest(d, (m) => (m.background = { service_worker: "other.js" }))),
    ["manifest.background"],
  );
});

test("remote or dynamic code in a bundle is refused", () => {
  const cases: [string, string, string][] = [
    ["eval", "eval(code);", "code.eval"],
    ["Function", 'new Function("return 1");', "code.new_function"],
    ["importScripts", 'importScripts("a.js");', "code.import_scripts"],
    ["string timer", 'setTimeout("x()", 1);', "code.string_timer"],
    ["script element", 'document.createElement("script");', "code.script_element"],
    ["dynamic import", "import(url);", "code.dynamic_import"],
    ["remote import", 'import("https://x.test/a.js");', "code.dynamic_import"],
    ["remote url", 'fetch("https://cdn.test/app.js");', "code.remote_url"],
    ["remote src", 'script.src = "https://x.test/a.js";', "code.script_src"],
  ];
  for (const [name, code, rule] of cases) {
    const found = damaged((d) => append(d, "background.js", code));
    assert.ok(found.includes(rule), `${name}: ${found}`);
  }
  assert.ok(damaged((d) => append(d, "content.js", 'eval("1")')).includes("code.eval"));
});

test("inert strings, comments and local dynamic import paths are not findings", () => {
  const found = damaged((d) => {
    append(d, "background.js", "// eval(x) is only mentioned here\n/* new Function(y) */");
    append(d, "background.js", 'const ns = "http://www.w3.org/2000/svg"; const lazy = () => import("./other.js");');
  });
  assert.deepEqual(found, []);
});

test("unreviewed files and a drifting file set are refused", () => {
  assert.deepEqual(
    damaged((d) => writeFileSync(join(d, "extra.js"), "1")),
    ["package.unexpected_file"],
  );
  assert.deepEqual(
    damaged((d) => rmSync(join(d, "observe.js"))),
    ["package.missing_file"],
  );
  assert.deepEqual(
    damaged((d) => mkdirSync(join(d, "nested"))),
    ["package.directory"],
  );
});

test("the panel page cannot load or run unreviewed script", () => {
  const html = (edit: (h: string) => string) => (d: string) => {
    const p = join(d, "panel.html");
    writeFileSync(p, edit(readFileSync(p, "utf8")));
  };
  assert.ok(damaged(html((h) => h.replace("</body>", "<script>x()</script></body>"))).includes("html.script_source"));
  assert.ok(
    damaged(html((h) => h.replace("</body>", '<script src="https://x.test/a.js"></script></body>'))).includes(
      "html.script_source",
    ),
  );
  assert.ok(damaged(html((h) => h.replace("<button", '<button onclick="x()"'))).includes("html.inline_handler"));
  assert.ok(damaged(html((h) => h.replace("</body>", '<iframe src="x"></iframe></body>'))).includes("html.embedding"));
});

test("the packaged wasm may not import anything from its host", () => {
  const importing = Uint8Array.from([
    0, 97, 115, 109, 1, 0, 0, 0, 1, 4, 1, 96, 0, 0, 2, 9, 1, 3, 101, 110, 118, 1, 102, 0, 0,
  ]);
  assert.deepEqual(
    damaged((d) => writeFileSync(join(d, "vsf_wasm.wasm"), importing)),
    ["wasm.imports"],
  );
});
