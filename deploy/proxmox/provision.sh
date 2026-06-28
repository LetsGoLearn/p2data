#!/usr/bin/env bash
# Provision the redactor service INSIDE a Debian/Ubuntu LXC container.
# Installs the toolchain, builds the native engine + Go binary, downloads the
# model, and installs a hardened systemd service. Idempotent — safe to re-run.
#
# Run as root inside the container:
#   APP_SRC=/opt/redactor/src MODEL_VARIANT=q8 bash provision.sh
set -euo pipefail

GO_VERSION="${GO_VERSION:-1.26.2}"
APP_SRC="${APP_SRC:-/opt/redactor/src}"
MODEL_VARIANT="${MODEL_VARIANT:-q8}"   # q8 | f16
PF_REPO="${PF_REPO:-https://github.com/localai-org/privacy-filter.cpp}"

SERVICE_USER="redactor"
DATA_DIR="/var/lib/redactor"
MODEL_DIR="$DATA_DIR/models"
ETC_DIR="/etc/redactor"

case "$MODEL_VARIANT" in
  q8)  MODEL_FILE="privacy-filter-q8.gguf" ;;
  f16) MODEL_FILE="privacy-filter-f16.gguf" ;;
  *) echo "unknown MODEL_VARIANT: $MODEL_VARIANT (use q8 or f16)" >&2; exit 1 ;;
esac

[ -d "$APP_SRC" ] || { echo "APP_SRC not found: $APP_SRC" >&2; exit 1; }

echo "==> [1/7] apt dependencies"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
# libopenblas-dev provides the build headers and runtime .so; pkg-config is
# required by ggml's BLAS backend CMake to locate OpenBLAS (GGML_BLAS=ON; see
# Makefile / cgo.go).
apt-get install -y --no-install-recommends \
  cmake build-essential git curl ca-certificates pkg-config libopenblas-dev

echo "==> [2/7] Go ${GO_VERSION}"
ARCH="$(dpkg --print-architecture)"   # amd64 | arm64
if [ "$(/usr/local/go/bin/go version 2>/dev/null | awk '{print $3}')" != "go${GO_VERSION}" ]; then
  curl -fL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" -o /tmp/go.tgz
  rm -rf /usr/local/go
  tar -C /usr/local -xzf /tmp/go.tgz
  rm -f /tmp/go.tgz
fi
export PATH="/usr/local/go/bin:${PATH}"

echo "==> [3/7] build native engine + binary"
cd "$APP_SRC"
if [ ! -f third_party/privacy-filter.cpp/CMakeLists.txt ]; then
  echo "    cloning privacy-filter.cpp"
  rm -rf third_party/privacy-filter.cpp
  git clone --recursive "$PF_REPO" third_party/privacy-filter.cpp
fi
make lib
make build
install -D -m 0755 bin/redactor /usr/local/bin/redactor

echo "==> [4/7] service user + directories"
id -u "$SERVICE_USER" >/dev/null 2>&1 || \
  useradd --system --home-dir "$DATA_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
mkdir -p "$MODEL_DIR" "$ETC_DIR"

echo "==> [5/7] model (${MODEL_VARIANT})"
if [ ! -f "${MODEL_DIR}/${MODEL_FILE}" ]; then
  url="https://huggingface.co/LocalAI-io/privacy-filter-GGUF/resolve/main/${MODEL_FILE}?download=true"
  curl -fL --progress-bar -o "${MODEL_DIR}/${MODEL_FILE}.part" "$url"
  mv "${MODEL_DIR}/${MODEL_FILE}.part" "${MODEL_DIR}/${MODEL_FILE}"
else
  echo "    already present: ${MODEL_DIR}/${MODEL_FILE}"
fi
chown -R "${SERVICE_USER}:${SERVICE_USER}" "$DATA_DIR"

echo "==> [6/7] env file + systemd unit"
if [ ! -f "${ETC_DIR}/redactor.env" ]; then
  install -m 0640 "${APP_SRC}/deploy/proxmox/redactor.env.example" "${ETC_DIR}/redactor.env"
  KEY="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  sed -i "s|^REDACTOR_API_KEYS=.*|REDACTOR_API_KEYS=${KEY}|" "${ETC_DIR}/redactor.env"
  sed -i "s|^REDACTOR_MODEL_PATH=.*|REDACTOR_MODEL_PATH=${MODEL_DIR}/${MODEL_FILE}|" "${ETC_DIR}/redactor.env"
  echo
  echo "    *** generated API key: ${KEY}"
  echo "    *** stored in ${ETC_DIR}/redactor.env"
  echo
else
  echo "    keeping existing ${ETC_DIR}/redactor.env"
fi
chown root:"$SERVICE_USER" "${ETC_DIR}/redactor.env"
chmod 0640 "${ETC_DIR}/redactor.env"
install -m 0644 "${APP_SRC}/deploy/proxmox/redactor.service" /etc/systemd/system/redactor.service

echo "==> [7/7] enable + start service"
systemctl daemon-reload
systemctl enable redactor >/dev/null 2>&1 || true
systemctl restart redactor
sleep 2
systemctl --no-pager --full status redactor | head -20 || true

echo
echo "==> done. Health check:"
echo "    curl -fsS http://127.0.0.1:8080/readyz"
