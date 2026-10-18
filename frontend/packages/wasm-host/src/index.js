export const EXPECTED_ABI_VERSION = 1;
export class WasmAbiError extends Error {
  code = "wasm.abi_mismatch";
  expected;
  actual;
  constructor(expected, actual) {
    super(`wasm.abi_mismatch expected=${expected} actual=${actual ?? "missing"}`);
    this.name = "WasmAbiError";
    this.expected = expected;
    this.actual = actual;
  }
}
export class WasmCallError extends Error {
  code;
  constructor(code) {
    super(code);
    this.name = "WasmCallError";
    this.code = code;
  }
}
const REQUIRED = [
  "memory",
  "vsf_abi_version",
  "vsf_alloc",
  "vsf_free",
  "vsf_compile",
  "vsf_output_ptr",
  "vsf_output_len",
  "vsf_error_ptr",
  "vsf_error_len",
];
export async function loadFabric(wasm, expectedAbi = EXPECTED_ABI_VERSION) {
  const { instance } = await WebAssembly.instantiate(wasm, {});
  const raw = instance.exports;
  if (typeof raw.vsf_abi_version !== "function") throw new WasmAbiError(expectedAbi, undefined);
  const actual = raw.vsf_abi_version();
  if (actual !== expectedAbi) throw new WasmAbiError(expectedAbi, actual);
  for (const name of REQUIRED) {
    if (!(name in raw)) throw new WasmAbiError(expectedAbi, undefined);
  }
  const ex = raw;
  const decoder = new TextDecoder();
  const read = (ptr, len) => new Uint8Array(ex.memory.buffer, ptr, len).slice();
  return {
    abiVersion: actual,
    compile(envelope) {
      const ptr = ex.vsf_alloc(envelope.length);
      if (ptr === 0) throw new WasmCallError("wasm.alloc_failed");
      try {
        new Uint8Array(ex.memory.buffer, ptr, envelope.length).set(envelope);
        const status = ex.vsf_compile(ptr, envelope.length);
        if (status !== 0) {
          const code = decoder.decode(read(ex.vsf_error_ptr(), ex.vsf_error_len()));
          throw new WasmCallError(code || `wasm.status_${status}`);
        }
        return read(ex.vsf_output_ptr(), ex.vsf_output_len());
      } finally {
        ex.vsf_free(ptr, envelope.length);
      }
    },
  };
}
