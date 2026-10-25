import type { Oracle } from "./spec.ts";
import { gradioOracles } from "./apps/gradio.ts";
import { kanboardOracles } from "./apps/kanboard.ts";
import { swaggerOracles } from "./apps/swagger.ts";
import { calibrationOracles } from "./apps/calibration.ts";
import { datasetteOracles } from "./apps/datasette.ts";
import { giteaOracles } from "./apps/gitea.ts";
import { jupyterOracles } from "./apps/jupyterlab.ts";
import { pocketbaseOracles } from "./apps/pocketbase.ts";

export const ORACLES: Record<string, Oracle> = { ...giteaOracles, ...pocketbaseOracles, ...jupyterOracles, ...datasetteOracles, ...swaggerOracles, ...kanboardOracles, ...gradioOracles, ...calibrationOracles };
