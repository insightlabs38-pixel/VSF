import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { findSecretShapes } from "@vsf/redact";
import { validateRegistry, writeRegistry } from "./catalog.ts";
import { loadJobs, runJob, verifyManifest } from "./ci.ts";
import { lintCommentFiles } from "./comments.ts";
import { checkIdentity, checkMessage } from "./commitmsg.ts";
import type { Locked } from "./depreview.ts";
import { scan } from "./depscan.ts";
import { digestTree, rollup } from "./digest.ts";
import { auditPackage } from "./extaudit.ts";
import { checkWorkflow, parseWorkflow } from "./gate.ts";
import { type Ignore, scanVulnerabilities } from "./osv.ts";
import { checkInstalled, checkPins } from "./toolchain.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const [, , cmd = "", ...args] = process.argv;

const git = (...a: string[]): string => {
  const r = spawnSync("git", a, { cwd: root, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  if (r.status !== 0) throw new Error(`git ${a.join(" ")}: ${r.stderr}`);
  return r.stdout;
};

function report(
  kind: string,
  items: { id?: string; rule?: string; message?: string; detail?: string; package?: string }[],
): number {
  for (const i of items) console.error(`${kind}: ${i.id ?? i.rule}: ${i.message ?? `${i.package} ${i.detail}`}`);
  if (items.length === 0) console.log(`${kind}: ok`);
  return items.length === 0 ? 0 : 1;
}

switch (cmd) {
  case "toolchain": {
    const findings = checkPins(root);
    if (!args.includes("--manifest-only")) findings.push(...checkInstalled(root));
    process.exit(report("toolchain", findings));
    break;
  }
  case "comments": {
    const files = (
      args.length
        ? args
        : git("ls-files", "--cached", "--others", "--exclude-standard", "*.rs", "*.ts", "*.go", "*.sh")
            .split("\n")
            .filter(Boolean)
    )
      .filter((f) => !f.startsWith("docs/deep/plan/") && !f.includes("generated."))
      .map((f) => join(root, f));
    const v = lintCommentFiles(files);
    process.exit(
      report(
        "comments",
        v.map((x) => ({ id: `${x.file.replace(`${root}/`, "")}:${x.line}`, message: x.message })),
      ),
    );
    break;
  }
  case "ext-audit": {
    const dirs = args.length
      ? args
      : [join(root, "artifacts/extension/product"), join(root, "artifacts/extension/test")];
    const problems: { id: string; message: string }[] = [];
    const reports = dirs.map((d) => {
      const variant = d.replace(/\/$/, "").split("/").pop() ?? "";
      const r = auditPackage(d, variant);
      for (const f of r.findings) problems.push({ id: `${variant}:${f.file}:${f.rule}`, message: f.message });
      return r;
    });
    const out = join(root, "artifacts/extension");
    mkdirSync(out, { recursive: true });
    writeFileSync(join(out, "audit.json"), `${JSON.stringify(reports, null, 1)}\n`);
    process.exit(report("ext-audit", problems));
    break;
  }
  case "commit-msg": {
    const text = readFileSync(args[0] ?? "", "utf8").replace(/^#.*$/gm, "");
    process.exit(report("commit-msg", checkMessage(text)));
    break;
  }
  case "commit-range": {
    const range = args[0] ?? "";
    const problems: { id: string; message: string }[] = [];
    for (const sha of git("rev-list", range).split("\n").filter(Boolean)) {
      const [an, ae, cn, ce] = git("log", "-1", "--format=%an%n%ae%n%cn%n%ce", sha).split("\n");
      for (const p of [
        ...checkMessage(git("log", "-1", "--format=%B", sha)),
        ...checkIdentity(an ?? "", ae ?? ""),
        ...checkIdentity(cn ?? "", ce ?? ""),
      ]) {
        problems.push({ id: `${sha.slice(0, 10)} ${p.id}`, message: p.message });
      }
    }
    process.exit(report("commit-range", problems));
    break;
  }
  case "dep-review": {
    const { locked, findings } = scan({
      root,
      advisoriesPath: join(root, "supply-chain/advisories.local.json"),
      licenses: !args.includes("--no-licenses"),
    });
    const out = join(root, "artifacts/supply-chain");
    mkdirSync(out, { recursive: true });
    writeFileSync(join(out, "dep-review.json"), `${JSON.stringify({ locked: locked.length, findings }, null, 1)}\n`);
    console.log(`dep-review: ${locked.length} locked packages`);
    process.exit(report("dep-review", findings));
    break;
  }
  case "vuln-scan": {
    const { locked } = scan({
      root,
      advisoriesPath: join(root, "supply-chain/advisories.local.json"),
      licenses: false,
    });
    const ignorePath = join(root, "supply-chain/vuln-ignores.json");
    const ignores = existsSync(ignorePath)
      ? (JSON.parse(readFileSync(ignorePath, "utf8")) as { ignores: Ignore[] }).ignores
      : [];
    const outcome = await scanVulnerabilities(locked as Locked[], { ignores });
    const out = join(root, "artifacts/supply-chain");
    mkdirSync(out, { recursive: true });
    writeFileSync(
      join(out, "vuln-scan.json"),
      `${JSON.stringify({ at: new Date().toISOString(), source: "osv.dev", ...outcome }, null, 1)}\n`,
    );
    if (outcome.status === "unavailable") {
      console.error(`vuln-scan: INCONCLUSIVE, not clean: ${outcome.reason} (${outcome.queried} packages unchecked)`);
      process.exit(3);
    }
    console.log(`vuln-scan: ${outcome.queried} packages checked against osv.dev, request ${outcome.digest}`);
    process.exit(
      report(
        "vuln-scan",
        outcome.findings.map((f) => ({ id: f.package, message: f.ids.join(", ") })),
      ),
    );
    break;
  }
  case "workflow-check": {
    const wf = parseWorkflow(readFileSync(join(root, ".github/workflows/ci.yml"), "utf8"));
    const jobs = (JSON.parse(readFileSync(join(root, "ci/jobs.json"), "utf8")) as { jobs: Record<string, unknown[]> })
      .jobs;
    process.exit(
      report(
        "workflow-check",
        checkWorkflow(wf, jobs).map((m) => ({ id: "workflow", message: m })),
      ),
    );
    break;
  }
  case "ci-run": {
    const job = args[0] ?? "";
    const out = join(root, "artifacts/ci", job);
    const induce = process.env.VSF_INDUCE_FAILURE === "1";
    const manifest = runJob(root, loadJobs(join(root, "ci/jobs.json")), job, out, induce);
    const problems = verifyManifest(out);
    for (const p of problems) console.error(`manifest: ${p}`);
    console.log(`ci-run ${job}: ${manifest.status}`);
    process.exit(manifest.status === "PASS" && problems.length === 0 ? 0 : 1);
    break;
  }
  case "ci-verify": {
    const problems = verifyManifest(resolve(args[0] ?? ""));
    process.exit(
      report(
        "ci-verify",
        problems.map((p) => ({ id: "manifest", message: p })),
      ),
    );
    break;
  }
  case "pr-text": {
    const problems = [
      ...checkMessage(process.env.PR_TITLE ?? "").map((p) => ({ ...p, id: `title.${p.id}` })),
      ...((process.env.PR_BODY ?? "").trim() === "" ? [] : checkMessage(process.env.PR_BODY ?? "")).map((p) => ({
        ...p,
        id: `body.${p.id}`,
      })),
    ];
    if ((process.env.PR_TITLE ?? "").split("\n").length > 1)
      problems.push({ id: "title.multiline", message: "title must be one line" });
    process.exit(report("pr-text", problems));
    break;
  }
  case "catalog": {
    if (!args.includes("--check")) writeRegistry(root);
    process.exit(
      report(
        "catalog",
        validateRegistry(root).map((m) => ({ id: "registry", message: m })),
      ),
    );
    break;
  }
  case "secrets": {
    const files = git("ls-files", "--cached", "--others", "--exclude-standard").split("\n").filter(Boolean);
    const hits: { id: string; message: string }[] = [];
    for (const f of files) {
      if (f.endsWith(".wasm") || f === "pnpm-lock.yaml") continue;
      let text: string;
      try {
        text = readFileSync(join(root, f), "utf8");
      } catch {
        continue;
      }
      const kinds = findSecretShapes(text);
      if (kinds.length > 0) hits.push({ id: f, message: kinds.join(",") });
    }
    process.exit(report("secrets", hits));
    break;
  }
  case "digest": {
    const entries = digestTree(root, args.length ? args : ["artifacts/wasm", "artifacts/bin"]);
    console.log(JSON.stringify({ rollup: rollup(entries), entries }, null, 1));
    break;
  }
  default:
    console.error(
      "usage: checks <toolchain|comments|commit-msg|commit-range|pr-text|catalog|secrets|dep-review|vuln-scan|workflow-check|ci-run|ci-verify|digest>",
    );
    process.exit(64);
}
