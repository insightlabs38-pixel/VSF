import { fileURLToPath } from "node:url";
import { buildExtension } from "./build.ts";
import { auditPackage, type Variant } from "./src/audit.ts";

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  let failed = 0;
  for (const variant of ["product", "test"] as Variant[]) {
    const hosts = variant === "product" ? [] : ["http://127.0.0.1/*"];
    const dir = await buildExtension({ name: `audit-${variant}`, hostPermissions: hosts });
    const findings = auditPackage(dir, variant);
    for (const f of findings) console.error(`audit ${variant}: ${f.rule}: ${f.detail}`);
    failed += findings.length;
    console.log(`audit ${variant}: ${findings.length === 0 ? "ok" : `${findings.length} findings`}`);
  }
  process.exit(failed === 0 ? 0 : 1);
}
