#!/usr/bin/env bash
set -euo pipefail

# One-liner friendly installer for Ubuntu/Debian.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/<OWNER>/<REPO>/main/install.sh | sudo bash
#
# Env:
#   PAQET_REPO=OWNER/REPO   (default: current repo name after you fork)

REPO="${PAQET_REPO:-changecoin938/Tunnel}"

need_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    exec sudo -E bash "$0" "$@"
  fi
}

need_root "$@"

if ! command -v apt-get >/dev/null 2>&1; then
  echo "apt-get not found (this installer is for Debian/Ubuntu)." >&2
  exit 1
fi

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
apt-get update -y
apt-get install -y --no-install-recommends \
  ca-certificates curl tar \
  iproute2 iptables \
  whiptail \
  ncurses-bin ncurses-term \
  libpcap0.8 \
  procps \
  systemd

tmp="$(mktemp -d)"
cleanup() { rm -rf "${tmp}"; }
trap cleanup EXIT

url="https://github.com/${REPO}/releases/latest/download/paqet-linux-${goarch}.tar.gz"
echo "Downloading: ${url}"
curl -fsSL "${url}" -o "${tmp}/paqet.tgz"

tar -xzf "${tmp}/paqet.tgz" -C "${tmp}"

install -m 0755 "${tmp}/paqet_linux_${goarch}" /usr/local/bin/paqet

install -d /usr/local/lib/paqet
if [[ -f "${tmp}/scripts/paqet-ui" ]]; then
  install -m 0755 "${tmp}/scripts/paqet-ui" /usr/local/bin/paqet-ui
else
  echo "WARN: scripts/paqet-ui not found in release tarball." >&2
fi
if [[ -f "${tmp}/scripts/paqet-iptables.sh" ]]; then
  install -m 0755 "${tmp}/scripts/paqet-iptables.sh" /usr/local/lib/paqet/paqet-iptables.sh
else
  echo "WARN: scripts/paqet-iptables.sh not found in release tarball." >&2
fi

echo "Installed: /usr/local/bin/paqet"
echo "Launching UI: paqet-ui"

# If this installer was run as a one-liner (`curl | bash`), stdin is not a TTY.
# Re-attach to /dev/tty so the UI can receive arrow keys / scroll input.
if [[ -t 0 && -t 1 && -t 2 ]]; then
  exec /usr/local/bin/paqet-ui
fi
if [[ -r /dev/tty && -w /dev/tty ]]; then
  exec /usr/local/bin/paqet-ui </dev/tty >/dev/tty 2>/dev/tty
fi

echo "No interactive TTY available. Run: sudo paqet-ui" >&2
exit 1


