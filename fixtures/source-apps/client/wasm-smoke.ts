import { generateEnvelopeBytes, sha256Hex } from "@vsf/schema";
import { loadFabric } from "@vsf/wasm-host";

const status = document.getElementById("status") as HTMLElement;
const digest = document.getElementById("digest") as HTMLElement;

async function main(): Promise<void> {
  const variant =
    new URLSearchParams(location.search).get("wasm") === "injected" ? "vsf_wasm.abi-injected.wasm" : "vsf_wasm.wasm";
  const response = await fetch(`/artifacts/wasm/${variant}`);
  if (!response.ok) throw Object.assign(new Error("fetch"), { code: "wasm.fetch_failed" });
  const fabric = await loadFabric(await response.arrayBuffer());
  const out = fabric.compile(generateEnvelopeBytes(42));
  digest.textContent = await sha256Hex(out);
  status.textContent = "ok";
}

main().catch((err: unknown) => {
  const code = (err as { code?: string }).code ?? "wasm.unknown";
  status.textContent = `error:${code}`;
});
