#!/usr/bin/env bash
# Creates the «Kassa production readyz failing» alert for the existing uptime
# check. Cloud Monitoring assigns the check ID on creation, so it is looked up
# here and substituted into kassa-uptime.json (which holds ${CHECK_ID},
# ${PROJECT} and ${CHANNEL_ID}).
#   PROJECT=<project> CHANNEL_ID=<channel> APP_HOST=<app-host> scripts/infra/monitoring/create-uptime-alert.sh
set -euo pipefail
PROJECT=${PROJECT:?set PROJECT to the Google Cloud project ID}
DIR=$(cd "$(dirname "$0")" && pwd)
CHECK_ID=$(gcloud monitoring uptime list-configs --project "$PROJECT" \
  --filter='displayName="Kassa production readyz"' --format='value(name.basename())')
[ -n "$CHECK_ID" ] || { echo "uptime check «Kassa production readyz» not found: create it first" >&2; exit 1; }
if gcloud alpha monitoring policies list --project "$PROJECT" --filter='displayName="Kassa production readyz failing"' --format='value(name)' | grep -q .; then
  echo "alert policy already exists"; exit 0
fi
policy=$(mktemp); trap 'rm -f "$policy"' EXIT
: "${CHANNEL_ID:?set CHANNEL_ID to the notification channel ID}"
: "${APP_HOST:?set APP_HOST to the app's host name}"
CHECK_ID="$CHECK_ID" PROJECT="$PROJECT" CHANNEL_ID="$CHANNEL_ID" APP_HOST="$APP_HOST" envsubst '${CHECK_ID} ${PROJECT} ${CHANNEL_ID} ${APP_HOST}' < "$DIR/kassa-uptime.json" > "$policy"
gcloud alpha monitoring policies create --project "$PROJECT" --policy-from-file="$policy" --format='value(name)'
