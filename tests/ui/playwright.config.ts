import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./specs",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  timeout: 60_000,
  reporter: [["list"], ["json", { outputFile: "../../artifacts/logs/ui-results.json" }]],
  snapshotPathTemplate: "{testDir}/../golden/gallery/{arg}{ext}",
  expect: { toHaveScreenshot: { maxDiffPixelRatio: 0.0005, threshold: 0.1 } },
  outputDir: "../../artifacts/logs/ui-output",
  use: {
    launchOptions: {
      args: [
        "--font-render-hinting=none",
        "--disable-font-subpixel-positioning",
        "--disable-lcd-text",
        "--force-color-profile=srgb",
      ],
    },
  },
});
