import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { test } from "node:test";
import { chromium } from "@playwright/test";
import type { Annotation } from "../../src/annotation.ts";
import { recognizedRun } from "../../src/execute.ts";
import type { Page, Source } from "../../src/manifest.ts";
import type { Operation } from "../../src/operations.ts";
import { serveStatic } from "../../src/serve.ts";

const upload = (second: boolean): string => `<!doctype html><title>u</title><main>
<label>Attachment <input type="file" id="f"></label>
${second ? '<label>Photo <input type="file" id="g"></label>' : ""}
<div id="out" role="status"></div>
<script>
for (const input of document.querySelectorAll("input[type=file]")) input.addEventListener("change", async () => {
  const f = input.files[0];
  const h = await crypto.subtle.digest("SHA-256", await f.arrayBuffer());
  document.getElementById("out").textContent = f.name + "|" + f.size + "|" + [...new Uint8Array(h)].map((b) => b.toString(16).padStart(2, "0")).join("");
});
</script></main>`;

const ann = (): Annotation => ({
  schema: "vsf-corpus-annotation-v1",
  source: "toy",
  page: "p",
  dossierSha256: "0".repeat(64),
  annotator: { kind: "human", id: "test", sawRecognizerOutput: false, basis: ["test"] },
  fields: [
    { name: "Attachment", kind: "file" },
    { name: "Photo", kind: "file" },
  ],
  actions: [],
  collections: [],
  composites: [{ kind: "file_input", name: "Attachment" }],
  workflows: [],
  unsupported: [],
});

const source = (): Source =>
  ({ id: "toy", aliases: {}, partition: "dev", fetch: { kind: "git", url: "", rev: "" } }) as unknown as Source;
const page: Page = { id: "p", path: "/index.html", workflows: ["attach a file"] };

async function withPage<T>(html: string, f: (origin: string, tmp: string) => Promise<T>): Promise<T> {
  const dir = mkdtempSync(`${tmpdir()}/toy-file-`);
  writeFileSync(`${dir}/index.html`, html);
  const running = await serveStatic(dir);
  const tmp = mkdtempSync(`${tmpdir()}/toy-file-rec-`);
  mkdirSync(tmp, { recursive: true });
  try {
    return await f(running.origin, tmp);
  } finally {
    await running.stop();
  }
}

const sha = (s: string): string => createHash("sha256").update(s).digest("hex");

test("a file with an awkward name reaches the page with its exact name, size and bytes", async () => {
  const browser = await chromium.launch({ channel: "chromium" });
  try {
    await withPage(upload(false), async (origin, tmp) => {
      for (const name of ['report "final" (2) ü.txt', "日本語 レポート.csv", "a b  c.txt"]) {
        const text = `content of ${name}\n`;
        const o: Operation = {
          name: "attach",
          effect: "state_change",
          steps: [{ do: "upload", target: "Attachment", value: name, text }],
          expect: [{ kind: "text", contains: `${name}|${Buffer.byteLength(text)}|${sha(text)}` }],
        };
        const r = await recognizedRun(browser, origin, source(), page, ann(), o, tmp);
        assert.equal(r.outcome, "verified", `${name}: ${r.detail}`);
      }
    });
  } finally {
    await browser.close();
  }
});

test("two file inputs under the browser's own name are ambiguous and nothing is sent to either", async () => {
  const browser = await chromium.launch({ channel: "chromium" });
  try {
    await withPage(upload(true), async (origin, tmp) => {
      const o: Operation = {
        name: "attach",
        effect: "state_change",
        steps: [{ do: "upload", target: "Choose File", value: "x.txt", text: "x" }],
        expect: [{ kind: "text", contains: "x.txt|1|" }],
      };
      const a: Annotation = { ...ann(), fields: [...ann().fields, { name: "Choose File", kind: "file" }] };
      const r = await recognizedRun(browser, origin, source(), page, a, o, tmp);
      assert.equal(r.outcome, "fallback");
      assert.match(r.detail ?? "", /ambiguous/);
      const named: Operation = {
        ...o,
        steps: [{ do: "upload", target: "Photo", value: "p.txt", text: "p" }],
        expect: [{ kind: "text", contains: `p.txt|1|${sha("p")}` }],
      };
      assert.equal((await recognizedRun(browser, origin, source(), page, ann(), named, tmp)).outcome, "verified");
    });
  } finally {
    await browser.close();
  }
});
