import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./specs",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  timeout: 90_000,
  reporter: [["list"], ["json", { outputFile: "../../artifacts/logs/specialized-results.json" }]],
  outputDir: "../../artifacts/specialized-output",
});
