#!/usr/bin/env bash
# Update an ALREADY-DEPLOYED redactor LXC in place: push the current source,
# rebuild the native engine + Go binary, and restart the service.
# Run this ON THE PROXMOX HOST (needs `pct`).
#
# It reuses the idempotent provision.sh, so it keeps the existing
# /etc/redactor/redactor.env (API key, pool size, threads) and the downloaded
# model — only the source is refreshed, rebuilt (cmake --fresh, picks up new
# BLAS flags), and the service restarted.
#
# Override settings via env, e.g.:
#   CTID=210 MODEL_VARIANT=q8 bash deploy/proxmox/update-lxc.sh
set -euo pipefail

CTID="${CTID:-118}"
APP_SRC="${APP_SRC:-/opt/redactor/src}"
MODEL_VARIANT="${MODEL_VARIANT:-q8}"

PROJECT_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

command -v pct >/dev/null || { echo "pct not found — run on the Proxmox host" >&2; exit 1; }
pct status "$CTID" >/dev/null 2>&1 || { echo "CT ${CTID} not found" >&2; exit 1; }

# Start the container if it is stopped, and wait for network.
if ! pct status "$CTID" | grep -q running; then
  echo "==> starting CT ${CTID}"
  pct start "$CTID"
  for _ in $(seq 1 30); do
    pct exec "$CTID" -- getent hosts deb.debian.org >/dev/null 2>&1 && break
    sleep 2
  done
fi

echo "==> [1/3] packaging current source"
TAR="$(mktemp -t redactor-src.XXXXXX).tgz"
# Same source set + exclusions as create-lxc.sh. The build dir is excluded so
# the rebuild starts from the new flags; the model and .git are never shipped.
tar czf "$TAR" -C "$PROJECT_ROOT" \
  --exclude='.git' \
  --exclude='third_party/privacy-filter.cpp/build' \
  --exclude='third_party/privacy-filter.cpp/demo' \
  --exclude='third_party/privacy-filter.cpp/.github' \
  --exclude='models' \
  --exclude='bin' \
  cmd internal scripts deploy third_party go.mod Makefile README.md

echo "==> [2/3] pushing source to CT ${CTID}:${APP_SRC}"
pct exec "$CTID" -- mkdir -p "$APP_SRC"
pct push "$CTID" "$TAR" /tmp/redactor-src.tgz
pct exec "$CTID" -- tar xzf /tmp/redactor-src.tgz -C "$APP_SRC"
pct exec "$CTID" -- rm -f /tmp/redactor-src.tgz
rm -f "$TAR"

echo "==> [3/3] rebuild + restart (provision.sh — keeps env + model)"
pct exec "$CTID" -- env \
  APP_SRC="$APP_SRC" \
  MODEL_VARIANT="$MODEL_VARIANT" \
  bash "$APP_SRC/deploy/proxmox/provision.sh"

echo
echo "==> update complete."
IPADDR="$(pct exec "$CTID" -- hostname -I 2>/dev/null | awk '{print $1}')"
if pct exec "$CTID" -- curl -fsS http://127.0.0.1:8080/readyz >/dev/null 2>&1; then
  echo "    readyz: OK"
else
  echo "    readyz: FAILED — check: pct exec ${CTID} -- journalctl -u redactor -e"
fi
echo "    API:  http://${IPADDR:-<container-ip>}:8080"
echo "    Logs: pct exec ${CTID} -- journalctl -u redactor -f"
