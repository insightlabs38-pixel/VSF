import { defineConfig } from "@playwright/test";

const port = Number(process.env.VSF_FIXTURE_PORT ?? 4173);

export default defineConfig({
  testDir: "./specs",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: [["list"], ["json", { outputFile: "../../artifacts/logs/e2e-results.json" }]],
  outputDir: "../../artifacts/e2e-output",
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    browserName: "chromium",
    trace: "off",
    colorScheme: "light",
    locale: "en-US",
    timezoneId: "UTC",
    viewport: { width: 1024, height: 768 },
  },
  webServer: {
    command: "node --conditions=source ../../fixtures/source-apps/src/main.ts",
    port,
    env: { PORT: String(port) },
    reuseExistingServer: false,
    timeout: 30_000,
  },
});
