#!/usr/bin/env bash
set -euo pipefail

# Test-channel installer/updater (uses GitHub pre-release tag: test-latest).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/<OWNER>/<REPO>/test/install-test.sh | sudo bash
#
# Env:
#   PAQET_REPO=OWNER/REPO   (default: changecoin938/Tunnel)
#   PAQET_TAG=tag_name      (default: test-latest)

REPO="${PAQET_REPO:-changecoin938/Tunnel}"
TAG="${PAQET_TAG:-test-latest}"

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
  apt-get install -y --no-install-recommends ca-certificates curl tar libpcap0.8 systemd || true
fi

tmp="$(mktemp -d)"
cleanup() { rm -rf "${tmp}"; }
trap cleanup EXIT

url="https://github.com/${REPO}/releases/download/${TAG}/paqet-linux-${goarch}.tar.gz"
echo "Downloading: ${url}"
curl -fsSL "${url}" -o "${tmp}/paqet.tgz"

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
echo "OK: installed ${BIN} from ${TAG}"
