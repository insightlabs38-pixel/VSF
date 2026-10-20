import assert from "node:assert/strict";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { auditPackage, DEFAULT_POLICY, scanHtml, scanScript } from "../src/extaudit.ts";

const root = fileURLToPath(new URL("../../../", import.meta.url));
const built = join(root, "artifacts/extension");
const have = existsSync(join(built, "product/manifest.json"));
const rules = (r: { findings: { rule: string }[] }): string[] => r.findings.map((f) => f.rule);

function copyOf(variant: string): string {
  const dir = mkdtempSync(join(tmpdir(), "vsf-audit-"));
  cpSync(join(built, variant), dir, { recursive: true });
  return dir;
}

function edit(dir: string, fn: (m: Record<string, unknown>) => void): void {
  const p = join(dir, "manifest.json");
  const m = JSON.parse(readFileSync(p, "utf8")) as Record<string, unknown>;
  fn(m);
  writeFileSync(p, JSON.stringify(m));
}

test("the product package passes and is classified file by file", { skip: !have }, () => {
  const r = auditPackage(join(built, "product"), "product");
  assert.deepEqual(r.findings, []);
  assert.ok(r.files.some((f) => f.path === "vsf_wasm.wasm" && f.class === "code"));
  assert.equal(r.files.find((f) => f.path === "manifest.json")?.class, "data");
  assert.match(r.digest, /^[0-9a-f]{64}$/);
});

test("the test variant passes with its pinned loopback host", { skip: !have }, () => {
  assert.deepEqual(auditPackage(join(built, "test"), "test").findings, []);
});

test("manifest tampering is caught", { skip: !have }, () => {
  const base = ["activeTab", "scripting", "storage"];
  const cases: [string, Record<string, unknown>, string][] = [
    ["extra permission", { permissions: [...base, "tabs"] }, "manifest-permissions"],
    ["debugger", { permissions: [...base, "debugger"] }, "manifest-permissions"],
    ["install-time hosts", { host_permissions: ["https://example.test/*"] }, "manifest-hosts"],
    ["all urls", { optional_host_permissions: ["<all_urls>"] }, "manifest-hosts"],
    ["static content script", { content_scripts: [{ matches: ["*://*/*"], js: ["content.js"] }] }, "manifest-surface"],
    ["externally connectable", { externally_connectable: { matches: ["https://*/*"] } }, "manifest-surface"],
    [
      "web accessible",
      { web_accessible_resources: [{ resources: ["*"], matches: ["<all_urls>"] }] },
      "manifest-surface",
    ],
    [
      "relaxed csp",
      { content_security_policy: { extension_pages: "script-src 'self' 'unsafe-eval'" } },
      "manifest-csp",
    ],
    ["mv2", { manifest_version: 2 }, "manifest-mv3"],
  ];
  for (const [name, patch, rule] of cases) {
    const dir = copyOf("product");
    try {
      edit(dir, (m) => Object.assign(m, patch));
      assert.ok(rules(auditPackage(dir, "product")).includes(rule), name);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  }
});

test("package-level tampering is caught", { skip: !have }, () => {
  const dir = copyOf("product");
  try {
    writeFileSync(join(dir, "extra.txt"), "x");
    writeFileSync(
      join(dir, "panel.html"),
      '<script>alert(1)</script><script src="https://cdn.example.test/x.js"></script>',
    );
    mkdirSync(join(dir, "nested"));
    writeFileSync(join(dir, "nested/a.js"), "eval('1')");
    const r = auditPackage(dir, "product");
    for (const rule of ["file-types", "no-inline-script", "no-remote-code", "no-dynamic-code"])
      assert.ok(rules(r).includes(rule), rule);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a WASM module with host imports is refused", { skip: !have }, () => {
  const dir = copyOf("product");
  try {
    const withImport = Uint8Array.from([
      0, 97, 115, 109, 1, 0, 0, 0, 1, 4, 1, 96, 0, 0, 2, 9, 1, 3, 101, 110, 118, 1, 102, 0, 0,
    ]);
    writeFileSync(join(dir, "vsf_wasm.wasm"), withImport);
    assert.ok(rules(auditPackage(dir, "product")).includes("wasm-imports"));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("script scanning finds dynamic and remote code and unlisted network sinks", () => {
  const bad = [
    "eval(x)",
    "new Function('a', 'return a')",
    "importScripts('https://a.test/x.js')",
    "import(url)",
    "document.createElement('script')",
    "s.src = u",
    "setTimeout('f()', 1)",
    "fetch('https://a.test')",
    "new WebSocket(u)",
    "navigator.sendBeacon(u)",
    "new Worker(u)",
    "WebAssembly.instantiateStreaming(fetch(chrome.runtime.getURL('a')))",
  ];
  for (const code of bad) assert.ok(scanScript("a.js", code).length > 0, code);
  const ok = [
    "fetch(chrome.runtime.getURL('vsf_wasm.wasm'))",
    "const f = (x) => x + 1; setTimeout(f, 1);",
    "const s = 'eval(x) and fetch(u)'; // eval(x)",
  ];
  for (const code of ok) assert.deepEqual(scanScript("a.js", code), [], code);
  assert.deepEqual(scanScript("bridge.js", "fetch(u)", ["fetch"]), []);
  assert.deepEqual(
    scanScript("a.js", "fetch(u)", ["XMLHttpRequest"]).map((f) => f.rule),
    ["network-sink"],
  );
});

test("html scanning refuses inline code and remote URLs", () => {
  assert.deepEqual(scanHtml("a.html", '<script type="module" src="panel.js"></script>'), []);
  assert.ok(scanHtml("a.html", "<script>1</script>").length > 0);
  assert.ok(scanHtml("a.html", '<button onclick="x()">').length > 0);
  assert.ok(scanHtml("a.html", '<a href="javascript:void 0">').length > 0);
  assert.ok(scanHtml("a.html", '<link href="//cdn.test/a.css">').length > 0);
});

test("the default policy names the reviewed CSP", () => {
  assert.equal(DEFAULT_POLICY.csp, "script-src 'self' 'wasm-unsafe-eval'; object-src 'self'");
});
