---
name: tdd
description: The red-green-refactor loop for this repo — pre-agreed public seams (domain functions, HTTP via httptest + memory store, store contract tests on the emulator), vertical slices, boundary-only fakes, capability-named tests, literal assertions from the spec/sheet. Use when building any behaviour or fixing a bug.
user-invocable: true
---

# TDD loop (Aesthetic Lab Kassa)

## 1. Agree the seam first
A **seam** is a public boundary where behaviour can be observed. Name it before creating a test file:

| Seam | Use for | How |
|---|---|---|
| **Domain** (`internal/domain`) | all money maths, validation, aggregation | plain exported functions, table-driven tests, property tests for invariants |
| **HTTP** (`internal/web`) | routing, auth, roles, form handling, SSE patches, rendered Russian text | `httptest` with the **memory store** and an injected clock; assert status, headers, and the patched HTML or signals |
| **Store contract** (`internal/store`) | transactions, versions, month lock, audit, void | the same test suite runs against memory and the Firestore emulator (`make test-store`) |
| **Mobile E2E** (Playwright, from M11) | layouts at 375 pt, flows that only taps can do | `bun run test:mobile` |

## 2. Vertical slices
One seam, one failing test, the minimum code to pass it, then repeat. The first slice is a tracer bullet: one happy path end to end. Never write a batch of tests before the code.

## 3. Red → green → refactor
1. **Red:** write one failing test named for a capability, for example `TestAnnaKeepsCashAndSalonOwesHerTheRest`. Run it and check it fails *for the expected reason*.
2. **Green:** write the least code that makes it pass.
3. **Refactor:** clean up while green, then run `make lint`.

## 4. Fakes only at the boundaries
Fakes are allowed only at the edges of the app:
- the clock (`WithClock`);
- randomness (`io.Reader`);
- Firestore (memory store, or the emulator);
- the network.

Never mock `internal/domain` or other internal packages.

## 5. Assertions
- **Use literal values taken from the spec, `docs/`, or the Google Sheet.** Never compute the expected value with the same formula as the production code.
- **Money acceptance cases** live in the `money-rules` skill and must stay green.
- **Test names** describe behaviour a person would recognise.
- **Never reach into private fields.**

## 6. Before you say "done"
- `make test` passes (it includes `go test -race ./...`), and so does `make lint`.
- New money paths: the invariant property test covers them.
- New screens: checked at 375 pt in light and dark themes (`mobile-ui` skill).
