import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./specs",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  timeout: 90_000,
  reporter: [["list"], ["json", { outputFile: "../../artifacts/logs/browser-results.json" }]],
  outputDir: "../../artifacts/browser-output",
});
