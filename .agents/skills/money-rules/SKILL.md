---
name: money-rules
description: The salon's money model — daily report maths («Себе», «Салону», «Оставьте в кассе»), certificates, kassa and person balances, the ledger, month close, salary, dividends and settlement, with invariants and acceptance figures from the Google Sheet. Load BEFORE writing or reviewing anything in internal/domain, any handler or template that shows or computes money, store transactions on reports/ledger/months, or the tests for them.
---

# Money rules (Aesthetic Lab Kassa)

The source of truth is `docs/technical-solution.md` §4–5 and `docs/system-design.md` §2–4. This skill is the condensed, checkable version. If it ever disagrees with those docs, the docs win; fix this file.

## Hard rules
1. **Money is `int64` cents.** Never `float64`, not even for a percentage step: `selfPct` is basis points (5000 = 50%). Round with `(selfBase*selfPct + 5000) / 10000` (half up, non-negative values only).
2. **The server computes and saves every derived field.** Client signals are a preview only.
3. **History is frozen.** `selfPct`, `selfBase`, the category flags and every derived value are saved on the report at save time. Changing a setting later never rewrites saved reports.
4. **Money is never deleted.** A wrong ledger row gets `status=voided` with a reason. Only `confirmed` rows count.
5. **Every write path appends an `audit` doc in the same Firestore transaction.**
6. **Sign convention for a person's balance:** positive means «Салон должен вам» (the salon owes the person); negative means «Вы должны салону» (the person owes the salon).

## Current client entries (2026-10-13)
New web records are one per employee/client/day. Only services count toward Self. Cream/oil/certificate sales go100% to the chosen cash/card kassa. Services includes certificate redemption, which reduces outstanding and earns staff share without another cash/card deposit. Service contributions are actual cash/card amounts excluding extras, default0; their sum <= services minus redemption. `domain.NewClientReport` supplies fixed frozen flags to the existing calculation. No customer payment-method split or customer names.
Numbering is per employee/day, server creation time immutable, token replay idempotent, edit version/owner/month locks and transaction audit mandatory. A client entered by mistake is removed by voiding (`voided`, `voidReason`; owner or admin, open month, current version, audit in the same transaction); `readReports` and the terms check skip voided records, numbers are never reused and replaying the create token cannot restore it. Do not newly create legacy daily web records. Saved old daily values/snapshots stay frozen; old snapshot statistics absent (`statsVersion=0`) are unavailable, never invented0. Actual cash/card monthly contributions and per-person category sales freeze at close (`statsVersion=1`).

Acceptance: services10000,cream2000 cash,serviceCash3000 at7000bps => Total12000,Self7000,Salon5000,Income5000,cashDeposit5000,delta0,held7000. Certificate sale10000 tocard =>card10000,outstanding+10000,Income0,Self0. Redeem service10000 with certificatePaid10000 at7000bps =>cash/card0,outstanding−10000,delta+7000,Income3000. Property: serviceValue−prepaid−serviceCash−serviceCard+balanceDelta=Self.

## Legacy daily report
```
total            = Σ revenue[c]
selfBase         = Σ revenue[c] where c.countsTowardSelf     (certificate sale ⇒ false)
selfAmount       = override ?? round(selfBase × selfPct)     0 ≤ selfAmount ≤ total
salonShare       = total − selfAmount                        («Салону»)
certificateSales = Σ revenue[c] where c.isCertificateSale
salonIncome      = salonShare − certificateSales             (income only, for profit)
cardIn           = Σ payments[m] where m.destination == kassa_card
prepaid          = Σ payments[m] where m.destination == prepaid   (paid with a certificate)
cash             = Σ payments[m] where m.destination == cash
check            : cardIn + prepaid + cash == total          (else reject the save)
suggestedToKassa = clamp(salonShare − cardIn − prepaid, 0, cash)
transferToCard   : 0 ≤ transferToCard ≤ cash                 («Переводом на карту салона»; empty = 0)
cashToKassa      : 0 ≤ cashToKassa ≤ cash − transferToCard   («Наличными в кассу»; empty = max(0, suggestedToKassa − transferToCard))
kassaCredit      = cardIn + prepaid + cashToKassa + transferToCard
balanceDelta     = kassaCredit − salonShare
```
**Invariant (property-test it):** `(cash − cashToKassa − transferToCard) + balanceDelta == selfAmount`, for every valid input.

## Balances
- **Person:**
  - opening
  - \+ Σ `balanceDelta` from their reports
  - \+ deposits (person → kassa)
  - \+ expenses they paid themselves (person → outside)
  - \+ salary and «Себе» bonus accruals
  - \+ dividend accruals
  - − payouts (kassa → person)
- **`kassa_card`:** opening + Σ `cardIn` + Σ `transferToCard` + ledger rows in − ledger rows out.
- **`kassa_cash`:** opening + Σ `cashToKassa` + ledger rows in − ledger rows out.
- **Transfers** (cash ↔ card) move money between the two accounts and change neither the kassa total nor any person.
- **Adjustments** (from «Пересчёт кассы») change one account only.
- **Outstanding certificates** = opening outstanding (sold before the start month, not yet used) + Σ certificate sales − Σ prepaid redemptions. Below zero → warning «Проверьте сертификаты».
- **Starting point:** the snapshot of the latest closed month. Then apply the open months. Ignore `pending` and `voided` rows.

## Monthly result and settlement
```
salonIncome(m) = Σ report.salonIncome
expenses(m)    = Σ confirmed expense rows (from kassa AND paid by persons)
salaries(m)    = Σ terms(p, m).salaryCents                           (Anna 120000)
selfBase[p]    = Σ report.selfBase of p's reports in m
selfBonus[p]   = selfBase[p] > terms.highSelfFromCents > 0              (strictly above; the whole month)
                   ? round(bonusBase[p] × (terms.highSelfPct − terms.selfPct)) : 0   (one rounding, half up)
bonusBase[p]   = selfBase of p's reports without a manual «Себе» override (an overridden day keeps what it was given)
profit(m)      = salonIncome − expenses − salaries − Σ selfBonus
dividendPool   = max(0, profit)                                      (no loss carry-forward)
dividend[p]    = largest-remainder split of dividendPool by terms(p, m).dividendWeight   (Σ == pool exactly)
terms(p, m)    = users/{p}/terms doc with the greatest ID ≤ m      (effective-dated; never today's values;
                 a new terms doc can't start where p already has reports in a month it would cover)
earned[p]      = Σ selfAmount + selfBonus[p] + salary[p] + dividend[p]
```
- **Start month is `INCOME_START_MONTH`: production uses `2026-10`.** October 2026 is the first real month (decision 2026-10-14), run in parallel with the sheet; the code default `2026-11` applies only where the variable is unset. Nothing is dated earlier, neither reports nor ledger rows. Tests inject the clock and the start month; they never depend on today's date. Opening rows (kassa cash and card, each person, outstanding certificates) are dated 2026-10-01. Person openings are the September settlement balances and exclude every payout dated on or after 1 October; those are recorded as app movements.
- **A closed month is locked for everyone, the admin included.** Reports and ledger rows can only be dated in an open month.
- **Close:**
  - Only if the previous month is closed (except the start month `S`), and no `pending` movement is dated in the month.
  - Closing balances = previous snapshot (or openings) + this month's rows only (`month == m`). Later open months are never included.
  - Write salary, `self_bonus` and dividend accrual rows, dated the last day of the month.
  - Write the snapshot (result + closing balances).
  - Set `status = closed`.
- **Reopen:**
  - Only the most recently closed month can be reopened.
  - Void the month's accruals.
  - Drop the snapshot.
  - No snapshot is ever stale, so balance reads trust the latest one.
- **Suggested action per person:**
  - balance > 0 → «Выплатить €X»;
  - balance < 0 → «Внести в кассу €X»;
  - otherwise nothing to do.
  - «Перенести» leaves the balance as it is.

## Acceptance figures (must be unit tests)
| Case | Expected |
|---|---|
| Spec day, a 50% master: services 600, creams 50, certificates (sale) 100, card 600, cash 150 | total 750, selfBase 650, «Себе» 325, «Салону» 425, suggestedToKassa 0, balanceDelta +175; she keeps 150 cash → 150 + 175 = 325 |
| Certificate sold by card, 100 | «Себе» 0, «Салону» 100, cardIn 100, balanceDelta 0, salonIncome 0 |
| Service 100 paid by certificate, master 70% | «Себе» 70, «Салону» 30, prepaid 100, balanceDelta +70, salonIncome 30 |
| Cash-only day 200, master 70% | «Салону» 60, suggestedToKassa 60, balanceDelta 0 if they leave 60 |
| Same day, €20 «Переводом на карту салона» | cashToKassa 40 (suggestion − transfer), kassa_card +20, kassa_cash +40, balanceDelta 0 |
| Reference month (fictional, laid out like the sheet's «2 Debit Credit» tab) | profit 1788.85 → dividends 596.28 / 596.28 / 596.29 (one gets the extra cent); balances Maria +96.93, Olga +333.28, Vera −88.50, Anna +489.78 (±1 cent from the dividend cent) |
| Loss month (profit < 0) | dividends 0 for everyone |
| Reference month, Maria 65% or 75% above €6 000 (base 7421) | bonus 742.10 accrued, profit 1788.85 − 742.10 = 1046.75 → dividends 348.92 / 348.92 / 348.91 |
| Threshold €5 000, 50% → 70%: base exactly 5000 / 5000.01 / 6000 | bonus 0 / 1000.00 / 1200.00 |
| Anna's salary changed to 1200 from 2026-12, then November reopened and closed again | November salary accrual still 1000 |
| Outstanding certificates: opening 500, sale 100, redemption 150 | 450 |
| Redemption 50 with opening 0 and no sales | −50 → «Проверьте сертификаты» warning |

The reference month's inputs per person (revenue, salon share due, returned to salon, staff-paid expenses, manager salary, already paid) are fictional, chosen to exercise the same rules as the real sheet: one left-over dividend cent, two with the bonus, and one person owing the salon:

| Person | Revenue | Salon share due | Returned | Staff-paid expenses | Salary | Already paid |
|---|---|---|---|---|---|---|
| Maria | 7421.00 | 2597.35 | 2050.00 | 48.00 | 0 | 0 |
| Olga | 5680.00 | 1988.00 | 1510.00 | 215.00 | 0 | 0 |
| Vera | 810.00 | 486.00 | 810.00 | 0 | 0 | 412.50 |
| Anna | 1430.00 | 786.50 | 1430.00 | 0 | 1200.00 | 1950.00 |

Salon expense rows total 2869.00. Salary 1200 brings all expenses to 4069.00. Salon share 5857.85 − 4069.00 = profit 1788.85.

## Where the code lives
- `internal/domain/money.go`: parse and format `€1 234,50`.
- `internal/domain/report.go`, `balance.go`, `settlement.go`, `aggregate.go`: pure functions. Values come in, values go out. No time, store or HTTP.
- `internal/store`: transactions only. It calls domain functions and never re-implements a formula.

## Review checklist
- [ ] No `float` anywhere near money; no `math.Round` on cents.
- [ ] Every new derived field is saved on the report.
- [ ] The invariant test and the acceptance table still pass.
- [ ] No ledger row is deleted; void instead.
- [ ] An audit row is written in the same transaction.
- [ ] Employee-facing handlers only read or write `userId == session.uid`.
- [ ] UI wording matches the glossary in the `mobile-ui` skill.
