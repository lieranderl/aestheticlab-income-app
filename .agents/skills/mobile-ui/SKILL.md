---
name: mobile-ui
description: Mobile-first screen rules and the Russian UI vocabulary for the Kassa app — 375 pt layouts, cards vs tables, bottom bar, bottom sheets, input rules for iPhone, safe areas, money/date formatting, glossary of terms. Use when creating or reviewing any templ page/component, copy text, or Playwright mobile checks.
---

# Mobile-first UI and Russian copy

Source: `docs/system-design.md` §6–7. Every screen is designed at **375 pt wide** first. Desktop is the same template, widened at `lg:`.

## Layout checklist (every screen)
- [ ] **No horizontal scroll at 375 pt.** Wide tables only `hidden lg:table`; below `lg`, show a card list of the same data.
- [ ] **One column on phones.** Figure tiles go 2 per row (`grid grid-cols-2 gap-3`).
- [ ] **The main action sits at the bottom.** A sticky bar (`sticky bottom-0`, *not* `fixed`), padded with `pb-safe` (`env(safe-area-inset-bottom)`).
- [ ] **Bottom nav:** daisyUI `dock`, at most 5 items, icon + short label. Items by role are in system-design §6.2.
- [ ] **Touch targets ≥ 44 × 44 pt**, ≥ 8 pt apart. No hover-only information, no long-press, no drag and drop (use ↑ ↓ buttons).
- [ ] **Editing opens a bottom sheet:** native `<dialog class="modal modal-bottom sm:modal-middle">`.
- [ ] **Date header:** «‹ 08.10.2026 ›» with arrow buttons, and tapping the date opens the native `<input type="date">`. **Month header:** «‹ Октябрь 2026 ›» (don't use `<input type="month">`).
- [ ] **Light and dark themes** both checked.
- [ ] **No inline `style`** (blocked by CSP).

## Inputs (iPhone)
- **Money fields:** `inputmode="decimal"`, `enterkeyhint="next"`, `autocomplete="off"`, `class="input input-lg w-full text-base"` (≥ 16 px, so Safari doesn't zoom).
- **Order:** the field order is the keyboard's "next" order. The first field gets `autofocus`.
- **Empty means 0.** Accept `600`, `600,5` and `600.50`.
- **Login:** `autocomplete="username"` / `"current-password"` / `"new-password"`, so password managers work.

## Formatting
| What | Format | Example |
|---|---|---|
| Money | `€` + thin-space thousands separator + comma decimals; whole amounts drop `,00` | `€1 234,50`, `€600` |
| Date | `DD.MM.YYYY` | `08.10.2026` |
| Month | capitalised nominative + year | `Октябрь 2026` |
| Weekday | short | `чт, 08.10` |
| Percent | integer + % | `50%` |
| Plurals | `ru.Plural(n, "день", "дня", "дней")` | `21 рабочий день`, `3 дня` |

## Glossary (use exactly these words)
| Term | Meaning |
|---|---|
| **Себе** | The person's share of their revenue. |
| **Салону** | The salon's share (Total − Себе). Never call this «В кассу». |
| **Касса** | The salon's money: «Касса — наличные» (the till) and «Касса — карта салона». |
| **Оставьте в кассе** | The salon's part of the day's cash on the daily form: «Наличными в кассу» (left in the till) and «Переводом на карту салона» (transferred to the kassa card). |
| **Салон должен вам / Вы должны салону** | The person's balance (+ / −). |
| **Внесение** | Deposit: a person puts money into the kassa. |
| **Выплата** | Payout: kassa → person. |
| **Расход** | Expense; «Расход за свой счёт» when a person paid it themselves. |
| **Перевод** | Moving money between the kassa till and the card. |
| **Корректировка / Пересчёт кассы** | Recount and its adjustment. |
| **Закрыть месяц / Открыть заново** | Close a month / reopen it. |
| **Итоги и расчёт** | Monthly result and settlement. |
| **Дивиденды, Зарплата** | Dividends, salary. |
| **Ожидает подтверждения** | Pending: waiting for the admin to confirm. |
| **импорт** | Badge on months that came from the Google Sheet. |

## Tone
- **Address the user formally («вы»).** Keep it short and friendly.
- **Gender-neutral wording.** Use imperatives or nouns («Внести в кассу», «Расход за свой счёт»), never past-tense verbs that mark gender («Внесла / Внёс»).
- **Errors say what to do:** «Сумма оплат меньше выручки на €50 — проверьте наличные».
- **Toasts:** «Сохранено ✓», «Нет сети — данные сохранены на телефоне, нажмите ещё раз».

## Verifying on a phone viewport
- **Built-in browser:** use `resize_window` with the `mobile` preset (375 × 812), take a screenshot, and check the list above. Do it again with `colorScheme: dark`. Reset with the `desktop` preset afterwards.
- **Playwright** (from M11): `bun run test:mobile` with the iPhone SE, iPhone 15 and Pixel 7 profiles.
