#!/usr/bin/env bash
# Create a Proxmox LXC container and deploy the redactor service into it.
# Run this ON THE PROXMOX HOST (needs `pct`).
#
# Override any setting via environment, e.g.:
#   CTID=210 MEMORY=6144 IP=192.168.1.50/24 GATEWAY=192.168.1.1 \
#     bash deploy/proxmox/create-lxc.sh
set -euo pipefail

CTID="${CTID:-200}"
HOSTNAME="${HOSTNAME:-redactor}"
# Find templates with: pveam available | grep debian ; pveam download local <tmpl>
TEMPLATE="${TEMPLATE:-local:vztmpl/debian-12-standard_12.12-1_amd64.tar.zst}"
STORAGE="${STORAGE:-local-lvm}"     # rootfs storage
CORES="${CORES:-4}"
MEMORY="${MEMORY:-4096}"            # MiB; model is ~1.5GB resident (q8)
SWAP="${SWAP:-512}"
DISK="${DISK:-16}"                  # GiB; build + Go + model need headroom
BRIDGE="${BRIDGE:-vmbr0}"
IP="${IP:-192.168.100.118\/24}"                    # "dhcp" or "192.168.1.50/24"
GATEWAY="${GATEWAY:-192.168.100.1}"             # required when IP is static
MODEL_VARIANT="${MODEL_VARIANT:-q8}"
SSH_KEY="${SSH_KEY:-$HOME/.ssh/id_rsa.pub}"

PROJECT_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

command -v pct >/dev/null || { echo "pct not found — run on the Proxmox host" >&2; exit 1; }

if [ "$IP" = "dhcp" ]; then
  NET="name=eth0,bridge=${BRIDGE},ip=dhcp"
else
  [ -n "$GATEWAY" ] || { echo "GATEWAY is required for a static IP" >&2; exit 1; }
  NET="name=eth0,bridge=${BRIDGE},ip=${IP},gw=${GATEWAY}"
fi

SSH_OPT=()
[ -f "$SSH_KEY" ] && SSH_OPT=(--ssh-public-keys "$SSH_KEY")

echo "==> creating CT ${CTID} (${HOSTNAME})"
pct create "$CTID" "$TEMPLATE" \
  --hostname "$HOSTNAME" \
  --cores "$CORES" --memory "$MEMORY" --swap "$SWAP" \
  --rootfs "${STORAGE}:${DISK}" \
  --net0 "$NET" \
  --unprivileged 1 \
  --features nesting=1 \
  --onboot 1 \
  "${SSH_OPT[@]}"

pct start "$CTID"

echo "==> waiting for network in CT ${CTID}"
for _ in $(seq 1 30); do
  pct exec "$CTID" -- getent hosts deb.debian.org >/dev/null 2>&1 && break
  sleep 2
done

echo "==> packaging and pushing source"
TAR="$(mktemp -t redactor-src.XXXXXX).tgz"
tar czf "$TAR" -C "$PROJECT_ROOT" \
  --exclude='.git' \
  --exclude='third_party/privacy-filter.cpp/build' \
  --exclude='third_party/privacy-filter.cpp/demo' \
  --exclude='third_party/privacy-filter.cpp/.github' \
  --exclude='models' \
  --exclude='bin' \
  cmd internal scripts deploy third_party go.mod Makefile README.md
pct exec "$CTID" -- mkdir -p /opt/redactor/src
pct push "$CTID" "$TAR" /tmp/redactor-src.tgz
pct exec "$CTID" -- tar xzf /tmp/redactor-src.tgz -C /opt/redactor/src
pct exec "$CTID" -- rm -f /tmp/redactor-src.tgz
rm -f "$TAR"

echo "==> provisioning (build + model + service); this can take several minutes"
pct exec "$CTID" -- env \
  APP_SRC=/opt/redactor/src \
  MODEL_VARIANT="$MODEL_VARIANT" \
  bash /opt/redactor/src/deploy/proxmox/provision.sh

echo
echo "==> deployed. Container ${CTID} is running the redactor service."
IPADDR="$(pct exec "$CTID" -- hostname -I 2>/dev/null | awk '{print $1}')"
echo "    API:    http://${IPADDR:-<container-ip>}:8080"
echo "    Key:    see the generated key above (or /etc/redactor/redactor.env)"
echo "    Logs:   pct exec ${CTID} -- journalctl -u redactor -f"
