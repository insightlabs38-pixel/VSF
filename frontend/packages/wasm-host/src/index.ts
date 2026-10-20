export const EXPECTED_ABI_VERSION = 4;

export class WasmAbiError extends Error {
  readonly code = "wasm.abi_mismatch";
  readonly expected: number;
  readonly actual: number | undefined;
  constructor(expected: number, actual: number | undefined) {
    super(`wasm.abi_mismatch expected=${expected} actual=${actual ?? "missing"}`);
    this.name = "WasmAbiError";
    this.expected = expected;
    this.actual = actual;
  }
}

export class WasmCallError extends Error {
  readonly code: string;
  constructor(code: string) {
    super(code);
    this.name = "WasmCallError";
    this.code = code;
  }
}

interface Exports {
  memory: WebAssembly.Memory;
  vsf_abi_version: () => number;
  vsf_alloc: (len: number) => number;
  vsf_free: (ptr: number, len: number) => void;
  vsf_compile: (ptr: number, len: number) => number;
  vsf_pack_analyze: (ptr: number, len: number) => number;
  vsf_ir_validate: (ptr: number, len: number) => number;
  vsf_assemble: (ptr: number, len: number) => number;
  vsf_output_ptr: () => number;
  vsf_output_len: () => number;
  vsf_error_ptr: () => number;
  vsf_error_len: () => number;
}

const REQUIRED = [
  "memory",
  "vsf_abi_version",
  "vsf_alloc",
  "vsf_free",
  "vsf_compile",
  "vsf_pack_analyze",
  "vsf_ir_validate",
  "vsf_assemble",
  "vsf_output_ptr",
  "vsf_output_len",
  "vsf_error_ptr",
  "vsf_error_len",
] as const;

export interface Fabric {
  readonly abiVersion: number;
  compile(envelope: Uint8Array): Uint8Array;
  analyzePack(pack: Uint8Array): Uint8Array;
  /** Canonical JSON report: decode result, digest and semantic violations. */
  validateIr(document: Uint8Array): Uint8Array;
  /** Frame of header length, header JSON and recognizer snapshot; returns the canonical `vsf-assembly-v1` document. */
  assemble(frame: Uint8Array): Uint8Array;
}

export async function loadFabric(wasm: BufferSource, expectedAbi: number = EXPECTED_ABI_VERSION): Promise<Fabric> {
  const { instance } = await WebAssembly.instantiate(wasm, {});
  const raw = instance.exports as Record<string, unknown>;
  if (typeof raw.vsf_abi_version !== "function") throw new WasmAbiError(expectedAbi, undefined);
  const actual = (raw.vsf_abi_version as () => number)();
  if (actual !== expectedAbi) throw new WasmAbiError(expectedAbi, actual);
  for (const name of REQUIRED) {
    if (!(name in raw)) throw new WasmAbiError(expectedAbi, undefined);
  }
  const ex = raw as unknown as Exports;
  const decoder = new TextDecoder();
  const read = (ptr: number, len: number): Uint8Array => new Uint8Array(ex.memory.buffer, ptr, len).slice();
  const call = (fn: (ptr: number, len: number) => number, input: Uint8Array): Uint8Array => {
    const ptr = ex.vsf_alloc(input.length);
    if (ptr === 0) throw new WasmCallError("wasm.alloc_failed");
    try {
      new Uint8Array(ex.memory.buffer, ptr, input.length).set(input);
      const status = fn(ptr, input.length);
      if (status !== 0) {
        const code = decoder.decode(read(ex.vsf_error_ptr(), ex.vsf_error_len()));
        throw new WasmCallError(code || `wasm.status_${status}`);
      }
      return read(ex.vsf_output_ptr(), ex.vsf_output_len());
    } finally {
      ex.vsf_free(ptr, input.length);
    }
  };
  return {
    abiVersion: actual,
    compile: (envelope) => call(ex.vsf_compile, envelope),
    analyzePack: (pack) => call(ex.vsf_pack_analyze, pack),
    validateIr: (document) => call(ex.vsf_ir_validate, document),
    assemble: (frame) => call(ex.vsf_assemble, frame),
  };
}

export * from "./assembly.ts";
export { type ReportedViolation, requireValidated, Validated, ValidationRefused, validate } from "./validated.ts";
