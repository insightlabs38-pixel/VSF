import assert from "node:assert/strict";
import { test } from "node:test";
import {
  detectEnvironment,
  type Environment,
  negotiate,
  OutboundQueue,
  Pipe,
  planReconnect,
  type TransportDeclaration,
  type TransportManifest,
  validateManifest,
} from "../src/transport.ts";

const decl = (kind: TransportDeclaration["kind"], over: Partial<TransportDeclaration> = {}): TransportDeclaration => ({
  kind,
  available: true,
  needs: [],
  backpressure: { mode: "credit", maxBufferedBytes: 1 << 20 },
  reconnect: { supported: true, resume: "offset", maxAttempts: 5, backoffMs: [100, 5000] },
  ordered: true,
  bidirectional: kind !== "sse",
  ...over,
});

const manifest = (over: Partial<TransportManifest> = {}): TransportManifest => ({
  schema: "vsf-transport-capabilities-v1",
  source: "term",
  transports: [
    decl("webrtc", { needs: ["secure_context", "network_peer"] }),
    decl("websocket"),
    decl("sse"),
    decl("http_poll", { reconnect: { supported: true, resume: "none", maxAttempts: 3, backoffMs: [500, 8000] } }),
  ],
  prefer: ["webrtc", "websocket", "sse", "http_poll"],
  whenNoneUsable: "retain_source_ui",
  ...over,
});

const env = (over: Partial<Environment> = {}): Environment => ({
  has: { websocket: true, sse: true, webrtc: true, http_poll: true },
  secureContext: true,
  allowed: { websocket: true, sse: true, webrtc: true, http_poll: true },
  permissions: [],
  networkPeer: true,
  ...over,
});

test("a valid manifest has no problems and the first preference wins when the environment allows it", () => {
  assert.deepEqual(validateManifest(manifest()), []);
  const n = negotiate(manifest(), env());
  assert.equal(n.chosen, "webrtc");
  assert.equal(n.mode, "native");
});

test("each reason a transport is skipped is recorded and the next preference is used", () => {
  const n = negotiate(manifest(), env({ secureContext: false }));
  assert.equal(n.chosen, "websocket");
  assert.equal(n.mode, "fallback");
  assert.deepEqual(
    n.tried.map((t) => t.outcome),
    ["needs_secure_context", "chosen", "not_preferred", "not_preferred"],
  );
  const blocked = negotiate(
    manifest(),
    env({ allowed: { websocket: false, sse: true, webrtc: false, http_poll: true } }),
  );
  assert.equal(blocked.chosen, "sse");
  assert.deepEqual(
    blocked.tried.slice(0, 2).map((t) => t.outcome),
    ["blocked_by_policy", "blocked_by_policy"],
  );
  const noPeer = negotiate(manifest(), env({ networkPeer: false }));
  assert.equal(noPeer.tried[0]?.outcome, "needs_network_peer");
  const missing = negotiate(manifest(), env({ has: { websocket: false, sse: false, webrtc: false, http_poll: true } }));
  assert.equal(missing.chosen, "http_poll");
});

test("a source that declares a transport unavailable is never offered it, whatever the browser has", () => {
  const m = manifest({
    transports: [decl("websocket", { available: false, reason: "the server refuses upgrades" }), decl("sse")],
    prefer: ["websocket", "sse"],
  });
  const n = negotiate(m, env());
  assert.equal(n.chosen, "sse");
  assert.equal(n.tried[0]?.outcome, "declared_unavailable");
});

test("when nothing is usable the source interface is kept and nothing else is picked", () => {
  const m = manifest({ transports: [decl("websocket")], prefer: ["websocket"] });
  const n = negotiate(m, env({ has: { websocket: false, sse: true, webrtc: true, http_poll: true } }));
  assert.equal(n.chosen, null);
  assert.equal(n.mode, "source_ui");
});

test("a permission-gated transport needs the grant by name", () => {
  const m = manifest({
    transports: [decl("webrtc", { needs: ["user_permission"] }), decl("websocket")],
    prefer: ["webrtc", "websocket"],
  });
  assert.equal(negotiate(m, env()).tried[0]?.outcome, "needs_permission");
  assert.equal(negotiate(m, env({ permissions: ["webrtc"] })).chosen, "webrtc");
});

test("manifests that would buffer without bound, hide why a transport is missing or break the one-way rule are refused", () => {
  const bad = (m: unknown): string[] => validateManifest(m).map((p) => `${p.path}:${p.message}`);
  assert.ok(
    bad(
      manifest({
        transports: [decl("websocket", { backpressure: { mode: "none", maxBufferedBytes: null } })],
        prefer: ["websocket"],
      }),
    ).some((x) => x.includes("unbounded")),
  );
  assert.ok(
    bad(
      manifest({
        transports: [decl("websocket", { backpressure: { mode: "block", maxBufferedBytes: 1 << 30 } })],
        prefer: ["websocket"],
      }),
    ).some((x) => x.includes("maxBufferedBytes")),
  );
  assert.ok(
    bad(manifest({ transports: [decl("websocket", { available: false })], prefer: ["websocket"] })).some((x) =>
      x.includes("says why"),
    ),
  );
  assert.ok(
    bad(manifest({ transports: [decl("sse", { bidirectional: true })], prefer: ["sse"] })).some((x) =>
      x.includes("one-way"),
    ),
  );
  assert.ok(bad(manifest({ whenNoneUsable: "poll_forever" as never })).some((x) => x.includes("source interface")));
  assert.ok(
    bad(manifest({ prefer: ["websocket", "carrier_pigeon" as never] })).some((x) => x.includes("not declared")),
  );
  assert.ok(
    bad(
      manifest({
        transports: [
          decl("websocket", { reconnect: { supported: false, resume: "offset", maxAttempts: 0, backoffMs: [1, 1] } }),
        ],
        prefer: ["websocket"],
      }),
    ).some((x) => x.includes("resume needs reconnect")),
  );
  assert.ok(bad({ schema: "other" }).length > 0);
  assert.equal(negotiate(manifest({ whenNoneUsable: "x" as never }), env()).mode, "source_ui");
});

test("detection reads what a runtime has and what its policy allows", () => {
  const e = detectEnvironment(
    { WebSocket: function WebSocket() {}, fetch: () => {}, isSecureContext: true },
    { websocket: false },
  );
  assert.deepEqual(e.has, { websocket: true, sse: false, webrtc: false, http_poll: true });
  assert.equal(e.allowed.websocket, false);
  assert.equal(e.secureContext, true);
});

test("a credit pipe refuses what it cannot hold and a drop-oldest pipe counts what it loses", () => {
  const credit = new Pipe<{ bytes: number; n: number }>({ mode: "credit", maxBufferedBytes: 10 });
  assert.equal(credit.push({ bytes: 6, n: 1 }), "accepted");
  assert.equal(credit.push({ bytes: 6, n: 2 }), "refused");
  assert.equal(credit.push({ bytes: 11, n: 3 }), "refused");
  assert.equal(credit.window, 4);
  assert.equal(credit.take()?.n, 1);
  assert.equal(credit.buffered, 0);
  const lossy = new Pipe<{ bytes: number; n: number }>({ mode: "drop_oldest", maxBufferedBytes: 10 });
  for (let n = 1; n <= 5; n++) lossy.push({ bytes: 4, n });
  assert.equal(lossy.dropped, 3);
  assert.deepEqual([lossy.take()?.n, lossy.take()?.n], [4, 5]);
  assert.throws(() => new Pipe({ mode: "none", maxBufferedBytes: null }), /unbounded/);
});

test("sustained pressure keeps memory inside the declared bound", () => {
  const p = new Pipe<{ bytes: number }>({ mode: "drop_oldest", maxBufferedBytes: 4096 });
  for (let i = 0; i < 100000; i++) {
    p.push({ bytes: 100 });
    assert.ok(p.buffered <= 4096);
  }
  assert.ok(p.dropped > 99000);
});

test("messages in flight when the connection drops are unknown and are not resent unless declared repeatable", () => {
  const q = new OutboundQueue();
  const typed = q.enqueue("rm -rf build\n");
  const ping = q.enqueue("ping", true);
  const queued = q.enqueue("ls\n");
  q.markSent(typed.seq);
  q.markSent(ping.seq);
  q.disconnected();
  assert.equal(q.stateOf(typed.seq), "unknown");
  assert.equal(q.stateOf(queued.seq), "discarded");
  assert.deepEqual(
    q.resendable().map((m) => m.seq),
    [ping.seq],
  );
  assert.deepEqual(
    q.unknown().map((m) => m.seq),
    [typed.seq],
  );
  q.ack(ping.seq);
  assert.deepEqual(q.resendable(), []);
});

test("reconnect attempts are finite, back off, and carry the resume point only when the source supports it", () => {
  const policy = {
    supported: true,
    resume: "offset" as const,
    maxAttempts: 4,
    backoffMs: [100, 800] as [number, number],
  };
  const delays: number[] = [];
  for (let a = 0; ; a++) {
    const r = planReconnect(policy, 42, a, () => 1);
    if (!r) break;
    assert.equal(r.after, 42);
    assert.equal(r.mode, "offset");
    delays.push(r.delayMs);
  }
  assert.deepEqual(delays, [100, 200, 400, 800]);
  assert.equal(planReconnect({ ...policy, supported: false }, 0, 0), null);
  assert.equal(planReconnect({ ...policy, resume: "none" }, 7, 0, () => 1)?.mode, "none");
});
