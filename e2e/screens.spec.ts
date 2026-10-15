// Every screen, per role: no sideways scroll, inputs ≥ 16 px, tap targets
// ≥ 44 pt and a usable bottom bar, with full-page screenshots in both themes.
import { expect, test } from "@playwright/test";
import { phoneRules, signIn, users } from "./fixtures";

const screens: Record<"anonymous" | keyof typeof users, string[]> = {
  anonymous: ["/login", "/offline"],
  olga: ["/", "/clients/new", "/my", "/movements/new", "/profile"],
  anna: [
    "/",
    "/clients/new",
    "/admin/day",
    "/admin/month",
    "/admin/kassa",
    "/admin/movements/new",
    "/admin/history",
    "/admin/history/movements",
    "/admin/audit",
    "/admin/users",
    "/admin/users/new",
    "/admin/categories",
    "/admin/categories/new",
    "/admin/trends",
    "/admin/export",
    "/my",
    "/more",
    "/profile",
  ],
};

for (const [who, paths] of Object.entries(screens)) {
  test.describe(who, () => {
    if (who !== "anonymous") test.beforeEach(({ page }) => signIn(page, who as keyof typeof users));

    for (const path of paths) {
      test(path, async ({ page }, info) => {
        const res = await page.goto(path);
        expect(res?.status(), `${path} status`).toBeLessThan(400);
        for (const scheme of ["light", "dark"] as const) {
          await page.emulateMedia({ colorScheme: scheme });
          expect.soft(await phoneRules(page), `${path} (${scheme})`).toEqual([]);
          const file = info.outputPath(`${who}${path.replaceAll("/", "_") || "_home"}-${scheme}.png`);
          // CSS-pixel scale keeps long pages (the change log) under the
          // 32 767 px image limit; anything taller gets the first screen only.
          const height = await page.evaluate(() => document.documentElement.scrollHeight);
          await page.screenshot({ path: file, fullPage: height <= 30000, scale: "css" });
          await info.attach(`${scheme}`, { path: file, contentType: "image/png" });
        }
      });
    }
  });
}
