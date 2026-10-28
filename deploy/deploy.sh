#!/usr/bin/env bash
# Deploy Before Yes to the shared Hetzner box behind Traefik, with tenant.sh.
#
# Usage:
#   deploy/deploy.sh [hostname]               build the current commit, ship it, run it
#   deploy/deploy.sh [hostname] <image-tag>   roll back: run an image already on the host
#
# The hostname defaults to beforeyes.nryn.dev. Confirm it when asked.
# The origin address is never written here. It comes from ORIGIN_IP in the
# environment or in deploy/origin.env (gitignored; see deploy/origin.env.example).
#
# Needs: a clean git tree, Docker locally, SSH access to root@ORIGIN_IP, and
# the host env file /opt/before-yes/.env.deploy (mode 0600, see deploy/README.md).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAME="beforeyes"
PORT="8080"
DEFAULT_HOSTNAME="beforeyes.nryn.dev"
REMOTE_ENV="/opt/before-yes/.env.deploy"
VOLUME="beforeyes-data"
TENANT_SH="${TENANT_SH:-$HOME/work/hetzner-hosting/tenant.sh}"
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
SSH_OPTS=(-i "$SSH_KEY" -o StrictHostKeyChecking=accept-new)
ssh_host() { ssh "${SSH_OPTS[@]}" "root@${ORIGIN_IP}" "$@"; }

HOSTNAME_ARG="${1:-$DEFAULT_HOSTNAME}"
ROLLBACK_TAG="${2:-}"

echo "━━━ Before Yes deploy ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Hostname:  $HOSTNAME_ARG  (port $PORT, container $NAME)"
echo "  Volume:    $VOLUME -> /data"
echo "  Env file:  $REMOTE_ENV (on the host)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
read -r -p "Deploy to $HOSTNAME_ARG? Type the hostname to confirm: " answer || answer=""
if [[ "$answer" != "$HOSTNAME_ARG" ]]; then
  echo "not confirmed; nothing deployed" >&2
  exit 1
fi

if [[ -n "$ROLLBACK_TAG" ]]; then
  IMAGE="$ROLLBACK_TAG"
  echo "→ Rollback: checking $IMAGE is on the host"
  if ! ssh_host "docker image inspect '$IMAGE' >/dev/null 2>&1"; then
    echo "error: $IMAGE is not on the host; deploy that commit first" >&2
    exit 1
  fi
else
  if [[ -n "$(git -C "$ROOT" status --porcelain)" ]]; then
    echo "error: the working tree has changes; commit them first so the image tag matches a commit" >&2
    exit 1
  fi
  SHA="$(git -C "$ROOT" rev-parse --short=12 HEAD)"
  IMAGE="before-yes:$SHA"
  echo "→ Building $IMAGE (this takes a few minutes)"
  docker build -t "$IMAGE" "$ROOT"
  echo "→ Copying $IMAGE to the host"
  docker save "$IMAGE" | gzip | ssh_host "gunzip | docker load"
fi

echo "→ Checking the host env file"
if ! ssh_host "test -f '$REMOTE_ENV' && [ \"\$(stat -c %a '$REMOTE_ENV')\" = 600 ]"; then
  echo "error: $REMOTE_ENV must exist on the host with mode 0600 (see deploy/README.md)" >&2
  exit 1
fi

echo "→ Ensuring the data volume exists"
ssh_host "docker volume create $VOLUME >/dev/null"

echo "→ Running $IMAGE behind Traefik with tenant.sh"
FOLEYFLOW_HETZNER_HOST="$ORIGIN_IP" FOLEYFLOW_SSH_KEY="$SSH_KEY" \
  "$TENANT_SH" "$NAME" "$HOSTNAME_ARG" "$PORT" "$IMAGE" \
  --env-file "$REMOTE_ENV" -v "$VOLUME:/data"

echo "→ Checking /healthz on the origin (bypasses public DNS)"
sleep 8
ssh_host "curl -k -fsS --resolve ${HOSTNAME_ARG}:443:127.0.0.1 https://${HOSTNAME_ARG}/healthz" \
  && echo "  healthz ok" || echo "  healthz not answering yet; retry in 30s, or check: docker logs $NAME"

echo "━━━ Deployed $IMAGE ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Next, if the DNS record is not there yet, add it in Cloudflare:"
echo "    A record  beforeyes  ->  the origin IP (ORIGIN_IP), Proxied"
echo "  Then seed the demo cards once: deploy/seed.sh"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
