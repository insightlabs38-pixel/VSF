#!/usr/bin/env node
// Native messaging test host: replies with the received byte count or a blob of the requested size.
import { readSync, writeSync } from "node:fs";

function readExact(n) {
  const buf = Buffer.alloc(n);
  let off = 0;
  while (off < n) {
    const r = readSync(0, buf, off, n - off, null);
    if (r === 0) return null;
    off += r;
  }
  return buf;
}

for (;;) {
  const head = readExact(4);
  if (!head) break;
  const len = head.readUInt32LE(0);
  const body = readExact(len);
  if (!body) break;
  const msg = JSON.parse(body.toString());
  const reply = msg.op === "blob" ? JSON.stringify({ blob: "x".repeat(msg.n) }) : JSON.stringify({ received: len });
  const out = Buffer.alloc(4 + Buffer.byteLength(reply));
  out.writeUInt32LE(Buffer.byteLength(reply), 0);
  out.write(reply, 4);
  let off = 0;
  while (off < out.length) off += writeSync(1, out, off);
}
