import type { IncomingMessage, ServerResponse } from "node:http";
import { expect, type Page, test } from "@playwright/test";
import type { ReconnectPolicy, TransportDeclaration, TransportManifest } from "@vsf/specialized";
import { ready } from "../support/page.ts";
import { startWorld, type World } from "../support/world.ts";
import type { Socket } from "../support/ws.ts";

let w: World;
test.beforeAll(async () => {
  w = await startWorld();
});
test.afterAll(async () => w.stop());

const TEXTS = ["a", "b", "c", "d", "e"];
const policy = (over: Partial<ReconnectPolicy> = {}): ReconnectPolicy => ({
  supported: true,
  resume: "offset",
  maxAttempts: 3,
  backoffMs: [20, 200],
  ...over,
});

const frame = (s: Socket, seq: number) => s.send(JSON.stringify({ seq, text: TEXTS[seq - 1] ?? `t${seq}` }));

async function connect(page: Page, p: ReconnectPolicy): Promise<void> {
  const url = `ws://${new URL(w.url).host}/ws`;
  await page.evaluate(
    ([u, pol]) => {
      (window as unknown as { c: ReturnType<typeof window.vsf.socketClient> }).c = window.vsf.socketClient(
        u as string,
        pol as ReconnectPolicy,
      );
    },
    [url, p] as const,
  );
}

const client = (page: Page) =>
  page.evaluate(() => {
    const c = (window as unknown as { c: ReturnType<typeof window.vsf.socketClient> }).c;
    return {
      texts: c.texts,
      gaps: c.gaps,
      attempts: c.attempts,
      state: c.state(),
      inputs: [1, 2, 3, 4].map((n) => c.queue.stateOf(n)),
    };
  });

test.beforeEach(async ({ page }) => {
  await ready(page, w);
});

test("after a drop the client resumes after the last sequence it saw: nothing is repeated and nothing is missing", async ({
  page,
}) => {
  let conns = 0;
  const resumes: number[] = [];
  w.onSocket = (s) => {
    const mine = ++conns;
    s.onText = (t) => {
      const m = JSON.parse(t) as { resume?: number };
      if (m.resume === undefined) return;
      resumes.push(m.resume);
      for (let i = m.resume + 1; i <= (mine === 1 ? 3 : 5); i++) frame(s, i);
      if (mine === 1) setTimeout(() => s.drop(), 20);
    };
  };
  await connect(page, policy());
  await page.waitForFunction(() => (window as unknown as { c: { texts: string[] } }).c.texts.length === 5);
  const c = await client(page);
  expect(c.texts).toEqual(TEXTS);
  expect(c.gaps).toEqual([]);
  expect(resumes).toEqual([0, 3]);
  expect(c.attempts.length).toBe(1);
});

test("when the source cannot resume, the loss is reported as a gap and never filled in", async ({ page }) => {
  let conns = 0;
  w.onSocket = (s) => {
    const mine = ++conns;
    if (mine === 1) {
      for (let i = 1; i <= 3; i++) frame(s, i);
      setTimeout(() => s.drop(), 20);
    } else {
      frame(s, 6);
      frame(s, 7);
    }
  };
  await connect(page, policy({ resume: "none" }));
  await page.waitForFunction(() => (window as unknown as { c: { texts: string[] } }).c.texts.length === 5);
  const c = await client(page);
  expect(c.texts).toEqual(["a", "b", "c", "t6", "t7"]);
  expect(c.gaps).toEqual([6]);
});

test("an input that was sent but not acknowledged before a drop is unknown and is not sent again unless declared repeatable", async ({
  page,
}) => {
  let conns = 0;
  const received: number[] = [];
  w.onSocket = (s) => {
    const mine = ++conns;
    s.onText = (t) => {
      const m = JSON.parse(t) as { resume?: number; input?: number };
      if (m.input === undefined) return;
      received.push(m.input);
      if (mine === 1 && m.input === 1) s.send(JSON.stringify({ ack: 1 }));
      if (mine === 1 && m.input === 3) s.drop();
      if (mine === 2) s.send(JSON.stringify({ ack: m.input }));
    };
  };
  await connect(page, policy());
  await page.waitForFunction(() => (window as unknown as { c: { state(): string } }).c.state() === "open");
  await page.evaluate(async () => {
    const c = (window as unknown as { c: ReturnType<typeof window.vsf.socketClient> }).c;
    c.send("one");
    await new Promise((r) => setTimeout(r, 100));
    c.send("two");
    c.send("three", true);
  });
  await page.waitForFunction(
    () => (window as unknown as { c: { queue: { stateOf(n: number): string } } }).c.queue.stateOf(3) === "acked",
  );
  const c = await client(page);
  expect(received).toEqual([1, 2, 3, 3]);
  expect(c.inputs).toEqual(["acked", "unknown", "acked", undefined]);
});

test("reconnect attempts are finite, back off, and then the connection is reported as failed", async ({ page }) => {
  w.onSocket = (s) => s.drop();
  await connect(page, policy({ maxAttempts: 3, backoffMs: [20, 200] }));
  await page.waitForFunction(() => (window as unknown as { c: { state(): string } }).c.state() === "failed");
  const c = await client(page);
  expect(c.attempts).toEqual([15, 30, 60]);
});

test("input queued while the connection is down is discarded rather than replayed later", async ({ page }) => {
  let conns = 0;
  const received: number[] = [];
  w.onSocket = (s) => {
    const mine = ++conns;
    s.onText = (t) => {
      const m = JSON.parse(t) as { input?: number };
      if (m.input !== undefined) received.push(m.input);
    };
    if (mine === 1) setTimeout(() => s.drop(), 50);
  };
  await connect(page, policy());
  await page.waitForFunction(() => (window as unknown as { c: { state(): string } }).c.state() === "open");
  await page.evaluate(async () => {
    const c = (window as unknown as { c: ReturnType<typeof window.vsf.socketClient> }).c;
    c.send("sent while open");
    await new Promise((r) => setTimeout(r, 200));
  });
  const c = await client(page);
  expect(received).toEqual([1]);
  expect(c.inputs[0]).toBe("unknown");
});

test("server-sent events resume from the last event id the browser saw, with no repeats", async ({ page }) => {
  const seen: (string | undefined)[] = [];
  w.onEvents = (req: IncomingMessage, res: ServerResponse) => {
    const last = req.headers["last-event-id"];
    seen.push(Array.isArray(last) ? last[0] : last);
    res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-store" });
    const from = last ? Number(last) : 0;
    res.write("retry: 50\n\n");
    const to = from === 0 ? 3 : 5;
    for (let i = from + 1; i <= to; i++) res.write(`id: ${i}\ndata: ${TEXTS[i - 1]}\n\n`);
    if (from === 0) setTimeout(() => res.destroy(), 20);
  };
  const got = await page.evaluate(
    () =>
      new Promise<string[]>((resolve) => {
        const out: string[] = [];
        const es = new EventSource("/events");
        es.onmessage = (e) => {
          out.push(e.data);
          if (out.length === 5) {
            es.close();
            resolve(out);
          }
        };
      }),
  );
  expect(got).toEqual(TEXTS);
  expect(seen).toEqual([undefined, "3"]);
});

const decl = (kind: TransportDeclaration["kind"], over: Partial<TransportDeclaration> = {}): TransportDeclaration => ({
  kind,
  available: true,
  needs: [],
  backpressure: { mode: "credit", maxBufferedBytes: 1 << 20 },
  reconnect: policy(),
  ordered: true,
  bidirectional: true,
  ...over,
});

const manifest = (transports: TransportDeclaration[], prefer: TransportDeclaration["kind"][]): TransportManifest => ({
  schema: "vsf-transport-capabilities-v1",
  source: "fixture",
  transports,
  prefer,
  whenNoneUsable: "retain_source_ui",
});

test("this browser has every transport, and capability negotiation honours what the source declares", async ({
  page,
}) => {
  const result = await page.evaluate(
    ([m]) => {
      const env = window.vsf.detectEnvironment(window as never, {}, [], true);
      return {
        has: env.has,
        secure: env.secureContext,
        all: window.vsf.negotiate(m as never, env),
        blocked: window.vsf.negotiate(m as never, window.vsf.detectEnvironment(window as never, { webrtc: false })),
      };
    },
    [
      manifest(
        [decl("webrtc", { needs: ["network_peer"] }), decl("websocket"), decl("sse", { bidirectional: false })],
        ["webrtc", "websocket", "sse"],
      ),
    ] as const,
  );
  expect(result.has).toEqual({ websocket: true, sse: true, webrtc: true, http_poll: true });
  expect(result.secure).toBe(true);
  expect(result.all.chosen).toBe("webrtc");
  expect(result.all.mode).toBe("native");
  expect(result.blocked.chosen).toBe("websocket");
  expect(result.blocked.mode).toBe("fallback");
  expect(result.blocked.tried[0]).toEqual({ kind: "webrtc", outcome: "blocked_by_policy" });
});

test("a manifest that hides why a transport is missing is refused and the source keeps its own interface", async ({
  page,
}) => {
  const r = await page.evaluate(
    ([m]) => ({
      problems: window.vsf.validateManifest(m).length,
      n: window.vsf.negotiate(m as never, window.vsf.detectEnvironment(window as never)),
    }),
    [manifest([decl("websocket"), decl("webrtc", { available: false })], ["websocket", "webrtc"])] as const,
  );
  expect(r.problems).toBeGreaterThan(0);
  expect(r.n).toEqual({ chosen: null, tried: [], mode: "source_ui" });
});

test("a source that offers no transport this page can use keeps its own interface", async ({ page }) => {
  const r = await page.evaluate(
    ([m]) => window.vsf.negotiate(m as never, window.vsf.detectEnvironment(window as never, { websocket: false })),
    [
      manifest(
        [decl("websocket"), decl("webrtc", { available: false, reason: "the source has no peer mode" })],
        ["websocket", "webrtc"],
      ),
    ] as const,
  );
  expect(r.chosen).toBeNull();
  expect(r.mode).toBe("source_ui");
  expect(r.tried).toEqual([
    { kind: "websocket", outcome: "blocked_by_policy" },
    { kind: "webrtc", outcome: "declared_unavailable" },
  ]);
});

test("a data channel really carries a message between two peers in this browser", async ({ page }) => {
  const echoed = await page.evaluate(async () => {
    const a = new RTCPeerConnection();
    const b = new RTCPeerConnection();
    a.onicecandidate = (e) => e.candidate && void b.addIceCandidate(e.candidate);
    b.onicecandidate = (e) => e.candidate && void a.addIceCandidate(e.candidate);
    const channel = a.createDataChannel("t");
    const got = new Promise<string>((resolve) => {
      b.ondatachannel = (e) => {
        e.channel.onmessage = (m) => resolve(String(m.data));
      };
    });
    const open = new Promise<void>((resolve) => {
      channel.onopen = () => resolve();
    });
    await a.setLocalDescription(await a.createOffer());
    await b.setRemoteDescription(a.localDescription as RTCSessionDescriptionInit);
    await b.setLocalDescription(await b.createAnswer());
    await a.setRemoteDescription(b.localDescription as RTCSessionDescriptionInit);
    await open;
    channel.send("ping");
    const text = await got;
    a.close();
    b.close();
    return text;
  });
  expect(echoed).toBe("ping");
});
