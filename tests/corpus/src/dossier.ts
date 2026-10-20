import type { Capture } from "./capture.ts";
import { sha256 } from "./evaluate.ts";
import type { Page, Source } from "./manifest.ts";

/** What an independent annotator sees: the browser's own accessibility tree and the application's own description, never VSF output. */
export function dossierOf(s: Source, p: Page, c: Capture): { text: string; sha256: string } {
  const text = [
    `# ${s.name} - ${p.id}`,
    `Upstream: ${s.upstream}`,
    `Framework: ${s.framework}`,
    `Ground truth basis: ${s.groundTruth.basis.join("; ")}`,
    `Page title: ${c.title}`,
    p.state ? `State: ${p.state}` : "State: as loaded",
    "What people do here (from the manifest):",
    ...p.workflows.map((w) => `- ${w}`),
    "",
    "## Accessibility tree (Chromium, full page)",
    "```yaml",
    c.aria.trim(),
    "```",
    "",
  ].join("\n");
  return { text, sha256: sha256(text) };
}
