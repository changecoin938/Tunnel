#!/usr/bin/env bash
set -euo pipefail

# One-liner friendly updater + recovery (GitHub tag: stable-latest by default).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/<OWNER>/<REPO>/main/update.sh | sudo bash
#
# Env:
#   PAQET_REPO=OWNER/REPO   (default: changecoin938/Tunnel)
#   PAQET_TAG=tag_name      (default: stable-latest; can set to test-latest)
#   PAQET_SOURCE_REF=ref    (optional: build from source ref, e.g. main)
#   PAQET_SKIP_SYSCTL=1     (skip sysctl safety tuning)
#   PAQET_SKIP_SWAP=1       (skip swap setup)
#   PAQET_SWAP_SIZE=2G      (default: 2G)
#   PAQET_GO_TARBALL_VERSION=1.25.4 (Go toolchain used for source builds)

REPO="${PAQET_REPO:-changecoin938/Tunnel}"
TAG="${PAQET_TAG:-stable-latest}"
SOURCE_REF="${PAQET_SOURCE_REF:-}"
SKIP_SYSCTL="${PAQET_SKIP_SYSCTL:-0}"
SKIP_SWAP="${PAQET_SKIP_SWAP:-0}"
SWAP_SIZE="${PAQET_SWAP_SIZE:-2G}"
GO_TARBALL_VERSION="${PAQET_GO_TARBALL_VERSION:-1.25.4}"

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

ensure_go_for_source_build() {
  if command -v go >/dev/null 2>&1; then
    if go version | grep -Eq 'go1\.(2[5-9]|[3-9][0-9])(\.| )'; then
      return 0
    fi
  fi

  local urls=(
    "https://mirrors.aliyun.com/golang/go${GO_TARBALL_VERSION}.linux-${goarch}.tar.gz"
    "https://go.dev/dl/go${GO_TARBALL_VERSION}.linux-${goarch}.tar.gz?download=1"
    "https://dl.google.com/go/go${GO_TARBALL_VERSION}.linux-${goarch}.tar.gz"
  )

  local ok=0
  for u in "${urls[@]}"; do
    echo "Downloading Go toolchain: ${u}"
    if curl -fL "${u}" -o "${tmp}/go.tgz"; then
      ok=1
      break
    fi
  done
  if [[ "${ok}" != "1" ]]; then
    echo "failed to download Go toolchain ${GO_TARBALL_VERSION}" >&2
    exit 1
  fi

  rm -rf /usr/local/go
  tar -C /usr/local -xzf "${tmp}/go.tgz"
  export PATH="/usr/local/go/bin:${PATH}"
}

detect_bin_path() {
  local b
  b="$(systemctl show paqet -p ExecStart --value 2>/dev/null | awk '{print $1}' | tr -d '\"' || true)"
  if [[ -n "${b}" && -x "${b}" ]]; then
    echo "${b}"
    return 0
  fi
  b="$(command -v paqet 2>/dev/null || true)"
  if [[ -n "${b}" ]]; then
    echo "${b}"
    return 0
  fi
  echo "/usr/local/bin/paqet"
}

install_helpers_from_dir() {
  local src_dir="${1}"
  if [[ -f "${src_dir}/scripts/paqet-ui" ]]; then
    install -m 0755 "${src_dir}/scripts/paqet-ui" /usr/local/bin/paqet-ui
  fi
  if [[ -f "${src_dir}/scripts/paqet-rootcause" ]]; then
    install -m 0755 "${src_dir}/scripts/paqet-rootcause" /usr/local/bin/paqet-rootcause
  fi
  install -d /usr/local/lib/paqet
  if [[ -f "${src_dir}/scripts/paqet-iptables.sh" ]]; then
    install -m 0755 "${src_dir}/scripts/paqet-iptables.sh" /usr/local/lib/paqet/paqet-iptables.sh
  fi
  if [[ -f "${src_dir}/scripts/paqet-systemd-iptables.sh" ]]; then
    install -m 0755 "${src_dir}/scripts/paqet-systemd-iptables.sh" /usr/local/lib/paqet/paqet-systemd-iptables.sh
  fi
}

install_from_source_ref() {
  local ref="${1}"
  apt-get install -y --no-install-recommends git build-essential libpcap-dev >/dev/null 2>&1 || true
  ensure_go_for_source_build

  local repo_url="https://github.com/${REPO}.git"
  local src_dir="${tmp}/src"
  if ! git clone --depth 1 --branch "${ref}" "${repo_url}" "${src_dir}" 2>/dev/null; then
    git clone --depth 1 "${repo_url}" "${src_dir}"
    (
      cd "${src_dir}"
      git fetch --depth 1 origin "${ref}" || true
      git checkout -q "${ref}" || true
    )
  fi

  (
    cd "${src_dir}"
    export PATH="/usr/local/go/bin:${PATH}"
    GOTOOLCHAIN=auto go build -trimpath -o "${tmp}/paqet" ./cmd
  )

  local bin
  bin="$(detect_bin_path)"
  cp -a "${bin}" "${bin}.bak.$(date +%s)" 2>/dev/null || true
  install -m 0755 "${tmp}/paqet" "${bin}"
  install_helpers_from_dir "${src_dir}"

  if command -v systemctl >/dev/null 2>&1; then
    systemctl restart paqet || true
  fi
  "${bin}" version || true
  echo "OK: built from source ref=${ref} (repo=${REPO}) -> ${bin}"
}

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

if [[ -n "${SOURCE_REF}" ]]; then
  install_from_source_ref "${SOURCE_REF}"
  exit 0
fi

url="https://github.com/${REPO}/releases/download/${TAG}/paqet-linux-${goarch}.tar.gz"
echo "Downloading: ${url}"
if ! curl -fsSL "${url}" -o "${tmp}/paqet.tgz"; then
  echo "WARN: release asset download failed for tag=${TAG}; fallback to source ref=main"
  install_from_source_ref "main"
  exit 0
fi

tar -xzf "${tmp}/paqet.tgz" -C "${tmp}"

# Update helper scripts too (important: older paqet-ui versions may re-apply unsafe sysctls).
if [[ -f "${tmp}/scripts/paqet-ui" ]]; then
  install -m 0755 "${tmp}/scripts/paqet-ui" /usr/local/bin/paqet-ui
fi
if [[ -f "${tmp}/scripts/paqet-rootcause" ]]; then
  install -m 0755 "${tmp}/scripts/paqet-rootcause" /usr/local/bin/paqet-rootcause
fi
install -d /usr/local/lib/paqet
if [[ -f "${tmp}/scripts/paqet-iptables.sh" ]]; then
  install -m 0755 "${tmp}/scripts/paqet-iptables.sh" /usr/local/lib/paqet/paqet-iptables.sh
fi
if [[ -f "${tmp}/scripts/paqet-systemd-iptables.sh" ]]; then
  install -m 0755 "${tmp}/scripts/paqet-systemd-iptables.sh" /usr/local/lib/paqet/paqet-systemd-iptables.sh
fi

# Prefer the systemd ExecStart path if possible so we don't accidentally install to a different binary.
BIN="$(detect_bin_path)"

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
