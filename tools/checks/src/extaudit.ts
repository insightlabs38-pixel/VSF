import { createHash } from "node:crypto";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import ts from "typescript";

export interface AuditFinding {
  rule: string;
  file: string;
  message: string;
}

export type FileClass = "code" | "data";

export interface AuditFile {
  path: string;
  bytes: number;
  sha256: string;
  class: FileClass;
  reason: string;
}

export interface AuditPolicy {
  permissions: readonly string[];
  optionalHosts: readonly string[];
  csp: string;
  maxPackageBytes: number;
  /** Network sinks by file; everything else in the package must be free of them. */
  networkSinks: Readonly<Record<string, readonly string[]>>;
}

export const DEFAULT_POLICY: AuditPolicy = {
  permissions: ["activeTab", "scripting", "storage"],
  optionalHosts: ["http://*/*", "https://*/*"],
  csp: "script-src 'self' 'wasm-unsafe-eval'; object-src 'self'",
  maxPackageBytes: 8 * 1024 * 1024,
  networkSinks: {},
};

const FORBIDDEN_MANIFEST_KEYS = [
  "content_scripts",
  "externally_connectable",
  "web_accessible_resources",
  "update_url",
  "devtools_page",
  "chrome_url_overrides",
  "sandbox",
  "declarative_net_request",
  "native_messaging",
  "oauth2",
  "commands",
];

const CODE_EXT = new Set([".js", ".mjs", ".html", ".wasm"]);
const DATA_EXT = new Set([".json"]);
const NETWORK_SINKS = new Set(["fetch", "XMLHttpRequest", "WebSocket", "EventSource", "sendBeacon"]);

function listFiles(dir: string): string[] {
  return readdirSync(dir)
    .sort()
    .flatMap((n) => {
      const p = join(dir, n);
      return statSync(p).isDirectory() ? listFiles(p) : [p];
    });
}

function ext(file: string): string {
  const i = file.lastIndexOf(".");
  return i < 0 ? "" : file.slice(i);
}

function calleeName(call: ts.CallExpression | ts.NewExpression): string {
  const e = call.expression;
  if (ts.isIdentifier(e)) return e.text;
  if (ts.isPropertyAccessExpression(e)) return e.name.text;
  return "";
}

const isGetUrl = (arg: ts.Expression | undefined): boolean =>
  arg !== undefined &&
  ts.isCallExpression(arg) &&
  ts.isPropertyAccessExpression(arg.expression) &&
  arg.expression.name.text === "getURL";

/** Static scan of one bundled script: dynamic code, remote code loading and unlisted network sinks. */
export function scanScript(file: string, text: string, sinks: readonly string[] = []): AuditFinding[] {
  const out: AuditFinding[] = [];
  const add = (rule: string, node: ts.Node, message: string): void => {
    const { line } = sf.getLineAndCharacterOfPosition(node.getStart(sf));
    out.push({ rule, file, message: `${message} (line ${line + 1})` });
  };
  const sf = ts.createSourceFile(file, text, ts.ScriptTarget.ES2022, true, ts.ScriptKind.JS);
  const visit = (node: ts.Node): void => {
    if (ts.isCallExpression(node) || ts.isNewExpression(node)) {
      const name = calleeName(node);
      const first = node.arguments?.[0];
      if (name === "eval" && ts.isCallExpression(node)) add("no-dynamic-code", node, "eval is forbidden");
      if (name === "Function" && ts.isNewExpression(node)) add("no-dynamic-code", node, "new Function is forbidden");
      if (name === "Function" && ts.isCallExpression(node)) add("no-dynamic-code", node, "Function() is forbidden");
      if (name === "importScripts") add("no-remote-code", node, "importScripts is forbidden");
      if ((name === "setTimeout" || name === "setInterval") && first && ts.isStringLiteralLike(first))
        add("no-dynamic-code", node, `${name} with a string is forbidden`);
      if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword)
        add("no-remote-code", node, "dynamic import is forbidden");
      if (name === "createElement" && first && ts.isStringLiteralLike(first) && first.text.toLowerCase() === "script")
        add("no-remote-code", node, "creating script elements is forbidden");
      if (name === "Worker" || name === "SharedWorker") add("no-remote-code", node, `${name} is forbidden`);
      if (NETWORK_SINKS.has(name)) {
        const local = name === "fetch" && isGetUrl(first);
        if (!local && !sinks.includes(name)) add("network-sink", node, `unlisted network sink ${name}`);
      }
      if (name === "instantiateStreaming" || name === "compileStreaming")
        add("no-remote-code", node, `${name} loads code from a response`);
    }
    if (ts.isIdentifier(node) && node.text === "XMLHttpRequest" && !sinks.includes("XMLHttpRequest")) {
      if (!ts.isNewExpression(node.parent)) add("network-sink", node, "XMLHttpRequest reference");
    }
    if (ts.isPropertyAccessExpression(node) && node.name.text === "src" && ts.isBinaryExpression(node.parent)) {
      if (node.parent.left === node && node.parent.operatorToken.kind === ts.SyntaxKind.EqualsToken)
        add("no-remote-code", node, "assigning src is forbidden");
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}

const SCRIPT_TAG = /<script\b([^>]*)>/gi;
const EVENT_ATTR = /\son[a-z]+\s*=/i;
const URL_ATTR = /\b(?:src|href|action|data)\s*=\s*["']?\s*(?:[a-z][a-z0-9+.-]*:)?\/\//i;

export function scanHtml(file: string, text: string): AuditFinding[] {
  const out: AuditFinding[] = [];
  for (const m of text.matchAll(SCRIPT_TAG)) {
    const attrs = m[1] ?? "";
    if (!/\bsrc\s*=/.test(attrs)) out.push({ rule: "no-inline-script", file, message: "inline script element" });
  }
  if (EVENT_ATTR.test(text)) out.push({ rule: "no-inline-script", file, message: "inline event handler attribute" });
  if (/javascript:/i.test(text)) out.push({ rule: "no-inline-script", file, message: "javascript: URL" });
  if (URL_ATTR.test(text)) out.push({ rule: "no-remote-code", file, message: "remote or protocol-relative URL" });
  return out;
}

/** The WASM must import nothing: it cannot reach the network, storage or the page. */
export function wasmImports(bytes: Uint8Array): { module: string; name: string }[] {
  return WebAssembly.Module.imports(new WebAssembly.Module(bytes as BufferSource)).map((i) => ({
    module: i.module,
    name: i.name,
  }));
}

function manifestFindings(manifest: Record<string, unknown>, variant: string, policy: AuditPolicy): AuditFinding[] {
  const out: AuditFinding[] = [];
  const add = (rule: string, message: string): void => void out.push({ rule, file: "manifest.json", message });
  if (manifest.manifest_version !== 3) add("manifest-mv3", "manifest_version must be 3");
  const bg = manifest.background as { service_worker?: string; scripts?: unknown } | undefined;
  if (!bg?.service_worker || bg.scripts !== undefined) add("manifest-mv3", "a packaged service worker is required");
  for (const k of FORBIDDEN_MANIFEST_KEYS) if (k in manifest) add("manifest-surface", `${k} is not allowed`);
  const perms = (manifest.permissions as string[] | undefined) ?? [];
  for (const p of perms)
    if (!policy.permissions.includes(p)) add("manifest-permissions", `permission ${p} is not allowed`);
  const optional = (manifest.optional_host_permissions as string[] | undefined) ?? [];
  for (const h of optional)
    if (!policy.optionalHosts.includes(h)) add("manifest-hosts", `optional host ${h} is not allowed`);
  if ("optional_permissions" in manifest) add("manifest-permissions", "optional_permissions is not allowed");
  const hosts = (manifest.host_permissions as string[] | undefined) ?? [];
  if (variant === "product" && hosts.length > 0)
    add("manifest-hosts", "the product build requests no install-time hosts");
  for (const h of hosts) {
    if (/<all_urls>|^\*:\/\/|^https?:\/\/\*\/|\*\.\*|^\*$/.test(h) || h === "http://*/*" || h === "https://*/*")
      add("manifest-hosts", `host pattern ${h} is too broad`);
  }
  const csp = (manifest.content_security_policy as { extension_pages?: string } | undefined)?.extension_pages;
  if (csp !== policy.csp) add("manifest-csp", "extension page policy differs from the reviewed policy");
  return out;
}

export interface AuditReport {
  variant: string;
  findings: AuditFinding[];
  files: AuditFile[];
  packageBytes: number;
  digest: string;
}

/** Audits one packaged variant directory against the distribution policy; the same rules back the written checklist. */
export function auditPackage(dir: string, variant: string, policy: AuditPolicy = DEFAULT_POLICY): AuditReport {
  const findings: AuditFinding[] = [];
  const files: AuditFile[] = [];
  const all = listFiles(dir);
  let packageBytes = 0;
  const manifestPath = join(dir, "manifest.json");
  let manifest: Record<string, unknown> = {};
  if (existsSync(manifestPath)) {
    manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as Record<string, unknown>;
    findings.push(...manifestFindings(manifest, variant, policy));
  } else findings.push({ rule: "manifest-mv3", file: "manifest.json", message: "missing manifest" });
  for (const abs of all) {
    const rel = relative(dir, abs);
    const bytes = readFileSync(abs);
    packageBytes += bytes.length;
    const e = ext(rel);
    let cls: FileClass;
    let reason: string;
    if (rel === "manifest.json") [cls, reason] = ["data", "reviewed declaration of permissions and entry points"];
    else if (e === ".wasm") [cls, reason] = ["code", "packaged compiler core; instantiated from the package only"];
    else if (e === ".html") [cls, reason] = ["code", "extension page markup; no inline script"];
    else if (CODE_EXT.has(e)) [cls, reason] = ["code", "bundled script; reviewed with the package"];
    else if (DATA_EXT.has(e)) [cls, reason] = ["data", "static data read by packaged code"];
    else {
      [cls, reason] = ["data", "unclassified"];
      findings.push({ rule: "file-types", file: rel, message: `file type ${e || "(none)"} is not allowed` });
    }
    files.push({
      path: rel,
      bytes: bytes.length,
      sha256: createHash("sha256").update(bytes).digest("hex"),
      class: cls,
      reason,
    });
    const text = bytes.toString("utf8");
    if (e === ".js" || e === ".mjs") findings.push(...scanScript(rel, text, policy.networkSinks[rel] ?? []));
    if (e === ".html") findings.push(...scanHtml(rel, text));
    if (e === ".wasm") {
      const imports = wasmImports(bytes);
      if (imports.length > 0)
        findings.push({ rule: "wasm-imports", file: rel, message: `module imports ${imports.length} host functions` });
    }
  }
  if (packageBytes > policy.maxPackageBytes)
    findings.push({ rule: "package-size", file: ".", message: `package is ${packageBytes} bytes` });
  const bg = (manifest.background as { service_worker?: string } | undefined)?.service_worker;
  if (bg && !files.some((f) => f.path === bg))
    findings.push({ rule: "manifest-mv3", file: "manifest.json", message: `service worker ${bg} is not packaged` });
  const action = (manifest.action as { default_popup?: string } | undefined)?.default_popup;
  if (action && !files.some((f) => f.path === action))
    findings.push({ rule: "manifest-mv3", file: "manifest.json", message: `popup ${action} is not packaged` });
  for (const f of files.filter((x) => x.path.endsWith(".html"))) {
    const html = readFileSync(join(dir, f.path), "utf8");
    for (const m of html.matchAll(/<script\b[^>]*\bsrc\s*=\s*["']([^"']+)["']/gi)) {
      const src = m[1] ?? "";
      if (!files.some((x) => x.path === src))
        findings.push({ rule: "no-remote-code", file: f.path, message: `script ${src} is not packaged` });
    }
  }
  const digest = createHash("sha256")
    .update(files.map((f) => `${f.path}\0${f.sha256}`).join("\n"))
    .digest("hex");
  return { variant, findings, files, packageBytes, digest };
}
