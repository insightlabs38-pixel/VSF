import { type ChildProcess, spawn, spawnSync } from "node:child_process";
import { cpSync, existsSync, mkdirSync, rmSync } from "node:fs";
import { fileURLToPath } from "node:url";

export const ROOT = fileURLToPath(new URL("../../../../../", import.meta.url));
export const CACHE = process.env.AUDIT_CACHE ?? `${ROOT}.audit-cache/`;
export const STATE = `${CACHE}state/`;
export const SEED = `${CACHE}seed/`;
const PY = `${CACHE}venv/bin/`;
const PYG = `${CACHE}venv-gradio/bin/`;

export interface Running {
  name: string;
  origin: string;
  dir: string;
  stop(): Promise<void>;
}

export interface AppDef {
  name: string;
  port: number;
  /** Command and arguments to run in `dir`, given the disposable state directory. */
  cmd(dir: string): { bin: string; args: string[]; cwd?: string; env?: Record<string, string> };
  ready: string;
}

const ENV_ROOT = { GITEA_I_AM_BEING_UNSAFE_RUNNING_AS_ROOT: "true" };

export const APPS: Record<string, AppDef> = {
  gitea: {
    name: "gitea",
    port: 13000,
    ready: "/",
    cmd: (dir) => ({
      bin: `${CACHE}bin/gitea`,
      args: ["--config", `${dir}/custom/conf/app.ini`, "--work-path", dir, "web"],
      env: ENV_ROOT,
    }),
  },
  pocketbase: {
    name: "pocketbase",
    port: 18090,
    ready: "/api/health",
    cmd: (dir) => ({ bin: `${CACHE}bin/pocketbase`, args: ["serve", "--dir", `${dir}/pb_data`, "--http", "127.0.0.1:18090"] }),
  },
  jupyterlab: {
    name: "jupyterlab",
    port: 18888,
    ready: "/api/status",
    cmd: (dir) => ({
      bin: `${PY}jupyter`,
      cwd: `${dir}/work`,
      args: [
        "lab",
        "--no-browser",
        "--allow-root",
        "--ip=127.0.0.1",
        "--port=18888",
        "--ServerApp.token=",
        "--ServerApp.password=",
        `--ServerApp.root_dir=${dir}/work`,
        "--LabApp.news_url=",
        "--LabApp.check_for_updates_class=jupyterlab.NeverCheckForUpdate",
      ],
      env: { JUPYTER_CONFIG_DIR: `${dir}/jconf`, JUPYTER_DATA_DIR: `${dir}/jdata`, JUPYTER_RUNTIME_DIR: `${dir}/jrun`, IPYTHONDIR: `${dir}/ipy` },
    }),
  },
  datasette: {
    name: "datasette",
    port: 18001,
    ready: "/",
    cmd: (dir) => ({ bin: `${PY}datasette`, args: ["serve", `${dir}/films.db`, "--host", "127.0.0.1", "--port", "18001", "-m", `${dir}/metadata.json`] }),
  },
  swagger: {
    name: "swagger",
    port: 18002,
    ready: "/openapi.json",
    cmd: (dir) => ({ bin: "node", args: [`${ROOT}docs/deep/reviews/post-b12-viability/harness/swagger-mock.mjs`, dir, "18002"] }),
  },
  kanboard: {
    name: "kanboard",
    port: 18081,
    ready: "/",
    cmd: (dir) => ({ bin: "php", args: ["-S", "127.0.0.1:18081", "-t", `${CACHE}kanboard-src`], env: { KANBOARD_DATA_DIR: `${dir}/data` } }),
  },
  todomvc: {
    name: "todomvc",
    port: 18870,
    ready: "/react/dist/index.html",
    cmd: () => ({ bin: "python3", args: ["-m", "http.server", "18870", "--bind", "127.0.0.1", "--directory", `${CACHE}../.corpus-cache/src/todomvc-react/examples`] }),
  },
  gradio: {
    name: "gradio",
    port: 18860,
    ready: "/",
    cmd: (dir) => ({ bin: `${PYG}python`, args: [`${dir}/run.py`], env: { GRADIO_SERVER_NAME: "127.0.0.1", GRADIO_SERVER_PORT: "18860", GRADIO_ANALYTICS_ENABLED: "False" } }),
  },
};

async function until(url: string, ms: number): Promise<void> {
  const t0 = Date.now();
  for (;;) {
    try {
      const r = await fetch(url, { redirect: "manual" });
      if (r.status < 500) return;
    } catch {
      /* not up yet */
    }
    if (Date.now() - t0 > ms) throw new Error(`not ready: ${url}`);
    await new Promise((r) => setTimeout(r, 250));
  }
}

/** Restores the sealed seed copy of an application and starts it; variant picks a subdirectory for multi-demo apps. */
export async function start(name: string, variant?: string): Promise<Running> {
  const def = APPS[name];
  if (!def) throw new Error(`unknown app ${name}`);
  spawnSync("fuser", ["-k", "-9", `${def.port}/tcp`], { stdio: "ignore" });
  await new Promise((r) => setTimeout(r, 300));
  const seed = `${SEED}${variant ?? name}`;
  const dir = `${STATE}${variant ?? name}`;
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(STATE, { recursive: true });
  if (existsSync(seed)) cpSync(seed, dir, { recursive: true });
  else mkdirSync(dir, { recursive: true });
  const c = def.cmd(dir);
  const child: ChildProcess = spawn(c.bin, c.args, {
    cwd: c.cwd ?? dir,
    env: { ...process.env, ...c.env },
    stdio: ["ignore", "ignore", "ignore"],
    detached: true,
  });
  const origin = `http://127.0.0.1:${def.port}`;
  await until(`${origin}${def.ready}`, 60_000);
  return {
    name,
    origin,
    dir,
    async stop() {
      try {
        if (child.pid) process.kill(-child.pid, "SIGKILL");
      } catch {
        /* already gone */
      }
      spawnSync("fuser", ["-k", "-9", `${def.port}/tcp`], { stdio: "ignore" });
      await new Promise((r) => setTimeout(r, 300));
    },
  };
}

export function sh(bin: string, args: string[], opt: { cwd?: string; env?: Record<string, string> } = {}): string {
  const r = spawnSync(bin, args, { cwd: opt.cwd, env: { ...process.env, ...opt.env }, encoding: "utf8" });
  if (r.status !== 0) throw new Error(`${bin} ${args.join(" ")}: ${r.stderr}`);
  return r.stdout;
}
