// The speed budget from system-design §6.7: a usable form within 1.5 s on 4G,
// with Datastar, one CSS file and the font as the only weight. Measured in
// Chromium (it can throttle the network) with an empty cache and no service
// worker, over Lighthouse's slow-4G profile: 150 ms round trips, 1.6 Mbit/s.
import { expect, test, type Page } from "@playwright/test";
import { signIn } from "./fixtures";

test.use({ serviceWorkers: "block" });
test.skip(({ browserName }) => browserName !== "chromium", "network throttling needs Chromium");

async function slow4G(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Network.enable");
  await cdp.send("Network.setCacheDisabled", { cacheDisabled: true });
  await cdp.send("Network.emulateNetworkConditions", {
    offline: false,
    latency: 150,
    downloadThroughput: (1.6 * 1024 * 1024) / 8,
    uploadThroughput: (750 * 1024) / 8,
  });
}

async function timeToUsable(page: Page, path: string, field: string) {
  const start = Date.now();
  await page.goto(path, { waitUntil: "load" });
  await expect(page.locator(field)).toBeEditable();
  return Date.now() - start;
}

async function weights(page: Page) {
  return page.evaluate(() => {
    const kb = (n: number) => Math.round(n / 102.4) / 10;
    const sum = (re: RegExp) =>
      (performance.getEntriesByType("resource") as PerformanceResourceTiming[])
        .filter((e) => re.test(new URL(e.name).pathname))
        .reduce((n, e) => n + e.encodedBodySize, 0);
    return { css: kb(sum(/\.css$/)), js: kb(sum(/\.js$/)), fonts: kb(sum(/\.woff2$/)) };
  });
}

test("first open: the sign-in form", async ({ page }) => {
  await slow4G(page);
  const ms = await timeToUsable(page, "/login", 'input[name="username"]');
  const w = await weights(page);
  console.log(`speed: /login usable in ${ms} ms; CSS ${w.css} KB, JS ${w.js} KB, fonts ${w.fonts} KB`);
  expect(w.css, "CSS over the wire (KB)").toBeLessThanOrEqual(30);
  expect(w.js, "JS over the wire (KB)").toBeLessThanOrEqual(20);
  expect(w.fonts, "fonts (KB)").toBeLessThanOrEqual(60);
  expect(ms).toBeLessThan(1500);
});

test("signed in: a new client form", async ({ page }) => {
  await signIn(page, "olga");
  await slow4G(page);
  const ms = await timeToUsable(page, "/clients/new", 'input[name="services"]');
  console.log(`speed: /clients/new usable in ${ms} ms`);
  expect(ms).toBeLessThan(1500);
});
