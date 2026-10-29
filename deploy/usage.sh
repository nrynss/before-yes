#!/usr/bin/env bash
# Print the usage counts from the running container: one row per UTC day,
# newest first, one column per event, and a totals row. The counts are
# private to the owners. This script runs over ssh, and no HTTP route or page
# shows them.
#
# Usage: deploy/usage.sh [-days N]     (N defaults to 14)
# Reads ORIGIN_IP from the environment or deploy/origin.env, as deploy/seed.sh does.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAME="beforeyes"
SSH_KEY="${BEFORE_YES_SSH_KEY:-$HOME/.ssh/id_ed25519_hetzner}"

DAYS=14
if [[ "${1:-}" == "-days" ]]; then
  DAYS="${2:-}"
  if [[ ! "$DAYS" =~ ^[0-9]+$ ]] || (( DAYS < 1 || DAYS > 3650 )); then
    echo "error: -days takes a whole number from 1 to 3650" >&2
    exit 1
  fi
elif [[ $# -gt 0 ]]; then
  echo "usage: deploy/usage.sh [-days N]" >&2
  exit 1
fi

if [[ -f "$ROOT/deploy/origin.env" ]]; then
  # shellcheck disable=SC1091
  source "$ROOT/deploy/origin.env"
fi
ORIGIN_IP="${ORIGIN_IP:-}"
if [[ -z "$ORIGIN_IP" || "$ORIGIN_IP" == "<origin-ip>" ]]; then
  echo "error: set ORIGIN_IP in the environment or in deploy/origin.env" >&2
  exit 1
fi

ssh -i "$SSH_KEY" -o StrictHostKeyChecking=accept-new "root@${ORIGIN_IP}" \
  "docker exec $NAME /app/before-yes usage -config /app/before-yes.toml -days $DAYS"
