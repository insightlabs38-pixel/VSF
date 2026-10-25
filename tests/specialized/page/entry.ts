import {
  detectEnvironment,
  fromSource,
  loadedAdapters,
  negotiate,
  OutboundQueue,
  openAdapter,
  Pipe,
  planReconnect,
  tableElement,
  toTable,
  validateManifest,
} from "@vsf/specialized";
import { socketClient } from "./client.ts";

const api = {
  fromSource,
  toTable,
  tableElement,
  openAdapter,
  loadedAdapters,
  detectEnvironment,
  negotiate,
  validateManifest,
  planReconnect,
  Pipe,
  OutboundQueue,
  socketClient,
};

declare global {
  interface Window {
    vsf: typeof api;
  }
}

window.vsf = api;
