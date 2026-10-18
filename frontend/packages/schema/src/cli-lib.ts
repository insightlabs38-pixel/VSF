import { errorCode, sha256Hex, verifyEnvelopeBytes } from "./index.ts";

export interface CliIo {
  readFile(path: string): Uint8Array;
  readStdin(): Uint8Array;
  stdout(bytes: Uint8Array | string): void;
  stderr(text: string): void;
}

const USAGE = "usage: vsf-schema <verify [FILE] | digest [FILE]>";

export async function canonicalizeFromStdinOrFile(args: string[], io: CliIo): Promise<number> {
  const [cmd, file] = args;
  if (cmd !== "verify" && cmd !== "digest") {
    io.stderr(`${USAGE}\n`);
    return 64;
  }
  let input: Uint8Array;
  try {
    input = file !== undefined && file !== "-" ? io.readFile(file) : io.readStdin();
  } catch (err) {
    io.stderr(`io: ${String(err)}\n`);
    return 66;
  }
  try {
    const { canonical } = verifyEnvelopeBytes(input);
    io.stdout(cmd === "digest" ? `${await sha256Hex(canonical)}\n` : canonical);
    return 0;
  } catch (err) {
    io.stderr(`error: ${errorCode(err)}\n`);
    return 2;
  }
}
