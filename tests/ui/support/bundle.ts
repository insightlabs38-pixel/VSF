import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const entry = fileURLToPath(new URL("./page-entry.ts", import.meta.url));
let cached: string | null = null;

/** The renderer and a small harness as one script, as a content script would carry it. */
export async function pageBundle(): Promise<string> {
  if (cached) return cached;
  const out = await build({
    entryPoints: [entry],
    bundle: true,
    write: false,
    format: "iife",
    target: "chrome120",
    conditions: ["source"],
    logLevel: "error",
  });
  cached = out.outputFiles[0]?.text ?? "";
  return cached;
}
