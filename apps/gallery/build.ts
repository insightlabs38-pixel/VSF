import { cpSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "../..");

/** Builds the reference gallery as plain static files: one script, one page, the embedded fonts. */
export async function buildGallery(out = join(root, "artifacts/gallery")): Promise<string> {
  rmSync(out, { recursive: true, force: true });
  mkdirSync(out, { recursive: true });
  await build({
    entryPoints: [join(here, "src/main.ts")],
    outfile: join(out, "gallery.js"),
    bundle: true,
    format: "iife",
    target: "chrome120",
    conditions: ["source"],
    legalComments: "none",
    logLevel: "error",
  });
  cpSync(join(here, "src/index.html"), join(out, "index.html"));
  cpSync(join(here, "fonts"), join(out, "fonts"), { recursive: true });
  return out;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  console.log(`gallery built: ${await buildGallery()}`);
}
