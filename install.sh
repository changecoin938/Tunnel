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

# Optional command mode:
#   curl -fsSL https://raw.githubusercontent.com/<OWNER>/<REPO>/main/install.sh | sudo bash -s -- purge
# This removes paqet service, configs, sysctl tuning, iptables rules (best-effort), and binaries.
if [[ "${1:-}" == "purge" || "${1:-}" == "uninstall" ]]; then
  SERVICE="paqet"
  SERVICE_FILE="/etc/systemd/system/${SERVICE}.service"
  CONFIG_DIR="/etc/paqet"
  CONFIG_FILE="${CONFIG_DIR}/config.yaml"
  SYSCTL_CONF="/etc/sysctl.d/99-paqet.conf"

  WATCHDOG_SCRIPT="/usr/local/bin/paqet-watchdog"
  WATCHDOG_SERVICE_FILE="/etc/systemd/system/paqet-watchdog.service"
  WATCHDOG_TIMER_FILE="/etc/systemd/system/paqet-watchdog.timer"

  BIN="/usr/local/bin/paqet"
  UI="/usr/local/bin/paqet-ui"
  ROOTCAUSE="/usr/local/bin/paqet-rootcause"
  LIB_DIR="/usr/local/lib/paqet"
  IPT_SH="${LIB_DIR}/paqet-iptables.sh"
  IPT_SYSTEMD_SH="${LIB_DIR}/paqet-systemd-iptables.sh"

  # Stop/disable service (best-effort)
  if command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now paqet-watchdog.timer 2>/dev/null || true
    systemctl stop "${SERVICE}" 2>/dev/null || true
    systemctl disable "${SERVICE}" 2>/dev/null || true
  fi

  # Remove iptables rules (best-effort, server role only)
  if [[ -x "${IPT_SYSTEMD_SH}" && -f "${CONFIG_FILE}" ]]; then
    "${IPT_SYSTEMD_SH}" remove "${CONFIG_FILE}" || true
  elif [[ -x "${IPT_SH}" && -f "${CONFIG_FILE}" ]]; then
    # Fallback: attempt to parse listen port from addr: ":PORT"
    port="$(grep -E '^[[:space:]]*addr:[[:space:]]*"?:[0-9]+' "${CONFIG_FILE}" | head -n1 | tr -cd 0-9 || true)"
    if [[ -n "${port}" ]]; then
      "${IPT_SH}" remove "${port}" || true
    fi
  fi

  # Remove files
  rm -f "${SERVICE_FILE}" || true
  rm -f "${WATCHDOG_TIMER_FILE}" "${WATCHDOG_SERVICE_FILE}" "${WATCHDOG_SCRIPT}" || true
  if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload 2>/dev/null || true
    systemctl reset-failed "${SERVICE}" 2>/dev/null || true
  fi

  rm -rf "${CONFIG_DIR}" || true
  rm -f "${SYSCTL_CONF}" || true
  if command -v sysctl >/dev/null 2>&1; then
    sysctl --system >/dev/null 2>&1 || true
  fi

  rm -f "${BIN}" "${UI}" || true
  rm -f "${ROOTCAUSE}" || true
  rm -rf "${LIB_DIR}" || true

  echo "OK: paqet purged."
  exit 0
fi

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
if [[ -f "${tmp}/scripts/paqet-rootcause" ]]; then
  install -m 0755 "${tmp}/scripts/paqet-rootcause" /usr/local/bin/paqet-rootcause
else
  echo "WARN: scripts/paqet-rootcause not found in release tarball." >&2
fi
if [[ -f "${tmp}/scripts/paqet-iptables.sh" ]]; then
  install -m 0755 "${tmp}/scripts/paqet-iptables.sh" /usr/local/lib/paqet/paqet-iptables.sh
else
  echo "WARN: scripts/paqet-iptables.sh not found in release tarball." >&2
fi
if [[ -f "${tmp}/scripts/paqet-systemd-iptables.sh" ]]; then
  install -m 0755 "${tmp}/scripts/paqet-systemd-iptables.sh" /usr/local/lib/paqet/paqet-systemd-iptables.sh
else
  echo "WARN: scripts/paqet-systemd-iptables.sh not found in release tarball." >&2
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
