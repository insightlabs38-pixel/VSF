import assert from "node:assert/strict";
import { test } from "node:test";
import { captureCapabilities, childScopes, missingSignal, type ObservationPolicy } from "@vsf/observer";

interface FakeDoc {
  documentElement: object | null;
  location: { href: string };
}

function frame(attrs: Record<string, string>, contentDocument: FakeDoc | null): Element {
  return {
    tagName: "IFRAME",
    getAttribute: (n: string) => attrs[n] ?? null,
    ownerDocument: { baseURI: "https://app.example/page" },
    contentDocument,
    shadowRoot: null,
  } as unknown as Element;
}

const policy = (allowed: string[]): ObservationPolicy => ({ allowOrigin: (o) => allowed.includes(o) });
const gen = () => 1;

test("an accessible document whose origin is not granted becomes a policy placeholder", () => {
  const doc: FakeDoc = { documentElement: {}, location: { href: "https://other.example/x" } };
  const [s] = childScopes(frame({}, doc), "https://app.example", {}, policy(["https://app.example"]), gen);
  assert.equal(s?.access, "opaque");
  assert.equal(s?.meta.opaque, "blocked_by_policy");
  assert.equal(s?.meta.origin, "https://other.example");
  assert.deepEqual(missingSignal(s?.meta as never), { signal: "cross_origin_frame", reason: "not_permitted" });
});

test("about:blank and srcdoc documents inherit the parent origin", () => {
  for (const href of ["about:blank", "about:srcdoc"]) {
    const doc: FakeDoc = { documentElement: {}, location: { href } };
    const [s] = childScopes(frame({}, doc), "https://app.example", {}, policy(["https://app.example"]), gen);
    assert.equal(s?.access, "accessible");
    assert.equal(s?.meta.origin, "https://app.example");
  }
});

test("a frame with no readable document is classified by what the markup says", () => {
  const p = policy(["https://app.example"]);
  const kind = (attrs: Record<string, string>) =>
    childScopes(frame(attrs, null), "https://app.example", {}, p, gen)[0]?.meta;
  assert.equal(kind({ src: "https://elsewhere.example/a" })?.opaque, "cross_origin_frame");
  assert.equal(kind({ src: "https://elsewhere.example/a" })?.origin, "https://elsewhere.example");
  assert.equal(kind({ src: "/inner", sandbox: "allow-scripts" })?.opaque, "sandboxed_frame");
  assert.equal(kind({ src: "/inner" })?.opaque, "not_loaded");
  assert.equal(kind({})?.opaque, "not_loaded");
  assert.equal(kind({ src: "javascript:alert(1)" })?.opaque, "not_loaded");
  assert.equal(kind({ src: "/inner", sandbox: "allow-same-origin" })?.opaque, "not_loaded");
});

test("an unreadable origin is never guessed", () => {
  const doc: FakeDoc = { documentElement: {}, location: { href: "data:text/html,hi" } };
  const [s] = childScopes(frame({}, doc), "https://app.example", {}, policy(["https://app.example"]), gen);
  assert.equal(s?.meta.opaque, "blocked_by_policy");
  assert.equal(s?.meta.origin, null);
});

test("capability ids are valid IR capability ids and limits are stated, not implied", () => {
  const all = captureCapabilities({}, null);
  assert.ok(all.every((c) => /^[a-z][a-z0-9_.-]{0,95}$/.test(c.id)));
  assert.equal(new Set(all.map((c) => c.id)).size, all.length);
  const states = new Set(["available", "degraded", "unavailable", "unknown"]);
  assert.ok(all.every((c) => states.has(c.state)));
  assert.ok(all.filter((c) => c.state !== "available").every((c) => (c.reason ?? "").length > 0));
  const withClosed = captureCapabilities({ openOrClosedShadowRoot: () => null }, null);
  assert.equal(withClosed.find((c) => c.id === "observe.shadow.closed")?.state, "available");
  assert.equal(all.find((c) => c.id === "observe.shadow.closed")?.state, "unavailable");
});
