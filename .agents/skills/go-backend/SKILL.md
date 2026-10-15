---
name: go-backend
description: Go server conventions for the Kassa app — net/http ServeMux routes, datastar-go SSE responses, reading signals, templ rendering, middleware chain, sessions, config, layering (web → domain → store), server timeouts, error handling and lint gates. Use when writing or reviewing Go in cmd/ or internal/.
---

# Go backend (Go 1.27, datastar-go v1.2.2, templ v0.3.1020)

Adapted from `~/Documents/Projects/datastar-webapp` (`.agents/skills/go-backend.md`, `docs/GO.md`).

## Layers
| Layer | Package | May import | Never |
|---|---|---|---|
| Presentation | `internal/web`, `internal/views` | domain, store, export | Firestore client, formulas |
| Domain | `internal/domain` | stdlib only | time.Now, I/O, HTTP, store |
| Persistence | `internal/store` | domain | HTTP, templ |
| Export | `internal/export` | domain | store |

## Routing
```go
mux := http.NewServeMux()
mux.HandleFunc("GET /{$}", a.signedIn(a.handleHome))               // handlers get the signed-in store.User
mux.HandleFunc("POST /reports/{date}", a.signedIn(a.handleSaveReport))
mux.HandleFunc("GET /admin/month", a.adminOnly(a.handleMonth))     // 403 «Нет доступа» for staff
mux.HandleFunc("GET /readyz", a.handleReady) // Cloud Run reserves /healthz
handler := handlers.RecoveryMiddleware(handlers.SecurityHeadersMiddleware(true, handlers.LoggingMiddleware(mux)))
```
- Path values are read with `r.PathValue("date")` and validated (`2006-01-02`) before use.
- **Every non-GET request** goes through the `Origin` / `Sec-Fetch-Site` check middleware.

## Datastar responses
```go
var in saveReportSignals
if err := datastar.ReadSignals(r, &in); err != nil { a.badRequest(w, r, err); return }
res, err := a.store.SaveReport(r.Context(), actor, in.toInput())
sse := datastar.NewSSE(w, r)
switch {
case errors.Is(err, store.ErrVersionConflict):
    _ = sse.MarshalAndPatchSignals(map[string]any{"toastError": "Запись изменена другим пользователем — обновите страницу"})
case err != nil:
    a.log.ErrorContext(r.Context(), "save report", "err", err)
    _ = sse.MarshalAndPatchSignals(map[string]any{"toastError": "Не удалось сохранить, попробуйте ещё раз"})
default:
    _ = sse.PatchElementTempl(views.DailyFormResult(res))
    _ = sse.MarshalAndPatchSignals(map[string]any{"toast": "Сохранено ✓", "version": res.Version})
}
```
- **Dual mode:** if `!datastar.IsDatastarRequest(r)`, redirect (303). The login form must work without JS.
- **Limit request bodies:** `http.MaxBytesReader(w, r.Body, 64<<10)`.
- **SSE streams** must return when `r.Context().Done()` fires. This app rarely needs long-lived streams.

## Config (env)
- `PORT`.
- `INCOME_BASE_URL` (for example `https://<app-host>`): decides Secure cookies and the allowed Origin.
- `INCOME_FIRESTORE_PROJECT`, `INCOME_FIRESTORE_DATABASE`: if unset, the app uses the memory store.
- `INCOME_SESSION_KEY`: at least 32 bytes, from Secret Manager.
- `INCOME_TZ` (default `Europe/Brussels`).
- `INCOME_START_MONTH` (default `2026-11`): the first month the app accepts. The main production service uses `2026-10`: October 2026 is the first real month (decision 2026-10-14). There is no trial service.

Load and validate config once in `cmd/web/main.go` and fail fast. Keep a local `.env.web` (gitignored) for development.

## Sessions and auth
- **Cookie:** HMAC-signed (copy the `signer` from datastar-webapp `internal/web/auth.go`). Payload `{uid, sv, exp}`. HttpOnly, SameSite=Lax, and Secure when the base URL is https.
- **Passwords:** bcrypt (cost 12). Unknown users get a dummy hash compare (datastar-webapp `internal/auth/accounts.go`).
- **Every request:** load the user, check `active` and `sessionVersion`.
- **Roles:** the `signedIn` / `adminOnly` wrappers (`internal/web/session.go`), plus an ownership check inside handlers (`uid == session.uid` unless admin). `signedIn` also sends a person with a temporary password to `/profile` and extends the session once a day.
- **Forms** (`internal/web/forms.go`): plain `<form method="post">` that works without JS, enhanced with `data-on:submit="@post(url, {contentType: 'form'})"`. On an error, `formFailed` patches the form back by its id (Datastar) or re-renders the page with 422 (plain post). On success, `navigate` sets the `redirectUrl` signal (Datastar) or answers 303. The CSP blocks Datastar's own script-based redirect. Mutations use POST, because HTML forms can't send PUT/DELETE.

## Server
```go
srv := &http.Server{Addr: ":" + port, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 1 << 20}
```
Graceful shutdown on SIGTERM within 8 s (Cloud Run gives 10 s).

## Errors and logging
- Wrap errors with `fmt.Errorf("save report %s: %w", id, err)`. Handle each error once: log it *or* return it.
- Log with `log/slog` JSON and `*Context` methods. Never log passwords, cookies or full request bodies.
- Users see Russian messages. Internal error text never reaches the UI.

## Gates
`make lint`: `go vet`, `staticcheck`, `golangci-lint` (errcheck, gosec, bodyclose, unused, nolintlint with explanation), `gofmt`, `templ fmt`. Zero warnings. If you ignore an error deliberately, write `_ =`.
For deeper Go guidance, see the `golang-*` skills (error handling, security, testing, lint, modernize, design patterns, concurrency).
