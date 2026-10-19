import assert from "node:assert/strict";
import { readdirSync } from "node:fs";
import type { Server } from "node:http";
import { after, before, test } from "node:test";
import { escapeHtml, importMapHashes, startServer, validateForm } from "../src/server.ts";

let server: Server;
let base = "";
before(async () => {
  ({ server, url: base } = await startServer(0));
});
after(() => server.close());

const post = (body: string) =>
  fetch(`${base}/forms/submit`, {
    method: "POST",
    body,
    headers: { "content-type": "application/x-www-form-urlencoded" },
  });

test("serves the form and accepts a valid submission", async () => {
  const page = await fetch(`${base}/forms/`);
  assert.equal(page.status, 200);
  assert.match(await page.text(), /<form method="post"/);
  const ok = await post("name=Ada&email=ada%40example.test&quantity=3");
  assert.equal(ok.status, 200);
  assert.match(await ok.text(), /3 x order for Ada/);
});

test("rejects invalid input with 422 and escapes reflected values", async () => {
  const bad = await post("name=%3Cscript%3Ealert(1)%3C%2Fscript%3E&email=nope&quantity=0");
  assert.equal(bad.status, 422);
  const html = await bad.text();
  assert.ok(!html.includes("<script>alert(1)"));
  assert.match(html, /&#60;script&#62;/);
  assert.match(html, /aria-invalid="true"/);
});

test("enforces body size limit, methods and path containment", async () => {
  assert.equal((await post(`name=${"a".repeat(20000)}`)).status, 413);
  assert.equal((await fetch(`${base}/forms/submit`)).status, 405);
  assert.equal((await fetch(`${base}/forms/`, { method: "POST" })).status, 405);
  const traversal = await fetch(`${base}/assets/client/..%2F..%2Fpackage.json`);
  assert.ok([403, 404].includes(traversal.status));
  assert.equal((await fetch(`${base}/assets/client/%00`)).status === 200, false);
  assert.equal((await fetch(`${base}/nope`)).status, 404);
});

test("sets security headers", async () => {
  const res = await fetch(`${base}/`);
  assert.equal(res.headers.get("x-content-type-options"), "nosniff");
  assert.match(res.headers.get("content-security-policy") ?? "", /default-src 'self'/);
});

test("validateForm and escapeHtml boundaries", () => {
  assert.deepEqual(validateForm(new URLSearchParams("name=A&email=a@b.co&quantity=100")).errors, {});
  assert.ok(validateForm(new URLSearchParams("name=A&email=a@b.co&quantity=101")).errors.quantity);
  assert.ok(validateForm(new URLSearchParams("name=&email=a@b.co&quantity=1")).errors.name);
  assert.equal(escapeHtml(`<a href="x">&'`), "&#60;a href=&#34;x&#34;&#62;&#38;&#39;");
});

test("inline import maps are allowed only by content hash", async () => {
  const res = await fetch(`${base}/wasm-smoke/`);
  const csp = res.headers.get("content-security-policy") ?? "";
  const hashes = importMapHashes(await res.text());
  assert.equal(hashes.length, 1);
  assert.ok(csp.includes(hashes[0] as string));
  assert.ok(!csp.includes("unsafe-inline'") || !/script-src[^;]*unsafe-inline/.test(csp));
  const plain = await fetch(`${base}/forms/`);
  assert.ok(!(plain.headers.get("content-security-policy") ?? "").includes("sha256-"));
});

const SPECIFIER = /(?:^|[\s;}])(?:import|export)\s*(?:[\w*{}\s,$]*?\sfrom\s*)?["']([^"']+)["']/g;

test("every page's module graph resolves through its import map", async () => {
  const pages = readdirSync(new URL("../public", import.meta.url), { recursive: true, encoding: "utf8" }).filter((f) =>
    f.endsWith("index.html"),
  );
  let checked = 0;
  for (const page of pages) {
    const html = await (await fetch(`${base}/${page}`)).text();
    const map = JSON.parse(/<script type="importmap">([\s\S]*?)<\/script>/.exec(html)?.[1] ?? "{}").imports ?? {};
    const queue = [...html.matchAll(/<script[^>]*type="module"[^>]*src="([^"]+)"/g)].map((m) => m[1] as string);
    const seen = new Set<string>();
    while (queue.length) {
      const url = queue.pop() as string;
      if (seen.has(url)) continue;
      seen.add(url);
      const res = await fetch(new URL(url, `${base}/${page}`));
      assert.equal(res.status, 200, `${page}: ${url} is not served`);
      for (const m of (await res.text()).matchAll(SPECIFIER)) {
        const spec = m[1] as string;
        if (spec.startsWith("node:")) assert.fail(`${page}: ${url} imports ${spec}`);
        const target = /^(\.{0,2}\/)/.test(spec) ? new URL(spec, new URL(url, `${base}/${page}`)).pathname : map[spec];
        assert.ok(target, `${page}: ${url} imports "${spec}" which the import map does not resolve`);
        queue.push(target);
      }
      checked++;
    }
  }
  assert.ok(checked > 10, `module graph walk covered only ${checked} modules`);
});
