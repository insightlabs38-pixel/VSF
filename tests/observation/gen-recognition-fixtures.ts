import { writeFileSync } from "node:fs";
import { captureApps, SNAPSHOTS_DIR } from "./support/recognition.ts";

const check = process.argv.includes("--check");
let stale = 0;
for (const [name, snap] of await captureApps()) {
  const text = `${JSON.stringify(snap, null, 1)}\n`;
  const file = `${SNAPSHOTS_DIR}${name}.json`;
  if (check) {
    const have = await import("node:fs").then((fs) => (fs.existsSync(file) ? fs.readFileSync(file, "utf8") : ""));
    if (have !== text) {
      console.error(`stale snapshot: ${name}`);
      stale++;
    }
  } else {
    writeFileSync(file, text);
    console.log(`wrote ${name}: ${snap.nodes.length} nodes`);
  }
}
process.exit(stale === 0 ? 0 : 1);
