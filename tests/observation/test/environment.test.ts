import assert from "node:assert/strict";
import { test } from "node:test";
import {
  cacheOverlay,
  declaredLocale,
  type EnvironmentObservation,
  variantInputs,
  viewportBucket,
} from "@vsf/observer";

const env = (over: Partial<EnvironmentObservation> = {}): EnvironmentObservation => ({
  locale: { state: "observed", value: "en-US" },
  viewport: { state: "observed", value: { width: 1000, height: 800, dpr: 1, bucket: "lg" } },
  flags: { beta: { state: "observed", value: "on" }, hidden: { state: "unknown" } },
  ...over,
});

test("declared locales are normalised and anything the IR would reject stays unknown", () => {
  assert.equal(declaredLocale("EN_us"), "en-US");
  assert.equal(declaredLocale("ar"), "ar");
  assert.equal(declaredLocale(" zh-Hant-TW "), "zh-Hant-TW");
  assert.equal(declaredLocale("zh-hant-tw"), "zh-Hant-TW");
  for (const bad of ["", "e", "english", "en-", "en--US", "12-34", "en-US-x-toolongsubtag", null, undefined]) {
    assert.equal(declaredLocale(bad), null, String(bad));
  }
});

test("viewport buckets change exactly at the stated widths", () => {
  const at = (w: number) => viewportBucket(w);
  assert.deepEqual(
    [at(0), at(575), at(576), at(767), at(768), at(991), at(992), at(1199), at(1200)],
    ["xs", "xs", "sm", "sm", "md", "md", "lg", "lg", "xl"],
  );
});

test("the overlay uses only what is required and marks unknowns with a symbol no value can take", () => {
  const needs = { locale: true, viewport: false, flags: ["beta"] };
  const o = cacheOverlay(env(), needs);
  assert.equal(o.content, "env1;loc=en-US;f.beta=on");
  assert.equal(o.shareable, true);
  const noLocale = cacheOverlay(env({ locale: { state: "unknown" } }), needs);
  assert.equal(noLocale.content, "env1;loc=?;f.beta=on");
  assert.equal(noLocale.shareable, false);
  assert.deepEqual(noLocale.unknown, ["locale"]);
  const unseen = cacheOverlay(env(), { locale: false, viewport: false, flags: ["hidden", "missing"] });
  assert.equal(unseen.content, "env1;f.hidden=?;f.missing=?");
  assert.deepEqual(unseen.unknown, ["flag:hidden", "flag:missing"]);
  const literalQuestion = cacheOverlay(env({ flags: { beta: { state: "observed", value: "?" } } }), needs);
  assert.notEqual(literalQuestion.content, "env1;loc=en-US;f.beta=?");
});

test("flag order and awkward values cannot alias two different environments", () => {
  const a = cacheOverlay(env({ flags: { a: { state: "observed", value: "1;f.b=2" } } }), {
    locale: false,
    viewport: false,
    flags: ["a", "b"],
  });
  const b = cacheOverlay(
    env({ flags: { a: { state: "observed", value: "1" }, b: { state: "observed", value: "2" } } }),
    { locale: false, viewport: false, flags: ["b", "a"] },
  );
  assert.notEqual(a.content, b.content);
  assert.equal(b.content, "env1;f.a=1;f.b=2");
});

test("variant inputs never invent a locale or a flag value", () => {
  const v = variantInputs(env());
  assert.deepEqual(v, { locale: "en-US", flags: [["beta", "on"]], unknownFlags: ["hidden"] });
  assert.equal(variantInputs(env({ locale: { state: "unknown" } })).locale, null);
});
