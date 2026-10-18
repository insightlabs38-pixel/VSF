import { readFileSync } from "node:fs";
import { canonicalizeFromStdinOrFile } from "./cli-lib.ts";

process.exitCode = await canonicalizeFromStdinOrFile(process.argv.slice(2), {
  readFile: (p) => readFileSync(p),
  readStdin: () => readFileSync(0),
  stdout: (b) => process.stdout.write(b),
  stderr: (s) => process.stderr.write(s),
});
