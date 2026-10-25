import { gradio } from "./apps/gradio.ts";
import { kanboard } from "./apps/kanboard.ts";
import { swagger } from "./apps/swagger.ts";
import { calibration } from "./apps/calibration.ts";
import { datasette } from "./apps/datasette.ts";
import { gitea } from "./apps/gitea.ts";
import { jupyterlab } from "./apps/jupyterlab.ts";
import { pocketbase } from "./apps/pocketbase.ts";
import type { Workflow } from "./spec.ts";

export const WORKFLOWS: Workflow[] = [...gitea, ...pocketbase, ...jupyterlab, ...datasette, ...swagger, ...kanboard, ...gradio];

export const CALIBRATION: Workflow[] = calibration;
