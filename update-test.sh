#!/usr/bin/env bash
set -euo pipefail

# One-liner friendly updater + recovery for the TEST channel (GitHub tag: test-latest).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/<OWNER>/<REPO>/test/update-test.sh | sudo bash
#
# Env:
#   PAQET_REPO=OWNER/REPO   (default: changecoin938/Tunnel)
#   PAQET_TAG=tag_name      (default: test-latest)
#   PAQET_SKIP_SYSCTL=1     (skip sysctl safety tuning)
#   PAQET_SKIP_SWAP=1       (skip swap setup)
#   PAQET_SWAP_SIZE=2G      (default: 2G)

REPO="${PAQET_REPO:-changecoin938/Tunnel}"
TAG="${PAQET_TAG:-test-latest}"
SKIP_SYSCTL="${PAQET_SKIP_SYSCTL:-0}"
SKIP_SWAP="${PAQET_SKIP_SWAP:-0}"
SWAP_SIZE="${PAQET_SWAP_SIZE:-2G}"

need_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    exec sudo -E bash "$0" "$@"
  fi
}

need_root "$@"

arch="$(uname -m)"
case "${arch}" in
  x86_64|amd64) goarch="amd64" ;;
  aarch64|arm64) goarch="arm64" ;;
  *)
    echo "unsupported arch: ${arch}" >&2
    exit 1
    ;;
esac

export DEBIAN_FRONTEND=noninteractive
if command -v apt-get >/dev/null 2>&1; then
  apt-get update -y
  apt-get install -y --no-install-recommends ca-certificates curl tar libpcap0.8 systemd procps || true
fi

if [[ "${SKIP_SYSCTL}" != "1" ]] && command -v sysctl >/dev/null 2>&1; then
  SYSCTL_CONF="/etc/sysctl.d/99-paqet.conf"
  cat >"${SYSCTL_CONF}" <<'EOF'
# paqet baseline tuning (adjust per host/RAM/NIC)
net.core.rmem_max = 33554432
net.core.wmem_max = 33554432
# IMPORTANT: keep defaults small to avoid TCP kernel OOM when many sockets exist.
net.core.rmem_default = 262144
net.core.wmem_default = 262144
net.ipv4.tcp_rmem = 4096 131072 33554432
net.ipv4.tcp_wmem = 4096 131072 33554432
net.core.netdev_max_backlog = 250000
EOF
  sysctl --system >/dev/null 2>&1 || true
fi

if [[ "${SKIP_SWAP}" != "1" ]] && command -v swapon >/dev/null 2>&1; then
  if ! swapon --show | grep -q .; then
    echo "No swap detected; creating swapfile (${SWAP_SIZE})..."
    if command -v fallocate >/dev/null 2>&1; then
      fallocate -l "${SWAP_SIZE}" /swapfile || true
    fi
    if [[ ! -s /swapfile ]]; then
      # Fallback for filesystems without fallocate support.
      case "${SWAP_SIZE}" in
        *G) dd if=/dev/zero of=/swapfile bs=1M "count=$(( ${SWAP_SIZE%G} * 1024 ))" status=none ;;
        *M) dd if=/dev/zero of=/swapfile bs=1M "count=$(( ${SWAP_SIZE%M} ))" status=none ;;
        *)  dd if=/dev/zero of=/swapfile bs=1M count=2048 status=none ;;
      esac
    fi
    chmod 600 /swapfile
    mkswap /swapfile >/dev/null
    swapon /swapfile
    grep -q '^/swapfile ' /etc/fstab 2>/dev/null || echo '/swapfile none swap sw 0 0' >>/etc/fstab
  fi
fi

tmp="$(mktemp -d)"
cleanup() { rm -rf "${tmp}"; }
trap cleanup EXIT

url="https://github.com/${REPO}/releases/download/${TAG}/paqet-linux-${goarch}.tar.gz"
echo "Downloading: ${url}"
curl -fsSL "${url}" -o "${tmp}/paqet.tgz"

tar -xzf "${tmp}/paqet.tgz" -C "${tmp}"

# Prefer the systemd ExecStart path if possible so we don't accidentally install to a different binary.
BIN="$(systemctl show paqet -p ExecStart --value 2>/dev/null | awk '{print $1}' | tr -d '\"' || true)"
if [[ -z "${BIN}" || ! -x "${BIN}" ]]; then
  BIN="$(command -v paqet 2>/dev/null || true)"
fi
if [[ -z "${BIN}" ]]; then
  BIN="/usr/local/bin/paqet"
fi

src="${tmp}/paqet_linux_${goarch}"
if [[ ! -f "${src}" ]]; then
  echo "missing binary in archive: ${src}" >&2
  exit 1
fi

cp -a "${BIN}" "${BIN}.bak.$(date +%s)" 2>/dev/null || true
install -m 0755 "${src}" "${BIN}"

if command -v systemctl >/dev/null 2>&1; then
  systemctl restart paqet || true
fi

"${BIN}" version || true
echo "OK: updated ${BIN} from ${TAG} (repo=${REPO})"

