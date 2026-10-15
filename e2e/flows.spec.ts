// The three phone flows from the M11 plan: a client entry, the movement sheet
// and a settlement payout. Each saves through Datastar, the way a phone does,
// and checks the save button is on screen (not under the bottom bar) first.
import { expect, test, type Locator } from "@playwright/test";
import { phoneRules, signIn } from "./fixtures";

async function tapSave(button: Locator) {
  await button.scrollIntoViewIfNeeded();
  await expect(button).toBeInViewport();
  const box = await button.boundingBox();
  expect(box!.height).toBeGreaterThanOrEqual(44);
  await button.click();
}

test("staff enter a client", async ({ page }) => {
  await signIn(page, "olga");
  await page.goto("/clients/new");
  await page.locator('input[name="services"]').fill("100");
  await page.locator('input[name="serviceCash"]').fill("100");
  // The live preview: 70% of €100 is the person's share.
  await expect(page.getByText("«Себе» €65")).toBeVisible();
  expect(await phoneRules(page)).toEqual([]);
  await tapSave(page.locator("#client-form").getByRole("button", { name: "Сохранить" }));
  await expect(page.getByText("Сохранено ✓")).toBeVisible();
});

test("staff record a deposit in the movement sheet", async ({ page }) => {
  await signIn(page, "olga");
  await page.goto("/my");
  await page.getByRole("link", { name: "+ Внести в кассу" }).click();
  const sheet = page.getByRole("dialog", { name: "Движение" });
  await expect(sheet).toBeVisible();
  await sheet.locator('input[name="amount"]').fill("10");
  expect(await phoneRules(page)).toEqual([]);
  await tapSave(sheet.getByRole("button", { name: "Сохранить" }));
  await expect(page.getByText("Сохранено — ожидает подтверждения")).toBeVisible();
});

test("admin records a settlement payout or deposit", async ({ page }) => {
  await signIn(page, "anna");
  await page.goto("/admin/month?m=2026-11&tab=settlement");
  // The client entered above leaves at least one person with something to settle.
  const action = page.getByRole("link", { name: /^(Выплатить|Внести в кассу) €/ }).first();
  await expect(action).toBeVisible();
  await action.click();
  const sheet = page.getByRole("dialog", { name: "Движение" });
  await expect(sheet).toBeVisible();
  await expect(sheet.locator('input[name="amount"]')).not.toHaveValue("");
  expect(await phoneRules(page)).toEqual([]);
  await tapSave(sheet.getByRole("button", { name: "Сохранить" }));
  await expect(page.getByText("Сохранено ✓")).toBeVisible();
});
