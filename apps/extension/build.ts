import { createHash } from "node:crypto";
import { cpSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "../..");

export interface Variant {
  name: string;
  hostPermissions: string[];
  extraPermissions?: string[];
}

export function extensionId(manifestKeyB64: string): string {
  const hex = createHash("sha256").update(Buffer.from(manifestKeyB64, "base64")).digest("hex").slice(0, 32);
  return [...hex].map((c) => String.fromCharCode(97 + Number.parseInt(c, 16))).join("");
}

function listFiles(dir: string): string[] {
  return readdirSync(dir)
    .sort()
    .flatMap((n) => {
      const p = join(dir, n);
      return statSync(p).isDirectory() ? listFiles(p) : [p];
    });
}

/** Packages the extension; the product variant requests host access at runtime, the test variant pins it. */
export async function buildExtension(v: Variant, outRoot = join(root, "artifacts/extension")): Promise<string> {
  const out = join(outRoot, v.name);
  rmSync(out, { recursive: true, force: true });
  mkdirSync(out, { recursive: true });
  const common = {
    bundle: true,
    minify: false,
    target: "chrome120",
    legalComments: "none" as const,
    logLevel: "error" as const,
  };
  await build({
    ...common,
    entryPoints: [join(here, "src/background.ts")],
    outfile: join(out, "background.js"),
    format: "esm",
    conditions: ["source"],
  });
  await build({
    ...common,
    entryPoints: [join(here, "src/content.ts")],
    outfile: join(out, "content.js"),
    format: "iife",
    conditions: ["source"],
    footer: { js: "__vsfRun();" },
  });
  await build({
    ...common,
    entryPoints: [join(here, "src/panel.ts")],
    outfile: join(out, "panel.js"),
    format: "esm",
    conditions: ["source"],
  });
  cpSync(join(here, "src/panel.html"), join(out, "panel.html"));
  cpSync(join(root, "artifacts/wasm/vsf_wasm.wasm"), join(out, "vsf_wasm.wasm"));
  const manifest = JSON.parse(readFileSync(join(here, "manifest.template.json"), "utf8")) as Record<string, unknown>;
  if (v.hostPermissions.length > 0) manifest.host_permissions = v.hostPermissions;
  if (v.extraPermissions?.length) manifest.permissions = [...(manifest.permissions as string[]), ...v.extraPermissions];
  writeFileSync(join(out, "manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`);
  const files = listFiles(out).map((f) => ({
    path: relative(out, f),
    bytes: statSync(f).size,
    sha256: createHash("sha256").update(readFileSync(f)).digest("hex"),
  }));
  writeFileSync(
    join(outRoot, `${v.name}.build.json`),
    `${JSON.stringify({ variant: v.name, extensionId: extensionId(manifest.key as string), files }, null, 2)}\n`,
  );
  return out;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  await buildExtension({ name: "product", hostPermissions: [] });
  const hosts = (process.env.VSF_EXT_TEST_HOSTS ?? "http://127.0.0.1/*").split(",");
  await buildExtension({ name: "test", hostPermissions: hosts });
  console.log("extension built: product, test");
}
