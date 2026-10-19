import assert from "node:assert/strict";
import { test } from "node:test";
import {
  type SharePolicy,
  stricter,
  textNamesAccount,
  textNamesCredential,
  validateSharePolicy,
  wordsOf,
} from "@vsf/observer";

const ctx = {
  origin: "https://app.example",
  principal: "prin_0123456789abcdef0123456789abcdef",
  now: 1_790_000_000_000,
};
const good = (): SharePolicy => ({
  schema: "vsf-share-policy-v1",
  id: "spol_0123456789abcdef",
  origin: ctx.origin,
  principal_ref: ctx.principal,
  text_roles: ["button", "columnheader"],
  max_chars: 40,
  issued_at: ctx.now - 1000,
  expires_at: ctx.now + 86_400_000,
});
const reason = (p: unknown): string | null => {
  const r = validateSharePolicy(p, ctx);
  return r.ok ? null : r.reason;
};

test("a complete policy for this origin and principal validates", () => {
  assert.equal(reason(good()), null);
});

test("forged, foreign, expired and malformed policies are refused with a reason", () => {
  assert.equal(reason(null), "policy.not_object");
  assert.equal(reason([]), "policy.not_object");
  assert.equal(reason("{}"), "policy.not_object");
  assert.equal(reason({ ...good(), extra: 1 }), "policy.keys");
  const { max_chars: _drop, ...partial } = good();
  assert.equal(reason(partial), "policy.keys");
  assert.equal(reason({ ...good(), schema: "vsf-share-policy-v2" }), "policy.schema");
  assert.equal(reason({ ...good(), id: "policy-1" }), "policy.id");
  assert.equal(reason({ ...good(), origin: "https://evil.example" }), "policy.origin");
  assert.equal(reason({ ...good(), principal_ref: "prin_ffffffffffffffffffffffffffffffff" }), "policy.principal");
  assert.equal(reason({ ...good(), expires_at: ctx.now }), "policy.expired");
  assert.equal(reason({ ...good(), issued_at: ctx.now + 3_600_000 }), "policy.not_yet_valid");
  assert.equal(reason({ ...good(), issued_at: ctx.now - 40 * 86_400_000 }), "policy.lifetime");
  assert.equal(reason({ ...good(), max_chars: 0 }), "policy.max_chars");
  assert.equal(reason({ ...good(), max_chars: 500 }), "policy.max_chars");
  assert.equal(reason({ ...good(), issued_at: 1.5 }), "policy.time");
});

test("a policy can never name a data-bearing or unknown role", () => {
  for (const roles of [["cell"], ["textbox"], ["gridcell", "button"], ["button", "button"], [], ["nonsense"], [1]]) {
    assert.equal(reason({ ...good(), text_roles: roles }), "policy.roles", JSON.stringify(roles));
  }
});

test("scope ranking: the stricter wins and public never lowers anything", () => {
  assert.equal(stricter("credential", "public"), "credential");
  assert.equal(stricter("public", "credential"), "credential");
  assert.equal(stricter("account", "credential"), "credential");
  assert.equal(stricter("public", "account"), "account");
  assert.equal(stricter("public", "public"), "public");
});

test("credential words are matched as words, not substrings", () => {
  for (const s of [
    "Password",
    "newPassword",
    "api_token",
    "x-api-key",
    "clientSecret",
    "Security code",
    "One-time code",
    "Card number",
    "PIN",
    "Recovery code",
    "apiKey",
  ]) {
    assert.equal(textNamesCredential(s), true, s);
  }
  for (const s of [
    "Passenger name",
    "Compass heading",
    "Bypass cache",
    "Tokenizer settings",
    "Pinned items",
    "Notes",
    "",
  ]) {
    assert.equal(textNamesCredential(s), false, s);
  }
  assert.equal(textNamesCredential(null), false);
});

test("account words mark personal data, ordinary labels stay public", () => {
  for (const s of ["Email", "Phone number", "Home address", "First name", "Date of birth", "userName"]) {
    assert.equal(textNamesAccount(s), true, s);
  }
  for (const s of ["Product name", "Quantity", "Notes", "Due date"]) assert.equal(textNamesAccount(s), false, s);
  assert.deepEqual(wordsOf("clientSecret_v2"), ["client", "secret", "v2"]);
});
