#!/usr/bin/env bash
# Runs govulncheck and fails only on findings that are not explicitly allowlisted below.
#
# Every entry must reference the tracking issue and the reason a normal dependency bump
# cannot close it (e.g. no stable release exists yet). Remove the entry — do not just leave
# it — once a real fix is available and applied.
set -uo pipefail

ALLOWED_IDS=(
  # GO-2026-6443: grpc server panic via missing authority/Host headers. The fix exists only
  # in v1.85.0-dev (google.golang.org/grpc) — no stable release contains it yet. Tracked in
  # issue #131; remove this entry once grpc ships a stable release with the fix and the
  # dependency is bumped.
  "GO-2026-6443"
)

OUTPUT=$(govulncheck ./... 2>&1)
STATUS=$?

echo "$OUTPUT"

if [ "$STATUS" -eq 0 ]; then
  exit 0
fi

FOUND_IDS=$(echo "$OUTPUT" | grep -oE '^Vulnerability #[0-9]+: GO-[0-9]+-[0-9]+' | awk '{print $3}')

UNALLOWED=()
for id in $FOUND_IDS; do
  allowed=false
  for a in "${ALLOWED_IDS[@]}"; do
    if [ "$id" = "$a" ]; then
      allowed=true
      break
    fi
  done
  if [ "$allowed" = false ]; then
    UNALLOWED+=("$id")
  fi
done

echo ""
if [ ${#UNALLOWED[@]} -gt 0 ]; then
  echo "govulncheck-gate: FAIL — un-allowlisted vulnerabilities found: ${UNALLOWED[*]}"
  exit 1
fi

echo "govulncheck-gate: all findings are explicitly allowlisted in this script; passing"
exit 0
