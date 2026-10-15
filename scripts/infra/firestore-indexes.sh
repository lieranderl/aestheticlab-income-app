#!/usr/bin/env bash
# Creates every composite index listed in firestore.indexes.json that does not
# exist yet (gcloud has no import for named databases). Safe to re-run.
#   scripts/infra/firestore-indexes.sh
set -euo pipefail
PROJECT=${PROJECT:?set PROJECT to the Google Cloud project ID}
FILE=${1:-firestore.indexes.json}
DB=${DB:-$(jq -r .database "$FILE")}
existing=$(gcloud firestore indexes composite list --project "$PROJECT" --database "$DB" --format=json \
  | jq -r '.[] | (.name | split("/collectionGroups/")[1] | split("/")[0]) + ":" + ([.fields[] | select(.fieldPath != "__name__") | .fieldPath + "=" + (.order // .arrayConfig | ascii_downcase)] | join(","))')
jq -c '.indexes[]' "$FILE" | while read -r idx; do
  group=$(jq -r .collectionGroup <<<"$idx")
  key="$group:$(jq -r '[.fields[] | .fieldPath + "=" + .order] | join(",")' <<<"$idx")"
  if grep -qxF "$key" <<<"$existing"; then echo "exists:   $key"; continue; fi
  args=()
  while read -r f; do args+=(--field-config="$f"); done < <(jq -r '.fields[] | "field-path=\(.fieldPath),order=\(.order)"' <<<"$idx")
  gcloud firestore indexes composite create --project "$PROJECT" --database "$DB" --collection-group "$group" "${args[@]}" --async --quiet >/dev/null
  echo "creating: $key"
done
