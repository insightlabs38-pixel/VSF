import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./specs",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  timeout: 60_000,
  reporter: [["list"], ["json", { outputFile: "../../artifacts/logs/ui-results.json" }]],
  outputDir: "../../artifacts/ui-output",
});
