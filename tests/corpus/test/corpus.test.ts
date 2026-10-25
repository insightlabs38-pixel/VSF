import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { test } from "node:test";
import { type Annotation, validateAnnotation } from "../src/annotation.ts";
import { structureDigest } from "../src/capture.ts";
import { parseAlias, parseCdn } from "../src/cdn.ts";
import { clusters } from "../src/evaluate.ts";
import { loadManifest, type Manifest, validateManifest } from "../src/manifest.ts";
import { actionAgreement, aggregate, fieldAgreement, matchPage, norm, type Recognition } from "../src/match.ts";
import { serveStatic } from "../src/serve.ts";

const base = (): Manifest => JSON.parse(JSON.stringify(loadManifest()));

test("the shipped manifest satisfies every independence rule", () => {
  assert.deepEqual(validateManifest(loadManifest()), []);
  const m = loadManifest();
  assert.ok(m.sources.length >= 8, "at least eight independently developed applications");
  for (const s of m.sources) assert.ok(s.pages.length > 0);
});

test("a manifest that is unpinned, unlicensed, contaminated or unannotated is refused", () => {
  const cases: Record<string, (m: Manifest) => void> = {
    "branch instead of a revision": (m) => {
      if (m.sources[0]?.fetch.kind === "git") m.sources[0].fetch.rev = "main";
    },
    "no license": (m) => {
      (m.sources[0] as { license: string }).license = "";
    },
    "no ground truth annotator": (m) => {
      (m.sources[0] as { groundTruth: { annotator: string } }).groundTruth.annotator = "";
    },
    "duplicate source": (m) => {
      m.sources.push(structuredClone(m.sources[0] as never));
    },
    "family split across partitions": (m) => {
      const a = m.sources.find((s) => s.partition === "heldout") as { family: string };
      const b = m.sources.find((s) => s.partition === "dev") as { family: string };
      b.family = a.family;
    },
    "release without a digest": (m) => {
      const r = m.sources.find((s) => s.fetch.kind === "release");
      if (r?.fetch.kind === "release") r.fetch.sha256 = "x";
    },
    "package requirement without a hash": (m) => {
      const r = m.sources.find((s) => s.fetch.kind === "pip");
      if (r?.fetch.kind === "pip") r.fetch.requirements[0] = "Django==5.2.18";
    },
    "package requirement left unpinned": (m) => {
      const r = m.sources.find((s) => s.fetch.kind === "pip");
      if (r?.fetch.kind === "pip") r.fetch.requirements[0] = `Django>=5 --hash=sha256:${"a".repeat(64)}`;
    },
    "unknown partition": (m) => {
      (m.sources[0] as { partition: string }).partition = "test";
    },
  };
  for (const [name, mut] of Object.entries(cases)) {
    const m = base();
    mut(m);
    assert.notDeepEqual(validateManifest(m), [], name);
  }
});

test("only pinned-version npm CDN addresses are mirrored", () => {
  assert.deepEqual(parseCdn("https://cdn.jsdelivr.net/npm/bootstrap@5.3.8/dist/js/bootstrap.min.js"), {
    name: "bootstrap",
    version: "5.3.8",
    path: "dist/js/bootstrap.min.js",
  });
  assert.deepEqual(parseCdn("https://cdn.jsdelivr.net/npm/@fontsource/source-sans-3@5.3.0/index.css?x=1"), {
    name: "@fontsource/source-sans-3",
    version: "5.3.0",
    path: "index.css",
  });
  assert.deepEqual(parseCdn("https://code.jquery.com/jquery-3.5.1.js"), {
    name: "jquery",
    version: "3.5.1",
    path: "dist/jquery.js",
  });
  assert.equal(parseCdn("https://code.jquery.com/jquery-latest.js"), null);
  for (const bad of [
    "https://cdn.jsdelivr.net/npm/bootstrap@5/dist/x.js",
    "https://cdn.jsdelivr.net/npm/bootstrap/dist/x.js",
    "https://cdn.jsdelivr.net/npm/bootstrap@5.3.8/../x.js",
    "https://evil.example/npm/bootstrap@5.3.8/x.js",
    "https://cdn.jsdelivr.net/gh/a/b@1.0.0/x.js",
  ])
    assert.equal(parseCdn(bad), null, bad);
});

test("the static server stays inside its root", async () => {
  const dir = mkdtempSync(`${tmpdir()}/corpus-serve-`);
  mkdirSync(`${dir}/site`);
  writeFileSync(`${dir}/site/index.html`, "<p>hi</p>");
  writeFileSync(`${dir}/secret.txt`, "no");
  const s = await serveStatic(`${dir}/site`);
  try {
    assert.equal(await (await fetch(`${s.origin}/`)).text(), "<p>hi</p>");
    assert.equal((await fetch(`${s.origin}/..%2fsecret.txt`)).status === 200, false);
    const raw = await fetch(`${s.origin}/%2e%2e/secret.txt`);
    assert.notEqual(await raw.text(), "no");
    assert.equal((await fetch(`${s.origin}/missing`)).status, 404);
  } finally {
    await s.stop();
  }
});

test("names normalize the way people read them", () => {
  assert.equal(norm("  E-mail *: "), "e mail");
  assert.equal(norm("Zip (required)"), "zip");
  assert.equal(norm("Größe"), "größe");
});

const ann = (over: Partial<Annotation> = {}): Annotation => ({
  schema: "vsf-corpus-annotation-v1",
  source: "s",
  page: "p",
  dossierSha256: "0".repeat(64),
  annotator: { kind: "separate-agent", id: "a", sawRecognizerOutput: false, basis: ["tree"] },
  fields: [],
  actions: [],
  collections: [],
  composites: [],
  workflows: [],
  unsupported: [],
  ...over,
});

test("agreement distinguishes exact, coarser, unclassified and contradiction", () => {
  assert.equal(fieldAgreement("text", "text"), "exact");
  assert.equal(fieldAgreement("date", "text"), "coarser");
  assert.equal(fieldAgreement("file", "text"), "contradict");
  assert.equal(fieldAgreement("toggle", "text"), "contradict");
  assert.equal(actionAgreement("destructive", ["destroy"]), "exact");
  assert.equal(actionAgreement("read", []), "unclassified");
  assert.equal(actionAgreement("read", ["destroy"]), "contradict");
  assert.equal(actionAgreement("unknown", []), "exact");
});

test("matching counts found, missing, wrong and unannotated units and never invents a success", () => {
  const rec: Recognition = {
    units: [
      { type: "field", node: 4, kind: "text", binding: { state: "bound", text: "Email:" }, usable: true },
      { type: "field", node: 9, kind: "text", binding: { state: "bound", text: "Elsewhere" }, usable: true },
      { type: "action", node: 5, name: "Delete", verbs: ["destroy"], usable: true },
    ],
  };
  const a = ann({
    fields: [
      { name: "Email", kind: "text" },
      { name: "Photo", kind: "file" },
    ],
    actions: [{ name: "Delete", effect: "state_change" }],
    composites: [
      { kind: "dialog", name: "Confirm" },
      { kind: "pagination", name: "Pages" },
    ],
    workflows: [
      { name: "edit", needs: ["Email"], effect: "state_change" },
      { name: "upload", needs: ["Photo"], effect: "state_change" },
      { name: "confirm", needs: ["Confirm"], effect: "destructive" },
    ],
  });
  const r = matchPage(a, rec);
  assert.equal(r.fields.annotated, 2);
  assert.equal(r.fields.found, 1);
  assert.equal(r.fields.exact, 1);
  assert.equal(r.actions.found, 1);
  assert.equal(r.actions.contradict, 1, "Delete is destructive, not a state change");
  assert.equal(r.recognizedOnPage.fields, 2);
  assert.equal(r.unannotated.fields, 1, "the second field nobody annotated is reported, not scored");
  assert.equal(r.composites.annotated, 2);
  assert.equal(r.composites.recognized, 0);
  assert.equal(r.workflows.supported, 1);
  assert.deepEqual(r.workflows.unsupported, ["upload", "confirm"]);
  assert.equal(r.falseClaims, 1);
  const g = aggregate([r, r]);
  assert.equal(g.fields.annotated, 4);
  assert.equal(g.composites.byKind.dialog, 2);
});

test("a recognized unit that contradicts the annotation is a false claim", () => {
  const rec: Recognition = {
    units: [{ type: "field", node: 4, kind: "text", binding: { state: "bound", text: "Photo" }, usable: true }],
  };
  const r = matchPage(ann({ fields: [{ name: "Photo", kind: "file" }] }), rec);
  assert.equal(r.fields.contradict, 1);
  assert.equal(r.falseClaims, 1);
});

test("annotations must declare independence and use the closed vocabularies", () => {
  assert.deepEqual(validateAnnotation(ann()), []);
  const bad = ann({ annotator: { kind: "separate-agent", id: "a", sawRecognizerOutput: true as never, basis: ["x"] } });
  assert.notDeepEqual(validateAnnotation(bad), []);
  assert.notDeepEqual(validateAnnotation(ann({ actions: [{ name: "x", effect: "maybe" as never }] })), []);
  assert.notDeepEqual(validateAnnotation(ann({ composites: [{ kind: "carousel" as never, name: "c" }] })), []);
  assert.notDeepEqual(validateAnnotation({ ...ann(), dossierSha256: "short" }), []);
});

test("clusters rank by independent families first, then blocked workflows, and hide examples when asked", () => {
  const item = (cluster: string, name: string) => ({
    k: "action" as const,
    name,
    want: "read",
    outcome: "unclassified" as const,
    cluster,
  });
  const row = (
    source: string,
    page: string,
    items: ReturnType<typeof item>[],
    blockedBy: Record<string, number> = {},
  ) =>
    ({
      source,
      page,
      partition: "dev",
      nodes: 1,
      recognizeMs: 1,
      rssKb: 1,
      captureMs: 1,
      blocked: 0,
      mirrored: 0,
      result: { items, workflows: { blockedBy } },
    }) as never;
  const rows = [
    row("a", "p1", [item("x", "one"), item("x", "two")], { x: 1 }),
    row("b", "p1", [item("x", "three"), item("y", "four")], { y: 5 }),
    row("a", "p2", [item("z", "five")], { z: 9 }),
  ];
  const ranked = clusters(rows, (s) => (s === "a" ? "fam-a" : "fam-b"), true);
  assert.deepEqual(
    ranked.map((c) => c.key),
    ["x", "z", "y"],
  );
  assert.equal(ranked[0]?.instances, 3);
  assert.equal(ranked[0]?.families, 2);
  assert.equal(ranked[1]?.workflowsBlocked, 9);
  assert.deepEqual(
    clusters(rows, () => "f", false).map((c) => c.examples.length),
    [0, 0, 0],
  );
});

test("aliases and mounts only reach pinned packages and the served tree", async () => {
  assert.deepEqual(parseAlias("tabulator-tables@6.6.1/dist"), {
    name: "tabulator-tables",
    version: "6.6.1",
    path: "dist",
  });
  assert.equal(parseAlias("tabulator-tables@latest/dist"), null);
  assert.equal(parseAlias("tabulator-tables@6.6.1/../x"), null);
  const dir = mkdtempSync(`${tmpdir()}/corpus-mount-`);
  mkdirSync(`${dir}/site`);
  mkdirSync(`${dir}/pkg`);
  writeFileSync(`${dir}/site/index.html`, "site");
  writeFileSync(`${dir}/pkg/lib.js`, "lib");
  writeFileSync(`${dir}/secret.txt`, "no");
  const s = await serveStatic(`${dir}/site`, { "/dist": `${dir}/pkg` });
  try {
    assert.equal(await (await fetch(`${s.origin}/dist/lib.js`)).text(), "lib");
    assert.notEqual(await (await fetch(`${s.origin}/dist/..%2fsecret.txt`)).text(), "no");
    assert.notEqual(await (await fetch(`${s.origin}/dist/%2e%2e/secret.txt`)).text(), "no");
    assert.equal(await (await fetch(`${s.origin}/`)).text(), "site");
  } finally {
    await s.stop();
  }
});

test("host disk usage shown by a page does not change its structure digest, other text does", () => {
  const node = (text: string) => [{ id: 1, parent: -1, tag: "p", role: null, name: null, text, flags: 1 }];
  const a = structureDigest(node("18.5 GiB of 252 GiB used"));
  assert.equal(a, structureDigest(node("3 MiB of 40 GiB used")));
  assert.notEqual(a, structureDigest(node("18.5 GiB of 252 GiB free")));
});
