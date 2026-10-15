---
name: gcp-ops
description: Operating the Kassa app on Google Cloud (project <project>) — what belongs to this app vs other salon services, safe read-only checks, deploy/rollback, Cloud Run, Firestore backups/restore, DNS record for kassa.aestheticlab.be, secrets, logs. Use before running any gcloud/gh command that inspects or changes cloud resources.
---

# GCP operations

Project **`<project>`**, region **`europe-west1`**. Use the gcloud CLI with `--project <project>` on every command: your default gcloud project may be a different one.

## Ownership: what you may change
| Resource | Owner | Allowed |
|---|---|---|
| Cloud Run `aestheticlab-income` | **this app** | deploy, update traffic, read logs |
| Firestore database `aestheticlab-income` | **this app** | indexes, backups, restore *into a new database* |
| Service accounts `aestheticlab-income-runtime@`, `aestheticlab-income-deployer@` | **this app** | yes |
| Secret `INCOME_SESSION_KEY` | **this app** | add versions |
| Bucket `<project>-income-backups`, Scheduler job `income-firestore-export` | **this app** | yes |
| Artifact Registry `aestheticlab-income` | **this app** | push images (the deployer has writer here only) |
| Artifact Registry `aestheticlab` | website | **read-only** |
| Service account `aestheticlab-income-backup@` | **this app** | yes |
| Budget «Kassa (app=kassa)», alert «Kassa production 5xx responses» | **this app** | yes |
| WIF pool `aestheticlab-github` | shared | add *our* provider only |
| Cloud DNS zone `aestheticlab`, record `kassa.aestheticlab.be.` | shared zone | **only** the `kassa` record |
| Cloud Run `aestheticlab-web`, `stage-aestheticlab-web`, `aestheticlab-calendar-telegram*` | other apps | **read-only** |
| Firestore `(default)` | Telegram bot | **never touch** |

## Rules
- **Production only:** `aestheticlab-income` is the only service/database. Trial resources and tools were retired after exports; do not recreate them. `INCOME_START_MONTH=2026-10`: **October 2026 is the first real month** (decision 2026-10-14), run in parallel with the sheet. All production data is real; never delete financial records. Openings are dated 2026-10-01 (MAINTAINING.md → Real October).
- **Look before you change.** Run the `describe` / `list` command first and show the result.
- **Ask the user before anything destructive or outward-facing:** deleting, changing IAM, changing DNS, shifting traffic outside the CI flow, deleting or disabling secrets, restoring a backup.
- **Never** run `gcloud firestore databases delete`, `gcloud run services delete`, `gcloud dns record-sets delete` (except the `kassa` record), or `gcloud projects …` IAM edits on roles beyond this app's service accounts.
- **Deploys go through GitHub Actions** (`.github/workflows/web.yml`). Manual `gcloud run deploy` is only for the very first bootstrap in M1, or a rollback.

## Everyday commands (read-only)
```bash
gcloud run services describe aestheticlab-income --project <project> --region europe-west1 --format='value(status.url,status.latestReadyRevisionName)'
gcloud run revisions list --service aestheticlab-income --project <project> --region europe-west1 --limit 5
gcloud logging read 'resource.type="cloud_run_revision" AND resource.labels.service_name="aestheticlab-income" AND severity>=WARNING' --project <project> --limit 50 --freshness 1d
gcloud firestore databases describe --database aestheticlab-income --project <project>
gcloud firestore backups list --location europe-west1 --project <project>
gcloud dns record-sets list --zone aestheticlab --project <project> --name kassa.aestheticlab.be.
curl -fsS https://kassa.aestheticlab.be/readyz
```

## Rollback (ask first)
```bash
gcloud run services update-traffic aestheticlab-income --project <project> --region europe-west1 --to-revisions <previous-revision>=100
```

## Restore drill (always into a new database, never over the live one)
```bash
gcloud firestore databases restore --source-backup projects/<project>/locations/europe-west1/backups/<id> --destination-database aestheticlab-income-restore-test --project <project>
# or point-in-time: gcloud firestore databases clone --source-database projects/<project>/databases/aestheticlab-income --snapshot-time <RFC3339> --destination-database aestheticlab-income-restore-test
```
Check the record counts, then delete **only** the `-restore-test` database (ask first).

The full setup script, including first-time infrastructure, lives in `docs/MAINTAINING.md`.
