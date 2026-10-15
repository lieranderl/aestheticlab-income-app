// Phone checks (M11): every screen at iPhone SE, iPhone 15 and Pixel 7 sizes,
// in light and dark themes, against cmd/e2e (memory store, fixed November
// clock, seeded staff). Run with `make mobile` or `bun run test:mobile`.
import { defineConfig, devices } from "@playwright/test";

const port = process.env.E2E_PORT ?? "8097";

export default defineConfig({
  testDir: ".",
  outputDir: "../tmp/playwright/results",
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  // One server holds the data for every device, so flows run one at a time.
  workers: 1,
  reporter: process.env.CI
    ? [["list"], ["html", { outputFolder: "../tmp/playwright/report", open: "never" }]]
    : [["list"]],
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    locale: "ru-RU",
    timezoneId: "Europe/Brussels",
    trace: "retain-on-failure",
  },
  projects: [
    { name: "iPhone SE", use: { ...devices["iPhone SE (3rd gen)"] } },
    { name: "iPhone 15", use: { ...devices["iPhone 15"] } },
    { name: "Pixel 7", use: { ...devices["Pixel 7"] } },
  ],
  webServer: {
    command: "go run ./cmd/e2e",
    cwd: "..",
    url: `http://127.0.0.1:${port}/readyz`,
    env: { PORT: port },
    reuseExistingServer: !process.env.CI,
    stdout: "ignore",
    timeout: 120_000,
  },
});
