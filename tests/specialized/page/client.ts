import { OutboundQueue, planReconnect, type ReconnectPolicy } from "@vsf/specialized";

export interface SocketClient {
  texts: string[];
  gaps: number[];
  attempts: number[];
  queue: OutboundQueue;
  state(): "connecting" | "open" | "waiting" | "failed" | "closed";
  send(data: string, repeatable?: boolean): number;
  close(): void;
}

/** A reference client for the reconnect rules: resume after the last sequence, never resend an unknown write. */
export function socketClient(url: string, policy: ReconnectPolicy): SocketClient {
  const queue = new OutboundQueue();
  const texts: string[] = [];
  const gaps: number[] = [];
  const attempts: number[] = [];
  const payload = new Map<number, string>();
  let ws: WebSocket | null = null;
  let last = 0;
  let attempt = 0;
  let state: ReturnType<SocketClient["state"]> = "connecting";
  let stopped = false;

  const flush = (): void => {
    if (ws?.readyState !== WebSocket.OPEN) return;
    for (const m of queue.resendable()) ws.send(JSON.stringify({ input: m.seq, data: m.data }));
    for (const [seq, data] of payload) {
      if (queue.stateOf(seq) !== "queued") continue;
      ws.send(JSON.stringify({ input: seq, data }));
      queue.markSent(seq);
    }
  };

  const connect = (): void => {
    state = "connecting";
    ws = new WebSocket(url);
    ws.onopen = () => {
      state = "open";
      if (policy.resume === "offset") ws?.send(JSON.stringify({ resume: last }));
      flush();
    };
    ws.onmessage = (e) => {
      const m = JSON.parse(String(e.data)) as { seq?: number; text?: string; ack?: number };
      // A connection counts as stable, and the retry budget is restored, only once the source has sent something.
      attempt = 0;
      if (m.ack !== undefined) return queue.ack(m.ack);
      if (m.seq === undefined) return;
      if (m.seq !== last + 1) gaps.push(m.seq);
      last = m.seq;
      texts.push(m.text ?? "");
    };
    ws.onclose = () => {
      queue.disconnected();
      if (stopped) {
        state = "closed";
        return;
      }
      const plan = planReconnect(policy, last, attempt, () => 0.5);
      if (!plan) {
        state = "failed";
        return;
      }
      attempt = plan.attempt;
      attempts.push(plan.delayMs);
      state = "waiting";
      setTimeout(connect, plan.delayMs);
    };
  };
  connect();

  return {
    texts,
    gaps,
    attempts,
    queue,
    state: () => state,
    send(data, repeatable = false) {
      const m = queue.enqueue(data, repeatable);
      payload.set(m.seq, data);
      flush();
      return m.seq;
    },
    close() {
      stopped = true;
      ws?.close();
    },
  };
}
