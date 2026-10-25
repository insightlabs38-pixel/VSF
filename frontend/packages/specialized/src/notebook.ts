/** A kernel is identified by its id, the session it runs in, and an epoch that rises on every restart. */
export interface KernelRef {
  id: string;
  session: string;
  epoch: number;
}

export type CellState = "idle" | "queued" | "running" | "ok" | "error" | "interrupted" | "unknown";

/** A resource a kernel embedded in an output (an image, a PDF). The bytes are kept so a digest can be checked. */
export interface Embedded {
  mime: string;
  bytes: Uint8Array;
}

export type OutputKind = "stream" | "result" | "display" | "error";

export interface Output {
  /** Position in the order the kernel produced it for this execution. */
  seq: number;
  kind: OutputKind;
  mime: Record<string, string>;
  embedded: Embedded[];
  /** Which kernel epoch produced it; outputs of an earlier epoch are stale after a restart. */
  epoch: number;
  stale: boolean;
}

export interface Cell {
  id: string;
  kind: "code" | "markdown";
  source: string;
  /** The source as it was when it last ran; if different, the outputs describe older code. */
  ranSource: string | null;
  state: CellState;
  executionCount: number | null;
  /** Execution request currently or last bound to this cell. */
  msgId: string | null;
  outputs: Output[];
  error: { name: string; value: string } | null;
  ranIn: KernelRef | null;
}

export interface Header {
  msg_id: string;
  msg_type: string;
  session: string;
}

/** The subset of the Jupyter messaging protocol the model reads; anything else is ignored and counted. */
export interface KernelMessage {
  header: Header;
  parent_header: { msg_id?: string } | Record<string, never>;
  channel: "iopub" | "shell";
  content: Record<string, unknown>;
}

/** Content that can run script when rendered; it is recorded as blocked and never passed on. */
export const UNSAFE_MIME = new Set([
  "text/html",
  "application/javascript",
  "text/javascript",
  "image/svg+xml",
  "application/x-javascript",
]);
const MAX_OUTPUT_BYTES = 8 << 20;
const MAX_OUTPUTS = 2000;

export class NotebookError extends Error {
  readonly code: string;
  constructor(code: string) {
    super(code);
    this.code = code;
  }
}

export interface Ignored {
  reason: "wrong_session" | "unknown_parent" | "stale_epoch" | "unsupported_type" | "output_limit";
  type: string;
}

/** Lowercase hex sha256 of an embedded resource, so what is shown can be compared with what the kernel sent. */
export async function digestOf(e: Embedded): Promise<string> {
  const h = await globalThis.crypto.subtle.digest("SHA-256", e.bytes as unknown as ArrayBuffer);
  return [...new Uint8Array(h)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

function decodeBase64(s: string): Uint8Array | null {
  if (!/^[A-Za-z0-9+/\s]*={0,2}\s*$/.test(s)) return null;
  const clean = s.replace(/\s+/g, "");
  if (clean.length % 4 !== 0) return null;
  const bin = atob(clean);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/** A notebook whose cells keep the kernel identity that ran them; outputs belong to one request and go stale on restart. */
export class Notebook {
  readonly cells = new Map<string, Cell>();
  readonly order: string[] = [];
  readonly ignored: Ignored[] = [];
  private busy = false;
  private nextMsg = 1;
  private bound = new Map<string, string>();
  private finished = new Map<string, { reply: boolean; idle: boolean }>();
  private seq = new Map<string, number>();

  private kernel: KernelRef;

  constructor(kernel: KernelRef) {
    this.kernel = kernel;
  }

  get kernelRef(): KernelRef {
    return { ...this.kernel };
  }

  add(id: string, source: string, kind: "code" | "markdown" = "code"): Cell {
    if (this.cells.has(id)) throw new NotebookError("cell.exists");
    const cell: Cell = {
      id,
      kind,
      source,
      ranSource: null,
      state: "idle",
      executionCount: null,
      msgId: null,
      outputs: [],
      error: null,
      ranIn: null,
    };
    this.cells.set(id, cell);
    this.order.push(id);
    return cell;
  }

  edit(id: string, source: string): void {
    const c = this.cell(id);
    if (c.state === "running" || c.state === "queued") throw new NotebookError("cell.busy");
    c.source = source;
  }

  /** True when the outputs on screen were produced by code other than the cell's current source. */
  outdated(id: string): boolean {
    const c = this.cell(id);
    return c.ranSource !== null && c.ranSource !== c.source;
  }

  private cell(id: string): Cell {
    const c = this.cells.get(id);
    if (!c) throw new NotebookError("cell.unknown");
    return c;
  }

  /** Asks the kernel to run a cell. Returns the request the kernel must answer; nothing is claimed to have run yet. */
  execute(id: string): { msgId: string; request: { msg_type: "execute_request"; content: { code: string } } } {
    const c = this.cell(id);
    if (c.kind !== "code") throw new NotebookError("cell.not_code");
    if (c.state === "running" || c.state === "queued") throw new NotebookError("cell.busy");
    const msgId = `exec-${this.kernel.epoch}-${this.nextMsg++}`;
    c.msgId = msgId;
    c.state = this.busy ? "queued" : "running";
    this.busy = true;
    c.outputs = [];
    c.error = null;
    c.ranSource = c.source;
    c.ranIn = { ...this.kernel };
    this.bound.set(msgId, id);
    this.finished.set(msgId, { reply: false, idle: false });
    this.seq.set(msgId, 0);
    return { msgId, request: { msg_type: "execute_request", content: { code: c.source } } };
  }

  /** The kernel was restarted or replaced: everything that ran before is stale, and nothing is carried into the new epoch. */
  restart(session: string): void {
    this.kernel = { id: this.kernel.id, session, epoch: this.kernel.epoch + 1 };
    for (const c of this.cells.values()) {
      for (const o of c.outputs) o.stale = true;
      if (c.state === "running" || c.state === "queued") c.state = "interrupted";
    }
    this.bound.clear();
    this.finished.clear();
    this.busy = false;
  }

  /** The user interrupted: queued cells stop at once; the running cell's state waits for the kernel's reply. */
  interrupt(): void {
    for (const c of this.cells.values()) {
      if (c.state === "queued") {
        c.state = "interrupted";
        if (c.msgId) {
          this.bound.delete(c.msgId);
          this.finished.delete(c.msgId);
        }
      }
    }
  }

  /** The connection to the kernel was lost: whatever was running cannot be confirmed either way. */
  connectionLost(): void {
    for (const c of this.cells.values()) if (c.state === "running" || c.state === "queued") c.state = "unknown";
    this.bound.clear();
    this.finished.clear();
    this.busy = false;
  }

  /** Applies one kernel message. Messages that do not belong to this kernel, session or request change nothing. */
  receive(msg: KernelMessage, kernelEpoch = this.kernel.epoch): void {
    const type = msg.header.msg_type;
    if (kernelEpoch !== this.kernel.epoch) return void this.ignored.push({ reason: "stale_epoch", type });
    if (msg.header.session !== this.kernel.session) return void this.ignored.push({ reason: "wrong_session", type });
    const parent = (msg.parent_header as { msg_id?: string }).msg_id;
    if (type === "status") return void this.onStatus(parent, msg.content);
    if (!parent || !this.bound.has(parent)) return void this.ignored.push({ reason: "unknown_parent", type });
    const cell = this.cell(this.bound.get(parent) as string);
    switch (type) {
      case "execute_input":
        cell.executionCount = Number(msg.content.execution_count) || cell.executionCount;
        break;
      case "stream":
        this.append(cell, parent, "stream", { "text/plain": String(msg.content.text ?? "") }, []);
        break;
      case "execute_result":
      case "display_data":
        this.output(
          cell,
          parent,
          type === "execute_result" ? "result" : "display",
          (msg.content.data ?? {}) as Record<string, unknown>,
        );
        break;
      case "error":
        cell.error = { name: String(msg.content.ename ?? ""), value: String(msg.content.evalue ?? "") };
        this.append(cell, parent, "error", { "text/plain": `${cell.error.name}: ${cell.error.value}` }, []);
        break;
      case "execute_reply":
        this.onReply(cell, parent, msg.content);
        break;
      default:
        this.ignored.push({ reason: "unsupported_type", type });
    }
  }

  private onStatus(parent: string | undefined, content: Record<string, unknown>): void {
    if (content.execution_state !== "idle" || !parent) return;
    const f = this.finished.get(parent);
    if (!f) return;
    f.idle = true;
    this.settle(parent);
  }

  private onReply(cell: Cell, parent: string, content: Record<string, unknown>): void {
    const f = this.finished.get(parent);
    if (!f) return;
    f.reply = true;
    const status = content.status;
    if (typeof content.execution_count === "number") cell.executionCount = content.execution_count;
    if (status === "error") {
      cell.state = "error";
      // A failed cell aborts what was queued behind it, as the kernel does.
      for (const c of this.cells.values()) if (c.state === "queued") c.state = "interrupted";
    } else if (status === "aborted") cell.state = "interrupted";
    else if (status === "ok") cell.state = "ok";
    else cell.state = "unknown";
    this.settle(parent);
  }

  private settle(parent: string): void {
    const f = this.finished.get(parent);
    if (!f?.reply || !f.idle) return;
    this.finished.delete(parent);
    this.bound.delete(parent);
    this.busy = false;
    // The next queued cell starts when the kernel is idle again.
    for (const id of this.order) {
      const c = this.cells.get(id) as Cell;
      if (c.state === "queued") {
        c.state = "running";
        this.busy = true;
        break;
      }
    }
  }

  private output(cell: Cell, parent: string, kind: OutputKind, data: Record<string, unknown>): void {
    const mime: Record<string, string> = {};
    const embedded: Embedded[] = [];
    for (const [type, value] of Object.entries(data)) {
      const text = typeof value === "string" ? value : JSON.stringify(value);
      if (UNSAFE_MIME.has(type)) {
        // Script-capable content is never rendered; its presence is recorded and a plain form is used if the kernel sent one.
        mime[`${type};blocked`] = "";
        continue;
      }
      if (type.startsWith("image/") || type === "application/pdf") {
        const bytes = decodeBase64(text);
        if (!bytes || bytes.length > MAX_OUTPUT_BYTES) {
          mime[`${type};invalid`] = "";
          continue;
        }
        embedded.push({ mime: type, bytes });
        continue;
      }
      mime[type] = text;
    }
    this.append(cell, parent, kind, mime, embedded);
  }

  private append(
    cell: Cell,
    parent: string,
    kind: OutputKind,
    mime: Record<string, string>,
    embedded: Embedded[],
  ): void {
    if (cell.outputs.length >= MAX_OUTPUTS) return void this.ignored.push({ reason: "output_limit", type: kind });
    const seq = (this.seq.get(parent) ?? 0) + 1;
    this.seq.set(parent, seq);
    const last = cell.outputs[cell.outputs.length - 1];
    // Consecutive stream text from the same request is one stream, not many.
    if (kind === "stream" && last?.kind === "stream" && !last.stale) {
      last.mime["text/plain"] = (last.mime["text/plain"] ?? "") + (mime["text/plain"] ?? "");
      last.seq = seq;
      return;
    }
    cell.outputs.push({ seq, kind, mime, embedded, epoch: this.kernel.epoch, stale: false });
  }
}
