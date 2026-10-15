# Client entries — agreed change, 2026-10-13

User requests one record per employee/client/day rather than a daily total. Follow-up answers: record services/extra sales and actual cash/card contributions, no customer payment-method split. Employee chooses cash or salon card for all extras. Optional service certificate redemption decreases outstanding certificates and counts staff share without new money.

## Rules and preservation
- Client labels numbered per employee/day; server creation time in Brussels, immutable on edit. No customer name/service descriptions.
- Services field is full service value including certificate-paid part; helper makes inclusion explicit. Redemption <= services. Cash/card service contributions <= services minus redemption.
- Cream/oil/certificate sale full amount deposited into chosen kassa account. No Self on extras. Certificate sales are liability until redeemed, cream/oil salon income now. Existing daily reports/closed snapshots retain old frozen amounts/rules.
- New record derives canonical report revenue/payments/cash-to-kassa through existing domain.ComputeReport; domain is authoritative. Service contribution fields exclude extras; totals show extras + manual services per account. Empty service contributions mean0, never guessed paid.
- Repeated create POST with same stable client token is idempotent; changed replay refused, no duplicate client/audit. Unique ordinal allocation transactional. Edit version check, month lock, owner/admin access, audit same transaction. No automatic financial retries.
- New-client drafts retain stable create token across reload/lost response until successful acknowledgement; per-owner/date/entry isolation.
- Legacy daily form retained only to inspect/correct existing legacy entries. Admin daily/month/person/history add client labels/time/count, services, extras, cash/card kassa totals. Closed monthly snapshots include new counts if closed after update; old snapshots do not invent count.
- Russian phone-first 375light/dark; simple form with optional extra fields and certificate redemption, preview totals, sticky save. Existing accounts/settings/test cutoff unchanged.

## Task1 Domain tracer
Add domain.ClientInput and NewClientReport(person,day,input,pct), using existing report formulas. Tests: services10000,cream2000 cash, serviceCash3000 at70% => total12000 Self7000 Salon5000 CashToKassa5000 BalanceDelta0; cream excludedSelf. Cash/card extra destinations and certificate sale/redemption, negative/overcontribution/unknownextras invalid. Property cash retained + delta=Self.

## Task2 Store and aggregation
Add SaveClientReport/ClientReport with token/number/input/version/timestamps, records in reports collection distinguish legacy/client. Atomic numbering/idempotency/audit and ownership/month lock. Stable query sorting and settlement hashes. Include oil label/catalog safely without mutating old categories. Extend summaries/frozen snapshot metadata for client counts and service/extra/kassa statistics, never duplicate ledger deposit entries. Memory+Firestore contract tests concurrent saves/replay/edit/balances/legacy coexistence/close-reopen.

## Task3 HTTP/form and read screens
Home is today's own client list/summary + add client form. Owner/admin edit client routes; legacy day form accessible separately. PlainPOST and Datastar, draft safe stable tokens, no custom JS outside draft.js. Admin day shows per-person day totals plus client cards, month/my/history counts/time and correct links. Admin/staff permissions all routes. Literal HTTP monetary assertions includingprepaid and no duplicatePOST, closedversionlostresponse. Legacy tests move only relevant old form GET references to legacy route.

## Task4 Verification and release
make ci, emulator race contracts, 375both themes/short save/long names, native/noJS, delayed/lost save, multiple client records sum, independent money/mobile/code review and all issues fixed. Docs/progress/system-design/money-rules updated. PR reviews/comments all read/resolved, exact-head CIpassed then squash/CIdeploy and prodreadyz. No live financial test rows.

Open questions: none. M9 was paused during this work; it later shipped adapted to this model (archive PR 34).

## Test database authorization (2026-10-13 follow-up)
User explicitly permits restructuring/optimizing test financial records, adding new attributes and removing obsolete ones, including recreation if necessary. Preserve accounts, settings and intended staff terms. Main Firestore only; other salon services/(default) stay out of scope. Current additive client metadata and counter fit existing indexes; no financial reset is required to implement this change. This permission also covers necessary controlled test-data cleanup before real use, with read/export/verification first.
