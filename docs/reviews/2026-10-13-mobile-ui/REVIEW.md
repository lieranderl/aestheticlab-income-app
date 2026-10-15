# Mobile entry and financial information audit — 13 October 2026

> Screenshots are not published: they were taken from the live app.

Status: audit complete; improvements implemented and verified locally on `feat/mobile-ui-clarity`, not deployed. The findings and first screenshot set below preserve the original audit evidence.

User requirement: convenient phone entry, clear separation of input and explanatory information, and dashboards structured for analysis.

## Scope and evidence

Captured the current app at commit `5575c76` in the Codex in-app browser, at 375 × 812 CSS pixels in dark mode. Used an isolated, disposable memory server and local Anna account. No production data or accounts changed. The valid example is services €100, creams €20, certificate redemption €40, and €30 cash contributed for services. It produces revenue €120, Self €50, Salon €70, cash contribution €50, retained cash €30, and a €20 increase in the salon's debt to the employee.

Screenshots below show the existing UI. Full-page captures retain fixed navigation at the capture viewport position; that placement inside the image is not evidence that scrolling content is permanently covered.

## Captured steps

| Step | Screen/task | Assessment | Evidence |
|---|---|---|---|
| 1 | Empty own day | Clear primary action; zero category rows compete with the empty-state message. | 01 |
| 2 | New client, collapsed extras | Large labelled fields and separate result card; instructions are interspersed with entry. | 02 |
| 3 | Services, extras and certificate redemption | Sale and redemption are grouped together; this hides their different meaning. Valid save showed «Сохранено ✓». | 03 |
| 4 | Monthly revenue | Twelve equally weighted tiles precede employee analysis; categories are repeated. | 04 |
| 5 | Monthly settlement | Estimates are labelled, but projected and current balances need stronger separation. | 05 |
| 6 | Kassa | Cash/card balances and confirmation section are distinct; certificate warning needs an actionable explanation. | 06 |
| 7 | Movement sheet | Large fields and operation selection; expense-only guidance appears even for a deposit. | 07 |
| 8 | Admin day | Same dense metric order as month; repeated detail postpones individual client records. | 08 |
| 9 | Own month | Current balance is explicitly dated; totals are repeated in the following breakdown. | 09 |
| 10 | Invalid certificate entry | Server rejects redemption above service value; misleading derived amounts remain in the preview. | 10 |

## Findings and exact changes to make

### 1. Separate service redemption from additional sales

Evidence: steps 2–3; `internal/views/clients.templ:59–67`.

«Дополнительно» contains creams, oil, certificate sales, their destination, and «Оплачено сертификатом». Redemption is part of the service value, while sales bring new money into the kassa. A user has to infer this distinction from instructions elsewhere.

Use separate sections: «Услуги», optional «Оплата сертификатом» associated with services, «Дополнительные продажи», «Внесено в кассу за услуги», and the read-only «Результат». Label amounts with euro units and show required/optional status. Retain the existing rule that services include the redeemed amount and service contributions exclude extras.

Keep short field constraints visible. Put longer examples and explanations in a separate «Как заполнить» disclosure outside the input cards. Do not use hover tooltips or placeholders as essential instructions. Associate essential hints/errors with their fields for assistive technology.

### 2. Stop presenting invalid previews as financial results

Evidence: step 10; `internal/views/clients.templ:80–87,101`.

With services €10 and redemption €20, the server correctly refuses saving and highlights redemption. The preview still displays retained cash −€10 and «Салон должен вам €15». These are invalid-input results, not meaningful money figures.

Show the constraint next to the redemption field, preserve entered values, and move focus/scroll to the first invalid field. Replace affected preview values with a clearly labelled incomplete calculation until inputs are valid. Keep a summary error, but do not rely on a top-of-form message and red border alone. This changes presentation, not authoritative money calculation or server validation.

### 3. Give dashboards a clear analytical order

Evidence: steps 1, 4, 8–9; `internal/web/admin_revenue.go:200`, `internal/views/admin_revenue.templ:54`, `internal/views/my.templ:43–50`.

Day/month summaries currently show client counts and individual product categories before total revenue. Revenue, Self, Salon, product sales, certificate redemption and kassa contributions have equal visual weight. The monthly view repeats categories in tiles, a category card and employee cards.

Use this order: selected period → primary totals → grouped breakdown → employee comparison → underlying records. Start with revenue, Self, Salon and client count. Group service/product revenue separately from period cash/card contributions and certificate activity. Keep low-priority category detail expandable; show meaningful zero totals without filling the main screen with zero product tiles. On phones, employee summaries lead with the same comparable metrics and expand to details; desktop tables use consistent columns. Keep current own balance outside selected-month totals, with its own date, as the personal view already does.

### 4. Distinguish contributions, current balances and settlement estimates

Evidence: steps 4–6, 9; `internal/views/settlement.templ:84–118`.

Revenue dashboards label report contributions «Касса — наличные» and «Касса — карта салона», the same names used for current account balances on the Kassa screen. Use a section heading such as «Внесено в кассу за месяц» or «Внесено в кассу за день» above contribution rows.

The open-month example prominently shows a projected €1,020 person balance, while the current realized balance is €20 in a smaller line beside a generic payout action. Separate «Предварительный итог месяца» from «Текущий остаток». Attach payout/deposit actions to the current balance; make their suggested amount visible, preserving partial-amount editing and existing stale-context safeguards. On closed months, show frozen month-end figures separately from subsequent payments/current balances.

### 5. Make guidance relevant to the active task

Evidence: steps 6–7; `internal/views/movements.templ:135,300`.

The deposit sheet displays «Для расхода обязателен» beneath Comment. Use «Комментарий (необязательно)» for deposit/transfer and «Комментарий (обязательно)» for expense, with only relevant help. Money units should be apparent before entry.

«Проверьте сертификаты» flags a problem but does not explain the next step. Show that a negative outstanding amount requires checking opening certificates, recorded sales and redemptions; place the explanation in a distinct warning/help block with a relevant action. Preserve the warning until corrected; do not infer or invent opening values.

## Implementation and acceptance

Priority: input grouping and invalid preview → dashboard metric grouping/labels → settlement current-versus-projected presentation → contextual movement/certificate guidance. Include the previously recorded desktop badge visibility fix.

Use the existing Go/templ/Datastar/daisyUI components. Preserve routes, access boundaries, int64 money calculations, idempotent writes, drafts, and frozen legacy reports/snapshots. No new cloud resources, data migration or production reset is required for this presentation work.

Test seams: HTTP-rendered form labels, field errors and dashboard sections; browser flows for optional sections, retained drafts, valid/invalid previews and save confirmation; existing domain/store contracts protect amounts. Verify service-only, extras-only, mixed cash/card contribution, certificate sale and redemption, invalid values, empty/closed months, current balances differing from frozen/estimated balances, and employee/admin visibility.

At 375 × 812 in both themes: no horizontal scroll; inputs ≥16 px; targets ≥44 px; help is separate and optional examples do not obscure entry; keyboard and save bar leave active fields accessible; invalid inputs do not appear as valid financial results. On real iPhone, confirm numeric keyboard, Next order and completion without verbal instruction. Do not claim user convenience solely from screenshot checks.

### Implementation verification

- Services/redemption, additional sales, contributions, results and optional «Как заполнить» are separate. Fields show euros, accessible constraints and errors. Existing draft tokens, versions, no-JS posts and server money rules are preserved.
- Invalid raw input, redemption exceeding services, and excessive service contributions hide derived results. Live rejection focuses redemption; after correction `aria-invalid` changes from `true` to `false`, the validation summary disappears, and valid results return. A corrected mixed service/sale/redemption record saved successfully with the original €120/€50/€70/€20 figures.
- Own-day, admin-day, own-month and admin-month summaries share the primary metric order and grouped period contributions. Employee details expand; desktop tables retain comparable columns and history links. Regression tests preserve custom/renamed legacy category labels that collide with metric labels, including frozen months and unavailable old snapshot statistics.
- Settlement displays current €20 and «Выплатить €20» before the separately labelled €1,020 projected month result. Current balances are dated; partial payment and stale-context safeguards remain intact. Desktop Profile at 1280 × 812 has a 1044 px document, but its sidebar badge remains fully visible at y=744–796.
- Movement comment labels follow the selected operation; negative certificate warnings explain opening/sale/redemption checks and link to records.
- Fresh `make ci` passed: generated files, workflow/Go lint, six Bun draft tests, all Go race tests, vulnerability/security checks and app build. Independent code, money and mobile reviews found no remaining actionable issues after fixes.

Dark interactions were verified on the live local app. Light client/month/settlement layouts were checked at 375 × 812 using captured rendered DOM with the app's explicit `aesthetic` theme and scripts removed; this checks layout, not light-mode interaction. Real-phone keyboard and screen-reader testing remain onboarding follow-ups. No production writes, migration or deployment performed.

After evidence: [client form](after/01-client-form-dark.png), [valid saved example](after/02-valid-client-dark.png), [dark month](after/03-month-dark.png), [light month](after/04-month-light.png), [light client](after/05-client-light.png), [dark settlement](after/06-settlement-dark.png), [desktop badge](after/07-desktop-badge.png), [corrected validation](after/08-corrected-validation.png), [light settlement](after/09-settlement-light.png), [dashboard preview](after/10-dashboard-preview.png).

## Limits

Dark-mode rendering and Anna's admin/personal screens were inspected with one populated local example. Client fields use 16 px text and decimal input mode; measured own-day, own-month and validation screens have scroll width 375 px. Existing source uses native dialogs, visible labels and mobile cards rather than desktop tables. No hover tooltips were found in the inspected templates.

Light theme, real phone keyboard, screen reader, four-person comparison, closed snapshots, long names and larger datasets still need verification. History/filter, staff/terms, login/password and legacy correction flows received source inspection only. Trends and exports remain unfinished M9 work, so analytical coverage there is not complete.

## Screenshots

### 1. Own day, empty


### 2. Client entry


### 3. Extras and certificate redemption


### 4. Monthly revenue


### 5. Settlement


### 6. Kassa


### 7. Movement sheet


### 8. Admin day


### 9. Own month


### 10. Invalid redemption

