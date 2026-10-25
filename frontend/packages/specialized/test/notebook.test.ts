import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";
import { digestOf, type KernelMessage, Notebook } from "../src/notebook.ts";

const K = { id: "k1", session: "sess-A", epoch: 1 };

let counter = 0;
const msg = (
  type: string,
  parent: string | undefined,
  content: Record<string, unknown> = {},
  session = "sess-A",
  channel: "iopub" | "shell" = "iopub",
): KernelMessage => ({
  header: { msg_id: `m${counter++}`, msg_type: type, session },
  parent_header: parent ? { msg_id: parent } : {},
  channel,
  content,
});

const run = (nb: Notebook, id: string): string => nb.execute(id).msgId;
const finish = (nb: Notebook, parent: string, status = "ok"): void => {
  nb.receive(msg("execute_reply", parent, { status, execution_count: 1 }, "sess-A", "shell"));
  nb.receive(msg("status", parent, { execution_state: "idle" }));
};

function book(): Notebook {
  const nb = new Notebook({ ...K });
  nb.add("a", "print('hi')");
  nb.add("b", "1/0");
  nb.add("c", "x = 2");
  nb.add("md", "# title", "markdown");
  return nb;
}

test("outputs attach to the cell whose request produced them and keep the kernel's order", () => {
  const nb = book();
  const a = run(nb, "a");
  nb.receive(msg("status", a, { execution_state: "busy" }));
  nb.receive(msg("execute_input", a, { execution_count: 7 }));
  nb.receive(msg("stream", a, { name: "stdout", text: "hi\n" }));
  nb.receive(msg("stream", a, { name: "stdout", text: "there\n" }));
  nb.receive(msg("execute_result", a, { data: { "text/plain": "42" } }));
  nb.receive(msg("display_data", a, { data: { "text/plain": "<fig>" } }));
  finish(nb, a);
  const cell = nb.cells.get("a");
  assert.equal(cell?.state, "ok");
  assert.equal(cell?.executionCount, 1);
  assert.deepEqual(
    cell?.outputs.map((o) => [o.kind, o.seq]),
    [
      ["stream", 2],
      ["result", 3],
      ["display", 4],
    ],
  );
  assert.equal(cell?.outputs[0]?.mime["text/plain"], "hi\nthere\n");
  assert.deepEqual(cell?.ranIn, K);
  assert.equal(nb.cells.get("b")?.outputs.length, 0);
});

test("nothing is claimed to have run before the kernel answers", () => {
  const nb = book();
  const a = run(nb, "a");
  assert.equal(nb.cells.get("a")?.state, "running");
  nb.receive(msg("execute_reply", a, { status: "ok" }, "sess-A", "shell"));
  assert.equal(nb.cells.get("a")?.state, "ok");
  assert.equal(nb.cells.get("a")?.executionCount, null);
  // The reply alone does not free the kernel; the idle status does.
  const b = run(nb, "b");
  assert.equal(nb.cells.get("b")?.state, "queued");
  nb.receive(msg("status", a, { execution_state: "idle" }));
  assert.equal(nb.cells.get("b")?.state, "running");
  assert.ok(b);
});

test("an error is attached to its cell, fails it, and aborts what was queued behind it", () => {
  const nb = book();
  const b = run(nb, "b");
  run(nb, "c");
  assert.equal(nb.cells.get("c")?.state, "queued");
  nb.receive(msg("error", b, { ename: "ZeroDivisionError", evalue: "division by zero" }));
  finish(nb, b, "error");
  assert.equal(nb.cells.get("b")?.state, "error");
  assert.deepEqual(nb.cells.get("b")?.error, { name: "ZeroDivisionError", value: "division by zero" });
  assert.equal(nb.cells.get("b")?.outputs[0]?.kind, "error");
  assert.equal(nb.cells.get("c")?.state, "interrupted");
});

test("a restart makes every earlier output stale and a late message from the old kernel changes nothing", () => {
  const nb = book();
  const a = run(nb, "a");
  nb.receive(msg("stream", a, { text: "before\n" }));
  finish(nb, a);
  const running = run(nb, "b");
  nb.restart("sess-B");
  assert.equal(nb.kernelRef.epoch, 2);
  assert.equal(nb.cells.get("b")?.state, "interrupted");
  assert.ok(nb.cells.get("a")?.outputs.every((o) => o.stale));
  nb.receive(msg("stream", running, { text: "ghost\n" }, "sess-A"));
  nb.receive(msg("execute_reply", running, { status: "ok" }, "sess-A", "shell"));
  assert.equal(nb.cells.get("b")?.outputs.length, 0);
  assert.equal(nb.cells.get("b")?.state, "interrupted");
  assert.deepEqual(
    nb.ignored.map((i) => i.reason),
    ["wrong_session", "wrong_session"],
  );
  const again = run(nb, "a");
  nb.receive(msg("stream", again, { text: "after\n" }, "sess-B"));
  finish(nb, again);
  // finish() replies in the old session; the new run only completes with messages from the new one.
  assert.equal(nb.cells.get("a")?.outputs.length, 1);
  assert.equal(nb.cells.get("a")?.outputs[0]?.stale, false);
  assert.equal(nb.cells.get("a")?.outputs[0]?.epoch, 2);
  assert.deepEqual(nb.cells.get("a")?.ranIn, { id: "k1", session: "sess-B", epoch: 2 });
});

test("a message delivered for an earlier epoch is ignored even in the right session", () => {
  const nb = book();
  const a = run(nb, "a");
  nb.receive(msg("stream", a, { text: "late\n" }), 0);
  assert.equal(nb.cells.get("a")?.outputs.length, 0);
  assert.equal(nb.ignored[0]?.reason, "stale_epoch");
});

test("losing the connection leaves running cells unknown instead of failed or finished", () => {
  const nb = book();
  const a = run(nb, "a");
  run(nb, "b");
  nb.receive(msg("stream", a, { text: "partial\n" }));
  nb.connectionLost();
  assert.equal(nb.cells.get("a")?.state, "unknown");
  assert.equal(nb.cells.get("b")?.state, "unknown");
  assert.equal(nb.cells.get("a")?.outputs.length, 1);
  nb.receive(msg("execute_reply", a, { status: "ok" }, "sess-A", "shell"));
  assert.equal(nb.cells.get("a")?.state, "unknown");
});

test("an interrupt stops the queue at once and leaves the running cell to the kernel's reply", () => {
  const nb = book();
  const a = run(nb, "a");
  run(nb, "b");
  nb.interrupt();
  assert.equal(nb.cells.get("a")?.state, "running");
  assert.equal(nb.cells.get("b")?.state, "interrupted");
  nb.receive(msg("error", a, { ename: "KeyboardInterrupt", evalue: "" }));
  finish(nb, a, "error");
  assert.equal(nb.cells.get("a")?.state, "error");
});

test("embedded resources are checked, kept with their digest, and script-capable content is never passed on", async () => {
  const nb = book();
  const a = run(nb, "a");
  const png = Buffer.from("\x89PNG\r\n\x1a\nnot really a picture", "binary");
  nb.receive(
    msg("display_data", a, {
      data: { "image/png": png.toString("base64"), "text/plain": "<Figure>", "text/html": "<script>alert(1)</script>" },
    }),
  );
  nb.receive(msg("display_data", a, { data: { "image/png": "!!!not base64!!!" } }));
  finish(nb, a);
  const [first, second] = nb.cells.get("a")?.outputs ?? [];
  assert.equal(first?.embedded.length, 1);
  assert.equal(await digestOf(first?.embedded[0] as never), createHash("sha256").update(png).digest("hex"));
  assert.equal(first?.mime["text/plain"], "<Figure>");
  assert.ok("text/html;blocked" in (first?.mime ?? {}));
  assert.ok(!("text/html" in (first?.mime ?? {})));
  assert.equal(second?.embedded.length, 0);
  assert.ok("image/png;invalid" in (second?.mime ?? {}));
});

test("editing a cell after it ran marks its outputs as describing older code", () => {
  const nb = book();
  const a = run(nb, "a");
  finish(nb, a);
  assert.equal(nb.outdated("a"), false);
  nb.edit("a", "print('changed')");
  assert.equal(nb.outdated("a"), true);
  assert.equal(nb.cells.get("a")?.outputs.length, 0);
});

test("a busy cell cannot be edited or run twice, markdown cannot run, and unknown cells are refused", () => {
  const nb = book();
  run(nb, "a");
  assert.throws(() => nb.edit("a", "x"), /cell.busy/);
  assert.throws(() => nb.execute("a"), /cell.busy/);
  assert.throws(() => nb.execute("md"), /cell.not_code/);
  assert.throws(() => nb.execute("zzz"), /cell.unknown/);
  assert.throws(() => nb.add("a", ""), /cell.exists/);
});

test("messages for requests the notebook never made are ignored and counted", () => {
  const nb = book();
  nb.receive(msg("stream", "somebody-elses", { text: "x" }));
  nb.receive(msg("stream", undefined, { text: "x" }));
  assert.deepEqual(
    nb.ignored.map((i) => i.reason),
    ["unknown_parent", "unknown_parent"],
  );
});

test("a very chatty cell is bounded", () => {
  const nb = book();
  const a = run(nb, "a");
  for (let i = 0; i < 5000; i++) nb.receive(msg("display_data", a, { data: { "text/plain": String(i) } }));
  assert.equal(nb.cells.get("a")?.outputs.length, 2000);
  assert.equal(nb.ignored.filter((i) => i.reason === "output_limit").length, 3000);
});
