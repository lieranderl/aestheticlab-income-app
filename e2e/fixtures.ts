import { expect, type Page } from "@playwright/test";

// Test-only sign-ins seeded by cmd/e2e (keep the two in step).
export const users = {
  anna: { username: "anna", password: "e2e-anna-pass" },
  olga: { username: "olga", password: "e2e-olga-pass" },
} as const;

export async function signIn(page: Page, who: keyof typeof users) {
  const { username, password } = users[who];
  await page.goto("/login");
  await page.locator('input[name="username"]').fill(username);
  await page.locator('input[name="password"]').fill(password);
  await page.locator('button[type="submit"]').click();
  await expect(page).not.toHaveURL(/\/login/);
}

// phoneRules checks the mobile-ui skill's layout rules on the current page and
// returns every problem found, so one run reports them all.
export async function phoneRules(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const problems: string[] = [];
    const doc = document.documentElement;
    if (doc.scrollWidth > window.innerWidth + 1) {
      problems.push(`sideways scroll: page is ${doc.scrollWidth}px wide in a ${window.innerWidth}px viewport`);
    }
    const shown = (el: Element) => {
      const r = el.getBoundingClientRect();
      const s = getComputedStyle(el);
      return r.width > 0 && r.height > 0 && s.visibility !== "hidden" && s.display !== "none";
    };
    const name = (el: Element) =>
      `${el.tagName.toLowerCase()}${el.getAttribute("name") ? `[name=${el.getAttribute("name")}]` : ""} «${(el.textContent || el.getAttribute("aria-label") || "").trim().slice(0, 30)}»`;

    for (const el of document.querySelectorAll("input:not([type=hidden]):not([type=checkbox]):not([type=radio]), select, textarea")) {
      if (!shown(el)) continue;
      const size = parseFloat(getComputedStyle(el).fontSize);
      if (size < 16) problems.push(`input under 16px (${size}px, Safari zooms): ${name(el)}`);
    }
    // Buttons and button-styled links: at least 44 × 44 pt (the dock is checked below).
    for (const el of document.querySelectorAll("main button, main .btn, main [role=button]")) {
      if (!shown(el) || el.closest("dialog:not([open])")) continue;
      const r = el.getBoundingClientRect();
      if (r.height < 43.5 || r.width < 43.5) problems.push(`tap target ${Math.round(r.width)}×${Math.round(r.height)}pt: ${name(el)}`);
    }
    // An open sheet (modal dialog) covers the bottom bar on purpose.
    const dock = document.querySelector("nav.dock");
    if (dock && shown(dock) && !document.querySelector("dialog[open]")) {
      for (const item of dock.querySelectorAll("a, button")) {
        const r = item.getBoundingClientRect();
        if (r.height < 43.5 || r.width < 43.5) problems.push(`dock item ${Math.round(r.width)}×${Math.round(r.height)}: ${name(item)}`);
        const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
        if (!hit || !item.contains(hit)) problems.push(`dock item covered by ${hit ? name(hit) : "nothing"}: ${name(item)}`);
      }
    }
    return problems;
  });
}
