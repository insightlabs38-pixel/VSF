import { loadFabric } from "@vsf/wasm-host";

declare global {
  interface Window {
    __analyze?: (b64: string, iterations: number) => Promise<{ ms: number[]; memoryBytes: number; error?: string }>;
  }
}

const fabric = await loadFabric(await (await fetch("/artifacts/wasm/vsf_wasm.wasm")).arrayBuffer());

window.__analyze = async (b64, iterations) => {
  const bin = atob(b64);
  const pack = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) pack[i] = bin.charCodeAt(i);
  const ms: number[] = [];
  try {
    for (let i = 0; i < iterations; i++) {
      const t0 = performance.now();
      fabric.analyzePack(pack);
      ms.push(performance.now() - t0);
    }
  } catch (e) {
    return { ms, memoryBytes: 0, error: (e as { code?: string }).code ?? String(e) };
  }
  return {
    ms,
    memoryBytes: (performance as unknown as { memory?: { usedJSHeapSize: number } }).memory?.usedJSHeapSize ?? 0,
  };
};
(document.getElementById("status") as HTMLElement).textContent = "ready";
