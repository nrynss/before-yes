#!/usr/bin/env bash
# Load the three demo cards (Roblox, Minecraft, Khan Academy Kids) into the
# running container's card store. The cards are baked into the image, so this
# uses no network and spends nothing. Running it again changes nothing.
#
# Usage: deploy/seed.sh
# Reads ORIGIN_IP from the environment or deploy/origin.env, as deploy/deploy.sh does.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAME="beforeyes"
SSH_KEY="${BEFORE_YES_SSH_KEY:-$HOME/.ssh/id_ed25519_hetzner}"

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
  "docker exec $NAME /app/before-yes seed-cards -config /app/before-yes.toml -dir /app/testdata/demo-cards"
