# Aesthetic Lab — Касса

Internal phone-first web app for the Aesthetic Lab salon: per-client revenue entries, the salon kassa, staff balances and the monthly settlement. It replaces the «Aesthetic Lab Finance» Google Sheet. Live at **https://kassa.aestheticlab.be** (from M1).

Go 1.27 · templ · Datastar · Tailwind 4 + daisyUI 5 · Firestore · Cloud Run.

```bash
make dev-october                 # October testing, http://localhost:8080
```

Sign in as `anna` with the temporary password printed at startup, then change it. October terms are ready. This mode uses memory only and resets data and login on every restart; it needs neither `.env.web` nor the Firestore emulator.

```bash
make dev                        # Emulator + hot reload; keeps data while emulator runs
make dev-memory DEV_START_MONTH=2026-08  # Include August in local testing
make dev-october DEV_PORT=8090   # Use another local port
make help                       # All commands
make ci                         # Full quality gate
```

`make dev` defaults to October and uses the emulator regardless of `.env.web`; on first use, it prints Anna's temporary password. Staff and effective-dated terms can be configured in the app. `make web-dev` / `make web` use `.env.web` as configured; without it, the application’s normal start-month default is November. Restart a running process after changing its environment.

- **What and why:** [docs/technical-solution.md](docs/technical-solution.md)
- **How it works:** [docs/system-design.md](docs/system-design.md)
- **Build order and status:** [docs/implementation-plan.md](docs/implementation-plan.md), [docs/progress.md](docs/progress.md)
- **Working on it with AI agents:** [AGENTS.md](AGENTS.md), [docs/AI_AGENTS.md](docs/AI_AGENTS.md)
