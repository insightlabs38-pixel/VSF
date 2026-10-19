import { encodePack, snapshotDocument } from "@vsf/observer";

function toBase64(bytes: Uint8Array): string {
  let out = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) out += String.fromCharCode(...bytes.subarray(i, i + chunk));
  return btoa(out);
}

/** Injected on demand; the build footer calls __vsfRun() so the script's completion value is the result. */
function run(): { origin: string; nodes: number; truncated: boolean; captureMs: number; packMs: number; b64: string } {
  const t0 = performance.now();
  const { snapshot, truncated } = snapshotDocument(document);
  const t1 = performance.now();
  const bytes = encodePack(snapshot);
  const b64 = toBase64(bytes);
  const t2 = performance.now();
  return { origin: location.origin, nodes: snapshot.nodes.length, truncated, captureMs: t1 - t0, packMs: t2 - t1, b64 };
}

(globalThis as { __vsfRun?: typeof run }).__vsfRun = run;

export interface Stamp {
  tab: number;
  epoch: string;
  seq: number;
  id: string;
}

/** The observation stage: the pack plus who captured it, stamped with values the service worker chose. */
async function observationEnvelope(stamp: Stamp): Promise<unknown> {
  const { snapshot, truncated } = snapshotDocument(document);
  const bytes = encodePack(snapshot);
  const digest = [...new Uint8Array(await crypto.subtle.digest("SHA-256", bytes as BufferSource))]
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
  return {
    v: 1,
    kind: "observation",
    id: stamp.id,
    parent: null,
    seq: stamp.seq,
    source: { tab: stamp.tab, frame: window === window.top ? 0 : 1, origin: location.origin, epoch: stamp.epoch },
    body: { pack: toBase64(bytes), digest, bytes: bytes.length, nodes: snapshot.nodes.length, truncated },
  };
}

(globalThis as { __vsfEnvelope?: typeof observationEnvelope }).__vsfEnvelope = observationEnvelope;
