import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

export type Variant = "product" | "test";

export interface Finding {
  rule: string;
  detail: string;
}

/** The only files a package may contain; anything else is unreviewed content. */
export const PACKAGE_FILES = [
  "background.js",
  "content.js",
  "manifest.json",
  "observe.js",
  "panel.html",
  "panel.js",
  "shell.js",
  "vsf_wasm.wasm",
] as const;

const PRODUCT_PERMISSIONS = ["scripting", "storage"];
const OPTIONAL_HOSTS = ["http://*/*", "https://*/*"];
const FORBIDDEN_KEYS = [
  "content_scripts",
  "externally_connectable",
  "web_accessible_resources",
  "declarative_net_request",
  "sandbox",
  "side_panel",
  "devtools_page",
  "chrome_url_overrides",
  "update_url",
];
const FORBIDDEN_PERMISSIONS = [
  "tabs",
  "cookies",
  "webRequest",
  "webRequestBlocking",
  "debugger",
  "history",
  "downloads",
  "management",
  "proxy",
  "declarativeNetRequest",
  "<all_urls>",
];
const ALL_HOSTS = /(?:^|\*:\/\/|https?:\/\/)\*\/\*$|^<all_urls>$/;

// Namespace identifiers are inert strings; every other absolute URL in packaged code is a finding.
const NAMESPACE_URLS = [
  /^http:\/\/www\.w3\.org\/\d{4}\/[a-z0-9/.-]+$/i,
  /^http:\/\/www\.w3\.org\/XML\/1998\/namespace$/i,
  /^https?:\/\/127\.0\.0\.1(?::\d+)?\/?$/,
];

const CODE_SINKS: [string, RegExp][] = [
  ["eval", /(?<![\w$.])eval\s*\(/],
  ["new_function", /new\s+Function\s*\(/],
  ["function_ctor", /(?<![\w$])Function\s*\(\s*["'`]/],
  ["import_scripts", /importScripts\s*\(/],
  ["string_timer", /set(?:Timeout|Interval)\s*\(\s*["'`]/],
  ["script_element", /createElement\s*\(\s*["'`]script["'`]/i],
  ["dynamic_import", /(?<![\w$.])import\s*\(\s*(?!["'`]\.{0,2}\/)/],
  ["script_src", /\.src\s*=\s*["'`]https?:/],
  ["wasm_streaming_remote", /WebAssembly\.(?:instantiate|compile)Streaming\s*\(\s*fetch\s*\(\s*["'`]https?:/],
];

function stripComments(code: string): string {
  return code.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:"'`\\])\/\/[^\n]*/g, "$1");
}

function manifestFindings(manifest: Record<string, unknown>, variant: Variant): Finding[] {
  const out: Finding[] = [];
  const add = (rule: string, detail: string): void => void out.push({ rule, detail });
  if (manifest.manifest_version !== 3) add("manifest.version", `manifest_version ${String(manifest.manifest_version)}`);
  for (const k of FORBIDDEN_KEYS) if (k in manifest) add("manifest.forbidden_key", k);
  const permissions = (manifest.permissions as string[] | undefined) ?? [];
  for (const p of permissions) {
    if (FORBIDDEN_PERMISSIONS.includes(p)) add("manifest.forbidden_permission", p);
    else if (variant === "product" && !PRODUCT_PERMISSIONS.includes(p)) add("manifest.unexpected_permission", p);
  }
  for (const p of PRODUCT_PERMISSIONS) if (!permissions.includes(p)) add("manifest.missing_permission", p);
  const hosts = (manifest.host_permissions as string[] | undefined) ?? [];
  if (variant === "product" && hosts.length > 0) add("manifest.install_time_hosts", hosts.join(","));
  for (const h of hosts) if (ALL_HOSTS.test(h)) add("manifest.all_hosts", h);
  const optional = (manifest.optional_host_permissions as string[] | undefined) ?? [];
  for (const h of optional) if (!OPTIONAL_HOSTS.includes(h)) add("manifest.optional_hosts", h);
  if (variant === "product" && optional.length === 0) add("manifest.no_runtime_request", "optional_host_permissions");
  if ("optional_permissions" in manifest) add("manifest.optional_permissions", "unreviewed optional permissions");
  const bg = manifest.background as { service_worker?: string; type?: string } | undefined;
  if (bg?.service_worker !== "background.js" || bg.type !== "module") add("manifest.background", JSON.stringify(bg));
  const csp = (manifest.content_security_policy as { extension_pages?: string } | undefined)?.extension_pages;
  if (csp !== "script-src 'self' 'wasm-unsafe-eval'; object-src 'self'") add("manifest.csp", String(csp));
  if (typeof manifest.key !== "string") add("manifest.key", "no pinned key");
  return out;
}

/** Audits an unpacked extension directory against the reviewed MV3 rules: packaged code only, minimal grants. */
export function auditPackage(dir: string, variant: Variant): Finding[] {
  const out: Finding[] = [];
  const add = (rule: string, detail: string): void => void out.push({ rule, detail });
  const files = readdirSync(dir).sort();
  for (const f of files) {
    if (statSync(join(dir, f)).isDirectory()) add("package.directory", f);
    else if (!(PACKAGE_FILES as readonly string[]).includes(f)) add("package.unexpected_file", f);
  }
  for (const f of PACKAGE_FILES) if (!files.includes(f)) add("package.missing_file", f);
  if (files.includes("manifest.json"))
    out.push(...manifestFindings(JSON.parse(readFileSync(join(dir, "manifest.json"), "utf8")), variant));
  for (const f of files.filter((n) => n.endsWith(".js"))) {
    const code = stripComments(readFileSync(join(dir, f), "utf8"));
    for (const [rule, re] of CODE_SINKS) if (re.test(code)) add(`code.${rule}`, f);
    for (const m of code.matchAll(/["'`](https?:\/\/[^"'`\s]+)["'`]/g)) {
      const url = m[1] as string;
      if (!NAMESPACE_URLS.some((re) => re.test(url))) add("code.remote_url", `${f}: ${url}`);
    }
  }
  if (files.includes("panel.html")) {
    const html = readFileSync(join(dir, "panel.html"), "utf8");
    for (const m of html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)) {
      const src = /\bsrc\s*=\s*["']([^"']+)["']/i.exec(m[1] ?? "")?.[1];
      if (src === undefined || /^[a-z][a-z0-9+.-]*:|^\/\//i.test(src)) add("html.script_source", src ?? "inline");
      else if (!files.includes(src)) add("html.script_missing", src);
      if ((m[2] ?? "").trim() !== "") add("html.inline_script", "panel.html");
    }
    if (/\son[a-z]+\s*=/i.test(html)) add("html.inline_handler", "panel.html");
    if (/<(?:iframe|object|embed|link|base)\b/i.test(html)) add("html.embedding", "panel.html");
  }
  if (files.includes("vsf_wasm.wasm")) {
    const module = new WebAssembly.Module(readFileSync(join(dir, "vsf_wasm.wasm")));
    const imports = WebAssembly.Module.imports(module);
    if (imports.length > 0) add("wasm.imports", imports.map((i) => `${i.module}.${i.name}`).join(","));
  }
  return out;
}
