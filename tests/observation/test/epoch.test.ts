import assert from "node:assert/strict";
import { test } from "node:test";
import { persistentEpochs } from "@vsf/observer";

function fakeWindow(broken = false): Window {
  const data = new Map<string, string>();
  const storage = {
    getItem: (k: string) => {
      if (broken) throw new Error("denied");
      return data.get(k) ?? null;
    },
    setItem: (k: string, v: string) => {
      if (broken) throw new Error("denied");
      data.set(k, v);
    },
    clear: () => data.clear(),
  };
  return { sessionStorage: storage } as unknown as Window;
}

test("epochs rise strictly even when the clock stands still or runs backwards", () => {
  const win = fakeWindow();
  let t = 1_000;
  const next = persistentEpochs(win, () => t);
  const a = next();
  const b = next();
  t = 500;
  const c = next();
  assert.ok(a < b && b < c, `${a} ${b} ${c}`);
});

test("lost storage restarts from the clock, so a reload never reuses an old epoch", () => {
  const win = fakeWindow();
  let t = 1_000;
  const first = persistentEpochs(win, () => t)();
  win.sessionStorage.clear();
  t = 2_000;
  const second = persistentEpochs(win, () => t)();
  assert.ok(second > first);
});

test("unusable storage still yields increasing epochs from memory", () => {
  const next = persistentEpochs(fakeWindow(true), () => 7);
  const a = next();
  const b = next();
  const c = next();
  assert.deepEqual([a, b, c], [7, 8, 9]);
});

test("epochs stay inside the IR ObservationEpoch range", () => {
  assert.ok(persistentEpochs(fakeWindow())() <= Number.MAX_SAFE_INTEGER);
});
