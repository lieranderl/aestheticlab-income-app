# Maintaining Aesthetic Lab Kassa

Operations runbook. Project **`<project>`** (number `<project-number>`), region **`europe-west1`**. Always pass `--project <project>`: the default gcloud project on the dev machine is a different one. Before running anything here, read the `gcp-ops` skill (`.agents/skills/gcp-ops/SKILL.md`) for what this app owns in the shared project.

## Resources (created in M1, 2026-10-08)

| Resource | Name | Notes |
|---|---|---|
| Firestore database | `aestheticlab-income` (Native, europe-west1) | Delete protection on. Point-in-time recovery 7 days. Daily backups kept 7 days, weekly (Sunday) kept 14 weeks. No Firebase on this project, so the only access is through IAM. |
| Composite indexes | `firestore.indexes.json` | Applied with `scripts/infra/firestore-indexes.sh`, which is safe to re-run. |
| Cloud Run service | `aestheticlab-income` | Public (allUsers invoker), 1 vCPU, 512 MiB, 0–2 instances, concurrency 80, gen2, CPU throttling plus startup boost, 300 s timeout, label `app=kassa`. |
| Domain | `kassa.aestheticlab.be` | Cloud Run domain mapping plus a CNAME `kassa → ghs.googlehosted.com.` (TTL 300) in Cloud DNS zone `aestheticlab`. Google manages the certificate. |
| Image repository | Artifact Registry `aestheticlab-income` (docker) | Separate from the website's `aestheticlab` repository. Cleanup keeps the latest 10 images and deletes anything older than 30 days. |
| Runtime service account | `aestheticlab-income-runtime@` | `roles/datastore.user` conditioned on `databases/aestheticlab-income` only. `secretAccessor` on `INCOME_SESSION_KEY` only. |
| Deployer service account | `aestheticlab-income-deployer@` | `run.developer` on the service only; `artifactregistry.writer` on `aestheticlab-income` only; `serviceAccountUser` on the runtime account. Workload Identity user for GitHub. |
| Backup service account | `aestheticlab-income-backup@` | Custom role `kassaFirestoreExporter` (export, metadata, operations get/list; **no import**) with a condition limiting it to this database; `storage.objectCreator` on the backups bucket (can't delete or overwrite exports). A restore is done by a person, never by this account. |
| GitHub access | WIF provider `github-income` in pool `aestheticlab-github` | Condition: `repository_id == 1410616671`, owner id `19622412`, `sub = repo:lieranderl@19622412/aestheticlab-income-app@1410616671:environment:production`, `ref = refs/heads/main`. GitHub environment `production`. The repo uses GitHub's **immutable OIDC subject** (owner and repo ids in `sub`), unlike the website's older repo. Renaming or re-creating the repo therefore breaks deploys on purpose. |
| Secret | `INCOME_SESSION_KEY` | 48 random bytes, base64, generated locally and never printed. Replicated in europe-west1 only. |
| Backups bucket | `gs://<project>-income-backups` | Uniform access, public access prevention, objects deleted after 1,095 days, label `app=kassa`. **90-day retention policy (unlocked)**: no one can delete or overwrite an export younger than 90 days; 7-day soft delete on top. The Firestore service agent can write to it. Locking the retention policy would make it permanent and can't be undone; it's left unlocked on purpose. |
| Monthly export | Cloud Scheduler job `income-firestore-export` | `0 3 1 * *` Europe/Brussels. POSTs to `…/databases/aestheticlab-income:exportDocuments` with an **OAuth** token (scope `datastore`) as the backup account. The output goes to a timestamped folder in the bucket. |
| Alert | «Kassa production 5xx responses» | Any 5xx in 5 minutes → the existing channel «Aesthetic Lab production alerts» (<owner-email>). |
| Uptime check and alert | «Kassa production readyz» / «Kassa production readyz failing» | `https://kassa.aestheticlab.be/readyz` every 5 minutes from Europe, the USA and Asia-Pacific; it must answer 200 with `ready`. Alert when 2+ regions fail. Policy template in `scripts/infra/monitoring/kassa-uptime.json` (`${CHECK_ID}` is filled in by `create-uptime-alert.sh`, because Monitoring assigns the ID on creation). |
| Budget | «Kassa (app=kassa)» | €5/month on the billing account, filtered by label `app=kassa`. Alerts at 50, 90 and 100 %, plus a forecast alert. Firestore can't carry labels, so its few cents aren't counted. |

### Cloud Run service settings
These live on the service. The deploy workflow only changes the image.
```
INCOME_BASE_URL=https://kassa.aestheticlab.be
INCOME_FIRESTORE_PROJECT=<project>
INCOME_FIRESTORE_DATABASE=aestheticlab-income
INCOME_START_MONTH=2026-10
INCOME_TZ=Europe/Brussels
INCOME_SESSION_KEY=secret:INCOME_SESSION_KEY:latest
```
**Production only, from 2026-10-12:** use **https://kassa.aestheticlab.be**, Cloud Run `aestheticlab-income`, and the main database `aestheticlab-income`, with `INCOME_START_MONTH=2026-10`.

**October 2026 is the first real month (decision 2026-10-14).** It runs in parallel with the sheet, which stays the record until October reconciles (M12). The practice entries made on 12–14 October (7 client entries, 2 movements) were all voided in the app. They count nowhere and stay as history: they appear in «Журнал изменений», and they moved the per-day client numbers on 1, 2 and 14 October for the two people who tested. No cleanup or cut-off change is needed. From now on every October record is real: never delete one, void it with a reason.

Account recovery and imports target `-database aestheticlab-income`. There is no separate trial environment or session secret.

**Local October testing:** run `make dev-october` and open http://localhost:8080. It uses memory only, prepares Anna's October terms, and prints a temporary password; every restart resets local data/login. `make dev` runs the emulator with hot reload and an explicit October cutoff, preserving its data while it runs; configure staff terms through the app. Use `DEV_PORT=8090` for another port and `make dev-memory DEV_START_MONTH=2026-08` to include August locally. `make web-dev` uses `.env.web`; an absent file uses the normal November cutoff. These local commands do not change the production cutoff or database.

**Real October, step by step:**
1. **Openings:** Anna gives the 1 October figures (see Opening balances below). Run the dry run, Anna confirms the printed figures, then load them after explicit approval.
2. **Backfill 1–14 October:** each person enters their own clients from the sheet with the right date. Anna enters October's deposits, expenses and payouts in «Касса» with their real dates.
3. **Daily:** every client goes into both the sheet and the app.
4. **Early November:** close October in the sheet and in the app and compare per person: revenue, «Себе», salon income, expenses, profit, dividends and balances. Explain every difference before relying on the app alone.

**Pausing financial writes** stays available for any controlled data operation. It shows the banner «Запись финансовых операций временно приостановлена» and refuses financial writes, including the admin's; reads and sign-in keep working:
```bash
gcloud run services update aestheticlab-income --project <project> --region europe-west1 \
  --update-env-vars=INCOME_PAUSE_FINANCIAL_WRITES=true --quiet
gcloud run services update aestheticlab-income --project <project> --region europe-west1 \
  --remove-env-vars=INCOME_PAUSE_FINANCIAL_WRITES --quiet
```

## Deploys

**First real deploy:** 2026-10-08, after M1 merged. The first attempt failed with `unauthorized_client … rejected by the attribute condition`: the provider expected the old `repo:owner/name` subject, but this repo uses GitHub's immutable subject. Fixed by matching the condition and the deployer binding to the immutable subject, then re-running.
- **Automatic:** the CI workflow's last job on `main` calls `.github/workflows/web.yml` after «CI passed», so it deploys exactly the commit CI tested; a red CI run never deploys. CI runs on `main` queue in push order, and a run whose commit is no longer the head of `main` skips the deploy, so an older commit never replaces a newer one. Every green `main` run deploys, docs-only changes included (same code, new revision): pushes to `main` always run the full CI. Only a docs-only pull request (`docs/`, `*.md`, `.agents/`, `.claude/`) skips the lint/test/build, emulator and phone-check jobs; its «CI passed» still has to be green. It builds the image before signing in to Google Cloud, deploys a revision without traffic (tag `candidate`), checks the candidate's `/readyz`, then moves all traffic to it.
- **Manual re-run:** Actions → CI → «Run workflow» on `main` (CI runs first).
- **Roll back** (ask first):
  ```bash
  gcloud run revisions list --service aestheticlab-income --project <project> --region europe-west1 --limit 5
  ```
  ```bash
  gcloud run services update-traffic aestheticlab-income --project <project> --region europe-west1 --to-revisions <revision>=100
  ```

## CI
CI runs on GitHub-hosted runners (`ubuntu-latest`), free and unlimited for this public repository. Never add a self-hosted runner: on a public repo, a pull request from any fork could run code on it. A docs-only pull request (`docs/`, `*.md`, `.agents/`, `.claude/`) skips the lint/test/build, emulator and phone-check jobs; «CI passed» still has to be green, and pushes to `main` always run everything.

## Backups and restore
- **Point-in-time recovery:** any minute in the last 7 days.
- **Scheduled backups:**
  ```bash
  gcloud firestore backups list --location europe-west1 --project <project>
  ```
- **Monthly exports:**
  ```bash
  gcloud storage ls gs://<project>-income-backups/
  ```
- **Restore, always into a new database, never over the live one:**
  ```bash
  gcloud firestore databases restore --project <project> \
    --source-backup projects/<project>/locations/europe-west1/backups/<id> \
    --destination-database aestheticlab-income-restore-test
  ```
  or from a point in time:
  ```bash
  gcloud firestore databases clone --project <project> \
    --source-database projects/<project>/databases/aestheticlab-income \
    --snapshot-time <RFC3339> --destination-database aestheticlab-income-restore-test
  ```
  Compare the copy with the live database read at the same moment (read-only; needs `gcloud auth application-default login`):
  ```bash
  go run ./cmd/restorecheck aestheticlab-income <RFC3339 snapshot time>
  go run ./cmd/restorecheck aestheticlab-income-restore-test
  ```
  The two outputs (documents per collection, then one fingerprint over every document) must be identical. A clone copies the source's delete protection, so delete **only** the `-restore-test` database in two steps:
  ```bash
  gcloud firestore databases update --database=aestheticlab-income-restore-test --no-delete-protection --project <project>
  gcloud firestore databases delete --database=aestheticlab-income-restore-test --project <project>
  ```
- **Restore drill, 2026-10-14 (M11):**
  1. Cloned the live database at **18:21:00 UTC** into `aestheticlab-income-restore-test`. The clone started at 18:26:31 and finished at 18:38:59 (`SUCCESSFUL`), so allow about **15 minutes** for this database size.
  2. Ran `cmd/restorecheck` on the live database at 18:21:00 and on the copy. Both gave 140 documents: accounts 2, audit 89, categories 16, clientDays 5, ledger 2, reports 7, usernames 4, users 4, users/{id}/terms 11. The fingerprint `57e118fa…8eba` matched, and the diff was empty. The live database was unchanged afterwards. That run used the first version of `cmd/restorecheck`, which listed collections at the current time and fingerprinted JSON. After review it now lists collections and documents at the read time too, and fingerprints the typed protobuf fields, so an integer and a double holding the same number no longer match. For the same 18:21 data the new fingerprint is `4beccb2c…b028`, still 140 documents. The next drill uses the new version.
  3. `jev_verify` (jev-1.13.0) verified the three completion claims: separate database, identical data, live untouched. That check is advisory; the exact comparison is the proof.
  4. Turned off delete protection on the copy only and deleted the copy.
- **First test export:** run on 2026-10-08 and written to `gs://<project>-income-backups/2026-10-08T16:53:16_97403/`. The first attempt failed with 403 because a new IAM binding took a few minutes to propagate. Allow ~5 minutes after granting before you test.

## Setup commands (as run in M1)
For rebuilding from scratch, or for review. Each step checks whether its resource already exists.

```bash
P=<project>; PN=<project-number>
RUNTIME=aestheticlab-income-runtime@$P.iam.gserviceaccount.com
DEPLOYER=aestheticlab-income-deployer@$P.iam.gserviceaccount.com
BACKUP=aestheticlab-income-backup@$P.iam.gserviceaccount.com
# GitHub's immutable OIDC subject for this repo (gh api repos/lieranderl/aestheticlab-income-app/actions/oidc/customization/sub)
SUB='repo:lieranderl@19622412/aestheticlab-income-app@1410616671:environment:production'

# 1. Firestore, backups, indexes
gcloud firestore databases create --project $P --database=aestheticlab-income --location=europe-west1 \
  --type=firestore-native --delete-protection --enable-pitr
gcloud firestore backups schedules create --project $P --database=aestheticlab-income --recurrence=daily --retention=7d
gcloud firestore backups schedules create --project $P --database=aestheticlab-income --recurrence=weekly --day-of-week=SUN --retention=14w
scripts/infra/firestore-indexes.sh

# 2. Service accounts and the secret
gcloud iam service-accounts create aestheticlab-income-runtime  --project $P --display-name="Kassa runtime (Cloud Run)"
gcloud iam service-accounts create aestheticlab-income-deployer --project $P --display-name="Kassa deployer (GitHub Actions)"
gcloud iam service-accounts create aestheticlab-income-backup   --project $P --display-name="Kassa monthly Firestore export"
gcloud projects add-iam-policy-binding $P --member="serviceAccount:$RUNTIME" --role=roles/datastore.user \
  --condition='expression=resource.name=="projects/<project>/databases/aestheticlab-income",title=only-aestheticlab-income-db,description=Kassa runtime may only use its own Firestore database'
openssl rand -base64 48 | tr -d '\n' | gcloud secrets create INCOME_SESSION_KEY --project $P \
  --replication-policy=user-managed --locations=europe-west1 --labels=app=kassa --data-file=-
gcloud secrets add-iam-policy-binding INCOME_SESSION_KEY --project $P --member="serviceAccount:$RUNTIME" --role=roles/secretmanager.secretAccessor

# 3. GitHub access (keyless)
gcloud iam workload-identity-pools providers create-oidc github-income --project $P --location=global \
  --workload-identity-pool=aestheticlab-github --display-name="Kassa production (main)" \
  --issuer-uri="https://token.actions.githubusercontent.com" \
  --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository,attribute.repository_owner_id=assertion.repository_owner_id" \
  --attribute-condition="assertion.repository_id == '1410616671' && assertion.repository_owner_id == '19622412' && assertion.sub == '$SUB' && assertion.ref == 'refs/heads/main'"
gcloud iam service-accounts add-iam-policy-binding $DEPLOYER --project $P --role=roles/iam.workloadIdentityUser \
  --member="principal://iam.googleapis.com/projects/$PN/locations/global/workloadIdentityPools/aestheticlab-github/subject/$SUB"
gcloud iam service-accounts add-iam-policy-binding $RUNTIME --project $P --role=roles/iam.serviceAccountUser --member="serviceAccount:$DEPLOYER"
gh api -X PUT repos/lieranderl/aestheticlab-income-app/environments/production

# 4. Image repository (this app only)
gcloud artifacts repositories create aestheticlab-income --project $P --location=europe-west1 --repository-format=docker \
  --description="Aesthetic Lab Kassa images" --labels=app=kassa
gcloud artifacts repositories set-cleanup-policies aestheticlab-income --project $P --location=europe-west1 \
  --policy=<(echo '[{"name":"keep-latest-10","action":{"type":"Keep"},"mostRecentVersions":{"keepCount":10}},{"name":"delete-older-than-30d","action":{"type":"Delete"},"condition":{"olderThan":"2592000s"}}]') --no-dry-run
gcloud artifacts repositories add-iam-policy-binding aestheticlab-income --project $P --location=europe-west1 \
  --role=roles/artifactregistry.writer --member="serviceAccount:$DEPLOYER"

# 5. Cloud Run service (placeholder image; the Deploy workflow replaces it)
gcloud run deploy aestheticlab-income --project $P --region europe-west1 \
  --image us-docker.pkg.dev/cloudrun/container/hello --service-account $RUNTIME --allow-unauthenticated --ingress all \
  --cpu 1 --memory 512Mi --min-instances 0 --max-instances 2 --concurrency 80 \
  --execution-environment gen2 --cpu-throttling --cpu-boost --port 8080 --timeout 300 \
  --set-env-vars "INCOME_BASE_URL=https://kassa.aestheticlab.be,INCOME_FIRESTORE_PROJECT=$P,INCOME_FIRESTORE_DATABASE=aestheticlab-income,INCOME_TZ=Europe/Brussels" \
  --set-secrets "INCOME_SESSION_KEY=INCOME_SESSION_KEY:latest" --labels app=kassa
gcloud run services add-iam-policy-binding aestheticlab-income --project $P --region europe-west1 \
  --role=roles/run.developer --member="serviceAccount:$DEPLOYER"

# 6. Domain
gcloud beta run domain-mappings create --service aestheticlab-income --domain kassa.aestheticlab.be --project $P --region europe-west1
gcloud dns record-sets create kassa.aestheticlab.be. --zone=aestheticlab --project $P --type=CNAME --ttl=300 --rrdatas="ghs.googlehosted.com."

# 7. Backups bucket and monthly export
gcloud storage buckets create gs://<project>-income-backups --project $P --location=europe-west1 \
  --default-storage-class=STANDARD --uniform-bucket-level-access --public-access-prevention \
  --lifecycle-file=<(echo '{"rule":[{"action":{"type":"Delete"},"condition":{"age":1095}}]}')
gcloud storage buckets update gs://<project>-income-backups --update-labels=app=kassa --retention-period=90d
gcloud storage buckets add-iam-policy-binding gs://<project>-income-backups \
  --member="serviceAccount:service-$PN@gcp-sa-firestore.iam.gserviceaccount.com" --role=roles/storage.objectAdmin
gcloud storage buckets add-iam-policy-binding gs://<project>-income-backups --member="serviceAccount:$BACKUP" --role=roles/storage.objectCreator
gcloud iam roles create kassaFirestoreExporter --project $P --title="Kassa Firestore exporter" \
  --description="Export only (no import) of the aestheticlab-income Firestore database; used by the monthly backup job" \
  --permissions=datastore.databases.export,datastore.databases.getMetadata,datastore.operations.get,datastore.operations.list,resourcemanager.projects.get --stage=GA
gcloud projects add-iam-policy-binding $P --member="serviceAccount:$BACKUP" --role=projects/$P/roles/kassaFirestoreExporter \
  --condition='expression=resource.name=="projects/<project>/databases/aestheticlab-income",title=only-aestheticlab-income-db,description=Kassa backup may only export its own Firestore database'
gcloud scheduler jobs create http income-firestore-export --project $P --location europe-west1 \
  --description="Monthly export of the Kassa Firestore database (kept 3 years)" \
  --schedule="0 3 1 * *" --time-zone="Europe/Brussels" \
  --uri="https://firestore.googleapis.com/v1/projects/$P/databases/aestheticlab-income:exportDocuments" \
  --http-method=POST --headers="Content-Type=application/json" \
  --message-body='{"outputUriPrefix":"gs://<project>-income-backups"}' \
  --oauth-service-account-email="$BACKUP" --oauth-token-scope="https://www.googleapis.com/auth/datastore"

# 8. Monitoring (policy files in scripts/infra/monitoring/)
gcloud alpha monitoring policies create --project $P --policy-from-file=scripts/infra/monitoring/kassa-5xx.json
gcloud monitoring uptime create "Kassa production readyz" --project $P --resource-type=uptime-url \
  --resource-labels=host=kassa.aestheticlab.be,project_id=$P --protocol=https --port=443 --path=/readyz \
  --validate-ssl=true --status-codes=200 --matcher-content=ready --matcher-type=contains-string \
  --period=5 --timeout=10 --regions=europe,usa-oregon,asia-pacific --user-labels=app=kassa
scripts/infra/monitoring/create-uptime-alert.sh   # looks up the new check ID and fills it into kassa-uptime.json

# 9. Budget
gcloud services enable billingbudgets.googleapis.com --project $P
gcloud billing budgets create --billing-account=017DA5-13C25F-05C229 --billing-project=$P --display-name="Kassa (app=kassa)" \
  --budget-amount=5EUR --calendar-period=month --filter-labels=app=kassa \
  --threshold-rule=percent=0.5 --threshold-rule=percent=0.9 --threshold-rule=percent=1.0 --threshold-rule=percent=1.0,basis=forecasted-spend \
  --notifications-rule-monitoring-notification-channels=projects/<project>/notificationChannels/<channel-id>
```

## First data (`cmd/seed`, once)
`cmd/seed` writes Anna's admin account, the default categories, payment methods, expense categories and the two kassa accounts in one transaction. It refuses to run when any user exists, so a second run changes nothing. It prints Anna's temporary password once; hand it to her in person. She must change it at first login.

Ask first; it writes to production. Run it with your own Application Default Credentials (`gcloud auth application-default login`):
```bash
go run ./cmd/seed -project <project> -database aestheticlab-income
```
Then, signed in as Anna:
1. Add Maria, Olga and Vera on «Ещё → Сотрудники», each with a temporary password. Hand each password over in person. Everyone sets their own password at first login.
2. Set everyone's terms from the start month (November 2026), Anna's included: «Себе» %, monthly salary and dividend share. Reports and the month close are refused for a person without terms.

Try it against the emulator first (`make firestore`):
```bash
FIRESTORE_EMULATOR_HOST=localhost:8086 go run ./cmd/seed -project demo-kassa
```

## Retired trial resources (2026-10-12)

The user authorized complete removal. Production was switched to the existing main database, checked at the no-traffic candidate and public `/readyz`, then the trial service, database, dedicated account, secret and obsolete conditional IAM grants were removed. Existing main data was not replaced with trial data. The trial-copy command and trial-deploy script are removed.

Verified managed recovery exports in the existing retained bucket:
- Main before October testing: `gs://<project>-income-backups/main-before-october-testing-20261012` (78 documents, successful).
- Trial before deletion: `gs://<project>-income-backups/trial-retirement-20261012` (75 documents, successful).

Restore into a new database if needed; never overwrite the live database. Revisions configured for the deleted trial database are no longer rollback targets. Roll back only to a healthy revision using the main database and the intended start month, or deploy the prior image with the current main settings through the rollback procedure.

## Opening balances (`cmd/import opening`, 1 October)
The app tracks money from the start month's first day (technical-solution §5.6). The figures before it go in once as `opening` ledger rows dated that day:
- **The morning of 1 October, before the first client:** cash in the till, the salon money on the salon's card (card balance minus her own money), and certificates sold before 1 October and not yet used. If the till wasn't counted that morning, Anna reconstructs the figure from the sheet.
- **Each person's September «Settlement Balance»** from the sheet, counting only payouts dated on or before 30 September. Payouts made from 1 October on are entered in the app as October movements.

Write the figures in a CSV on your machine only (`*.csv` is gitignored). `;` or `,` separates the columns, as the header line does; amounts take a decimal comma. A negative person amount means they owe the salon. `already_paid` is the sheet's September «Already paid», printed only for checking:
```
kind;id;amount;already_paid
kassa;kassa_cash;412,50;
kassa;kassa_card;1830;
certificates;;500;
person;maria;265,13;0
```
Always run `-dry-run` first and have Anna confirm the printed figures. Then ask, as the real run writes to production. Rows can only be loaded from the start day on, and the file loads in one transaction, whole or not at all. A figure that is already loaded with the same amount is skipped, so the person rows can follow a few days later in the same file. A different amount for a figure already loaded is refused: void the wrong row first (M7).
```bash
go run ./cmd/import opening -project <project> -database aestheticlab-income -start-month 2026-10 -dry-run opening.csv
```
```bash
go run ./cmd/import opening -project <project> -database aestheticlab-income -start-month 2026-10 opening.csv
```
Always pass `-start-month 2026-10`: the tool's default is the code default `2026-11`, and production uses October.

## Renaming a kassa account
The app has no screen for account names. `cmd/renameaccount` changes only the display name, audited as `cli:renameaccount`. Run the dry run, then the real command after approval:
```bash
go run ./cmd/renameaccount -project <project> -id kassa_card -name "Касса — карта салона" -dry-run
go run ./cmd/renameaccount -project <project> -id kassa_card -name "Касса — карта салона"
```

## Logins
- **Lockout:** after 5 wrong passwords in a row, an account locks for 15 minutes. The login page answers the same way for unknown, wrong and locked logins, so it never reveals which usernames exist. Its message states the rule, and the lock shows only in the logs (`account locked after failed logins`). Resetting the password on «Сотрудники» also unlocks the account.
- **Password reset and deactivation:** both log the person out on every device at once.
- **Admin locked out, or the admin's password lost** (no one left to reset it in the app): run `cmd/resetpass` with your own Application Default Credentials. Ask first, since it writes to production. It prints a new temporary password once, only in your terminal. It ends every session, clears the lock, forces a change at the next login, and logs the change as `cli:resetpass`.
  ```bash
  go run ./cmd/resetpass -project <project> -username anna
  ```
- **Local runs** never touch production:
  - `make dev` uses the emulator (project `demo-kassa`), seeds it once and prints anna's temporary password the first time. `make firestore-reset` starts over with a new one.
  - `make dev-memory` logs a one-off password at every start.
  - Only `make web` / `make web-dev` read `.env.web`. Never point `.env.web` at `<project>`.

## Session key rotation
Rotating the key logs everyone out once. Ask first.
```bash
openssl rand -base64 48 | tr -d '\n' | gcloud secrets versions add INCOME_SESSION_KEY --project <project> --data-file=-
```
```bash
gcloud run services update aestheticlab-income --project <project> --region europe-west1 --update-secrets INCOME_SESSION_KEY=INCOME_SESSION_KEY:latest
```
Then disable the old version.

## Month-close checklist
1. Open «Месяц → Итоги и расчёт» for the selected month. Open figures are estimates; salary, «Себе» bonus and dividends are not accrued yet.
2. The month must have ended, and the preceding month must be closed (except the configured first month). Confirm or cancel all pending movements in «Касса» before closing.
3. Review missing-report warnings by date/person, and count the cash if the last recount is missing or older than seven salon-calendar days. These are warnings; pending movements, invalid terms, month order and an unfinished month block closing.
4. Check income, expenses, salary/bonus, dividends and each person's breakdown against the source records. Tap «Закрыть месяц» and confirm. Changed data requires refreshing the calculation before confirming again.
5. Check the closed status and frozen figures. The month is locked for everyone; salary, bonus and dividend accruals plus a snapshot were saved in one audited transaction. If the response was lost, refresh/check month status before trying again.
6. Record payouts or received amounts through the person's card, with the actual date in an open month. The amount is editable up to the current remaining balance, including partial payments. The current remaining balance drives suggestions; the selected month's frozen closing figure stays unchanged. «Перенести» records no payment and keeps the balance.
7. To correct a closed month, use «Открыть заново» on the newest closed month. Reopen later months first if necessary. Reopening voids its accruals and removes its snapshot; correct the records and close again in chronological order.

October is the first real month: close it like any other once its records match the sheet.
