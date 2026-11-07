# Booking sync from Google Calendar — proposal, 2026-11-07

Status: **implemented 2026-11-07** (steps 1–5 in code; step 6 needs the owner to share the calendars and approve setting `BOOKING_CALENDARS`, see MAINTAINING.md → Booking calendars). Revised after four Codex reviews (2026-11-07); ready for implementation. Owner review needed before any code.

## Why
Bookings live in Timely, which has no usable API (Zapier is not enabled: too expensive). Timely two-way syncs every booking into Google Calendar, one calendar per staff member, owned by the salon Google account and shared with each person. The kassa can read those calendars to:
1. **Reconcile:** booked vs linked clients per person per day, so a forgotten entry shows before month close.
2. **Pre-fill:** start a new client entry from a booking with the booked service value as a suggestion.
3. **Stats (later):** booked value vs entered revenue, occupancy.

Building our own booking system was assessed and rejected for now: it is customer-facing (public booking, reminders, deposits, uptime), needs client personal data under GDPR, and roughly doubles the codebase. Revisit only if this integration proves insufficient.

## What the calendars contain (observed 2026-11-07)
Per-staff calendars (in fixtures: «Maria Aesthetic», «Olga Aesthetic», «Vera Aesthetic», «Anna Aesthetic»). Events: Timely bookings, Timely «Busy Time» blocks, personal/manual events.

- Timely booking description line 1: `<client> - <service>[ + <service>…] Price: €<amount> with <staff> (<Status>)`. Title = client name only. Lines 2+ = phone, SMS, email, location. **Never stored or logged.**
- Status is always `Confirmed` (owner, 2026-11-07).
- **Cancelled booking = event deleted** from the calendar.
- No hidden fields: no booking ID in extended properties. The Google event ID (unique per calendar) is the only stable key.

## Event classification (pure domain function)
`ClassifyEvent(summary, description string) EventClass` (in `internal/domain/booking.go`) reads only line 1 of the description, never later lines. The title is **compared, never returned**: line 1 must start with `<title> - `, exactly that prefix is cut, and a leftover ` - ` is Unreadable, so no part of a client name reaches a service. A title edited in Google Calendar therefore makes the booking Unreadable (a visible warning). Prices are read only in Timely's format (`€30.00`, `€45`, `€1,250.00`); anything else is Unreadable.

| Outcome | Rule | Effect |
|---|---|---|
| **Booking** | line 1 has the Timely signature (` - `, ` with `, trailing `(<Status>)`) and status is `Confirmed`. Price parsed to cents via the existing money parser; missing price → `PriceKnown=false`, still a booking. | counted, listed, linkable |
| **Unrelated** | no Timely signature (busy blocks: title `Busy Time with …`; personal events) | ignored |
| **Unreadable** | Timely signature (or `Price:`) present but the line can't be parsed: unknown status, malformed price, missing services | not counted as a booking; **shown as a data-quality warning** «Не удалось прочитать событий: 1» on the admin day and the person's home; logs carry only counts and event IDs, never event text |

So a future Timely format or status change surfaces as a warning instead of silently changing counts.

## Calendar reads
**Stateless window reads, no sync state.** When a screen needs bookings, call `events.list` per mapped calendar:
- `singleEvents=true`, `orderBy=startTime`, no `showDeleted`: a deleted (cancelled) booking is simply absent.
- **Day boundaries in `Europe/Brussels`**: `timeMin` = local midnight of the day, `timeMax` = local midnight of the next day, computed with the Brussels zone so DST days (23/25 h) are correct. The API returns events *overlapping* the window, so the kassa then keeps only events whose **local start date** is the day. A booking belongs to the date it starts on.
- **Follow every `nextPageToken`.** If any page fails, that calendar's result for the window is **unavailable** as a whole, never partial.
- Timeout ~3 s per calendar. Unavailable → screens render without bookings and show «Записи недоступны». Never blocks money entry.
- No sync tokens, watch channels, scheduler or Firestore copy of bookings → scale to zero unaffected, nothing personal persisted. Volume: ~10–25 bookings/day, 4 calendars.

**Access:** share each staff calendar (not «Aesthetic Lab», birthdays, holidays) with the kassa Cloud Run runtime service account, «See all event details». Scope `calendar.readonly` via ADC. The kassa never writes to Calendar. The bot runs in the same GCP project, but the kassa uses **its own** runtime service account, never the bot's.

**Config:** `BOOKING_CALENDARS=<calendarId>|<uid>;…` env var on Cloud Run (not in the repo). Calendar → person; never parse names. Unset = feature off. A person has at most one calendar.

## Linking entries to bookings
A link says «this client entry is that booking». Money is never derived from it.

**Storage.** `bookingRef` (calendar ID + event ID; not personal) is an additive field in the client entry's `clientInput`. Uniqueness is enforced by a lock document `bookingLinks/{sha256(calendarId|eventId)}` = `{reportId, person, date, active}`. The report field and the lock are always written in the same transaction, with an audit row; a contract test checks they agree.

**One active entry per booking.** Creating or linking with a `bookingRef` whose lock is active for another non-voided entry is refused: «Эта запись уже внесена» with a link to that entry. Opening «Внести» twice and saving both → the second save is refused (two different creation tokens don't matter, the lock does). Concurrent saves: Firestore transaction contention lets exactly one win; tested on memory and emulator.

**Voiding releases the link.** `VoidClientReport` sets the lock `active=false` in the same transaction. The voided record keeps its `bookingRef` for history. The booking then shows as not linked and can be entered again.

**Linking a manual entry.** Someone who entered a client without starting from the booking can fix it:
- on an unlinked booking: «Связать с внесённым клиентом» → choose one of that person's unlinked entries on the same day;
- on a linked entry: «Отвязать».
Both are audited writes on the entry (version +1, version check), open month only, owner or admin. Closed month: read-only.

**Wording.** Unlinked bookings are labelled «Не связано», not «Не внесено»: an unlinked booking may have been entered manually. Admin day: «Записей: 6 · Клиентов: 5 · Не связано: 1».

**Bookings that move or disappear after linking.** The entry always stays on its own date (it records when the client was served); money never changes. Notes come from two extra, bounded reads on top of the day window:

- **Entry's day (origin).** The day's client entries are already loaded; their `bookingRef`s are the linked events (no new query or index). Each linked ref **absent from the day window** gets one `events.get`:
  - Booking with another local start date → «Запись перенесена на 12.11»;
  - 404/410, or `status: cancelled` → **confirmed deletion**: «Связанная запись удалена в календаре»;
  - event exists but is now Unrelated/Unreadable → «Связанная запись изменена и не читается»;
  - error or timeout → «Не удалось проверить запись». **Never** shown as deleted.
- **Booking's day (destination).** For the bookings returned by the day window, the server reads their lock documents by ID (`bookingLinks/{sha256(calendarId|eventId)}`, one batch get, ~25 docs max). An active lock whose date ≠ this day → the booking counts as linked, shown «Связано с клиентом от 07.11», not «Не связано».
- **Bounds:** at most 20 extra `events.get` per screen, in parallel within the same ~3 s budget; beyond that «Ещё N записей не проверено». Typical: 0–1.
- **Across months:** works the same; if the entry's month is closed, the note is shown but unlink is not offered (closed months are read-only).
- Time changes on the same day need nothing: the link follows the event ID.

## Server validation
Order for a create that carries a `bookingRef`:
1. **Identity and access** as today (owner or admin, person active). No month check yet.
2. **Replay first, without Calendar or month check.** If a record already exists for this creation token: identical payload (financial fields **and** `bookingRef`) → acknowledged as today; different → refused as a changed replay. Calendar is never contacted for a replay, and the month may have closed since (as in today's `SaveClientReport`, which acknowledges before `requireOpen`), so a lost response followed by a cancellation, reschedule, Calendar outage or month close still gets its acknowledgement.
3. Only for a **new** write: **open month** (`requireOpen`), then the calendar must be the one mapped to the **entry's person**. An employee can only use their own calendar; the admin can use the calendar of the person the entry is for. Any other calendar → refused (forbidden).
4. The server fetches the event (`events.get`) from that calendar. Not found, not a **Booking** outcome, or local start date ≠ entry date → refused. A forged event ID can't pass.
5. **The write transaction re-checks everything that can change while Calendar is being asked**, in today's `SaveClientReport` order: identity/access (person active, may submit), the token (a concurrent identical create in between → acknowledged, not duplicated; changed → refused), **month openness (`requireOpen`)**, entry version, then the booking lock. Only then does it write report, lock and audit.

Calendar calls happen outside the Firestore transaction (it may retry). The open-month check in step 3 is only a fast refusal before calling Calendar; the binding check is the one inside the transaction, so a month closed during the Calendar request still refuses the new write. Link and unlink follow the same rule: every precondition is re-checked inside their transaction.

**Link on an existing entry** follows the same pattern: identity/access; a repeated link to the ref the entry already has, with its lock active → acknowledged without Calendar or month check; otherwise open month, entry version check, steps 3–5.

**Unlink needs no Calendar.** It exists precisely for bookings that were deleted, moved or became unreadable, so it must not depend on the event: identity/access → entry version check → open month → in one transaction clear `bookingRef`, set the lock `active=false`, version +1, audit row. A repeated unlink of an entry that already has no ref → acknowledged.

If the calendar is unavailable for a **new** create, the save with the ref is refused: «Записи недоступны. Сохраните без привязки или попробуйте позже», with a «Сохранить без привязки» choice that drops the ref (the person decides; nothing is dropped silently). This refusal happens before any write, so no record exists for the token and dropping the ref is a fresh create, not a changed replay. The draft keeps all fields.

**Preserving the ref.**
- **Edit:** the edit form never changes `bookingRef`. The server ignores any submitted value on edit and keeps the stored one; only link/unlink change it.
- **Drafts:** new-entry drafts store `bookingRef` with the creation token, so reload or lost response restore it.
- **Replay:** `sameClient` (`internal/store/client_reports.go`, today financial fields only) also compares `bookingRef`. An unchanged replay is acknowledged; a replay with a different ref is refused like any changed replay.

## Money rules
The booked price is a **suggestion only**. It pre-fills the services field; the person confirms actual amounts; the server recomputes via `domain.ComputeReport` as today. Booked figures never enter the ledger, balances, settlement or close snapshots. Booked counts stay **live-only** for v1 (Codex agreed).

## Access
An employee sees only their own calendar's bookings and links only their own entries; the admin sees and links all.

## Screens (Russian, phone first)
- **Employee home:** «Записи сегодня»: time, services, «по записи €30,00», «Внести» (→ new client form pre-filled, with the ref) or «Связать с внесённым клиентом». Linked bookings show «✓ внесено». Unreadable-event warning when present.
- **Admin day:** per person «Записей · Клиентов · Не связано», list of unlinked bookings (time + services, no client names), moved/deleted-link notes, unreadable-event warning.
- **Admin month (later):** booked count/value vs entered count/revenue per person.

## Slices (TDD)
1. **Domain:** `ClassifyEvent` with fake fixtures: single/multi service, comma/dot amounts, missing price (Booking, price unknown), non-`Confirmed` status → Unreadable, malformed price → Unreadable, busy block and personal event → Unrelated, lines 2+ ignored. Brussels day window incl. DST days; start-date assignment of an overlapping event.
2. **Gateway:** `BookingSource` interface + memory fake (tests, e2e, dev); Google implementation tested with httptest: pagination across pages, a failing later page → unavailable, timeout → unavailable.
3. **Store:** `bookingRef` + `bookingLinks` lock. Contract tests (memory + emulator): one active entry per booking, concurrent saves (one wins), void releases the lock, relink after void, link/unlink version check and audit, closed month refused, replay with same/different ref, edit keeps the stored ref.
4. **HTTP, employee:** «Записи сегодня», «Внести» pre-fill, «Связать», «Отвязать», draft restores the ref. Validation tests: forged event ID, another employee's calendar, wrong date, calendar unavailable → refusal with «Сохранить без привязки». **Lost-response retries** of an identical create acknowledged with no Calendar call after the booking was deleted, rescheduled, Calendar is down, or **the month was closed** (fake asserts zero calls); changed replay still refused; repeated link acknowledged. **Month closes between Calendar validation and the write** (fake calendar closes the month mid-call): the new create and a new link are refused and nothing is written; an identical replay of an already saved entry is still acknowledged. **Unlink** during a Calendar outage and after the booking was deleted succeeds with zero Calendar calls; unlink in a closed month refused; stale version refused.
5. **HTTP, admin day:** counts, «Не связано», unreadable warning; access tests. Moved/deleted notes: move within the day, to the next day (both days' notes; destination counts it linked), to the next month with the origin month closed (no unlink offered), confirmed deletion (404 and `cancelled`), `events.get` error ≠ deletion, more than 20 linked refs absent → bounded with «Ещё N».
6. **Ops:** share 4 calendars with the runtime SA, set `BOOKING_CALENDARS`, verify read-only against the real calendars: per-person booking counts and times for a day match the Google Calendar screen (no client data copied into notes, logs or screenshots). No test calendar is mapped, so no fake booking ever reaches a real person's screen. Update `MAINTAINING.md`, `system-design.md`, `progress.md`, `money-rules` if needed.

## Prior art: Telegram bot (`telegrambot-aestheticlab`)
Python service that already reads these calendars with `calendar.readonly` via its own Cloud Run service account, using push watch channels + Cloud Scheduler renewal + sync tokens in its own Firestore. Confirms the service-account sharing approach works. **We do not reuse it or its Firestore:** separate service (out of bounds per kassa cloud-safety rules), no parsed prices, and push/scheduler would add always-on parts. Lessons taken: `singleEvents=true`; deleted events come back as `status: cancelled` stubs only in sync-token mode — avoided here by window reads.

## Open questions
Resolved 2026-11-07: status is only `Confirmed`; same GCP project as the bot (the kassa still uses its own service account); booked counts live-only for v1.

1. Do any Timely service names contain ` - ` (e.g. "Laser - Full Legs")? Owner doesn't know (2026-11-07). Kept safe: such bookings are Unreadable, so no client text can leak, but they would show a daily warning and not be counted. Check at go-live (step 6): if the unreadable count is above 0 for real bookings, switch to a list of the salon's service names.
2. Outside this plan: the bot forwards the full description (client phone and email) into a Telegram chat — owner to decide if that is intended.
