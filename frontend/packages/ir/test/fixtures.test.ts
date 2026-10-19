import assert from "node:assert/strict";
import { test } from "node:test";
import { parse, sha256Hex } from "@vsf/schema";
import {
  decodeDocument,
  decodeDocumentValue,
  documentDigestAt,
  encodeDocument,
  encodeDocumentAt,
  IrError,
  typedDigest,
} from "../src/index.ts";
import { loadCases, readFixture, validBytes, validNames } from "../src/testing.ts";

const names = validNames();

test("every valid fixture decodes and re-encodes to identical bytes", () => {
  assert.ok(names.length >= 10);
  for (const name of names) {
    const bytes = validBytes(name);
    const doc = decodeDocumentValue(parse(bytes));
    const body = doc.unsafe();
    const version = doc.version;
    assert.deepEqual(encodeDocumentAt(body, version), bytes, name);
    assert.deepEqual(
      decodeDocument(encodeDocument(body)).unsafe(),
      body,
      `${name}: re-encoding at the current version`,
    );
  }
});

test("pinned digests written by the Rust authority match this implementation", async () => {
  const pins = parse(new TextEncoder().encode(readFixture("pins.json"))) as Map<string, unknown>;
  const entries = pins.get("entries") as Map<string, unknown>[];
  assert.deepEqual(
    entries.map((e) => e.get("name")),
    names,
  );
  for (const e of entries) {
    const name = e.get("name") as string;
    const bytes = validBytes(name);
    assert.equal(bytes.length, e.get("canonical_bytes"), name);
    assert.equal(await sha256Hex(bytes), e.get("canonical_sha256"), name);
    const doc = decodeDocumentValue(parse(bytes));
    const body = doc.unsafe();
    const version = doc.version;
    assert.equal(await documentDigestAt(body, version), e.get("document_digest"), name);
  }
});

test("NFC and NFD text are preserved and stay distinct", () => {
  const body = decodeDocument(validBytes("semantic-orders")).unsafe();
  assert.equal(body.family, "semantic");
  if (body.family !== "semantic") return;
  const labels = body.body.entities.map((e) => e.label);
  assert.ok(labels.includes("Café" as never));
  assert.ok(labels.includes("Café" as never));
  assert.notEqual("Café", "Café");
});

test("typed digests are domain separated", async () => {
  const c = new TextEncoder().encode("{}");
  assert.notEqual(await typedDigest("document:operation", c), await typedDigest("document:semantic", c));
  assert.notEqual(await typedDigest("a", c), `sha256:${await sha256Hex(c)}`);
});

test("every invalid case fails closed with the expected code and path", () => {
  const cases = loadCases("invalid/cases.json");
  assert.ok(cases.length >= 70);
  for (const c of cases) {
    const expect = c.expect as Map<string, string>;
    assert.throws(
      () => decodeDocument(c.bytes),
      (err: unknown) => err instanceof IrError && err.code === expect.get("code") && err.path === expect.get("path"),
      c.name,
    );
  }
});
