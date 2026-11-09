# Security policy

Aesthetic Lab Kassa is an internal app for one salon. Only the latest
commit on `main` (the one deployed to production) is supported.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through
[GitHub private vulnerability reporting](https://github.com/lieranderl/aestheticlab-income-app/security/advisories/new)
(Security → Report a vulnerability).

Include what you found, how to reproduce it and the impact you expect.
You should get a reply within a week. Please give us a reasonable time to
fix the issue before you disclose it.

## Scope

In scope: this repository's code, its GitHub Actions workflows and the
deployed app. Out of scope: other services in the same cloud project,
denial of service and social engineering.

## What the repository already does

- Secret scanning with push protection, plus gitleaks on the whole history in CI.
- Dependabot alerts and security updates; weekly grouped version updates.
- CodeQL code scanning; `govulncheck` and `gosec` in `make ci`.
- Every GitHub Action pinned to a full commit SHA (enforced), workflows checked by zizmor.
- `main` changes only through squash-merged pull requests with a green `CI passed` check,
  resolved review threads and no high-severity CodeQL alerts.
- Keyless deploys (Workload Identity Federation) from `main` only, through the `production` environment.
