import {
  captureCapabilities,
  encodePack,
  missingSignals,
  persistentEpochs,
  Replica,
  type RuntimeCaps,
  Session,
} from "@vsf/observer";

function toBase64(bytes: Uint8Array): string {
  let out = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) out += String.fromCharCode(...bytes.subarray(i, i + chunk));
  return btoa(out);
}

interface DomApi {
  openOrClosedShadowRoot?: (el: HTMLElement) => ShadowRoot | null;
}

/** One live-session baseline: only this runtime can see closed shadow roots, and it says so. */
function run(allowedOrigin: string, withSnapshot = false) {
  const dom = (globalThis as unknown as { chrome?: { dom?: DomApi } }).chrome?.dom;
  const caps: RuntimeCaps = {};
  if (dom?.openOrClosedShadowRoot) {
    const open = dom.openOrClosedShadowRoot.bind(dom);
    caps.openOrClosedShadowRoot = (el) => open(el as HTMLElement);
  }
  const replica = new Replica();
  const t0 = performance.now();
  const session = Session.start({
    doc: document,
    policy: { allowOrigin: (o) => o === allowedOrigin },
    nextEpoch: persistentEpochs(window),
    caps,
    batchMs: null,
    sink: (b) => void replica.apply(b),
  });
  const t1 = performance.now();
  const shadow = { open: 0, closed: 0 };
  for (const n of replica.nodes.values()) {
    if (n.scope?.kind === "shadow" && !n.scope.opaque) shadow[n.scope.mode ?? "open"]++;
  }
  const snapshot = replica.toSnapshot();
  const bytes = encodePack(snapshot);
  const result = {
    origin: location.origin,
    epoch: session.epoch,
    nodes: snapshot.nodes.length,
    state: replica.state,
    captureMs: t1 - t0,
    capabilities: captureCapabilities(caps, window),
    shadow,
    missing: missingSignals(replica, session.stats.scopes, caps),
    env: replica.env,
    b64: toBase64(bytes),
    ...(withSnapshot ? { snapshot: JSON.stringify(replica.toRecognizerInput()) } : {}),
  };
  session.stop();
  return result;
}

(globalThis as { __vsfObserve?: typeof run }).__vsfObserve = run;
