#!/usr/bin/env bash
set -euo pipefail

# Binary-only updater (no source build on servers).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/<OWNER>/<REPO>/main/update.sh | sudo bash
#
# Env:
#   PAQET_REPO=OWNER/REPO     (default: changecoin938/Tunnel)
#   PAQET_TAG=latest          (default: latest; or stable-latest/test-latest/vX.Y.Z)
#   PAQET_SERVICE=paqet       (default: paqet)

REPO="${PAQET_REPO:-changecoin938/Tunnel}"
TAG="${PAQET_TAG:-latest}"
SERVICE="${PAQET_SERVICE:-paqet}"

need_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    exec sudo -E bash "$0" "$@"
  fi
}

need_root "$@"

arch="$(uname -m)"
case "${arch}" in
  x86_64|amd64)
    goarch="amd64"
    asset_raw="paqet-linux-amd64"
    asset_tgz="paqet-linux-amd64.tar.gz"
    asset_in_tgz="paqet_linux_amd64"
    ;;
  aarch64|arm64)
    goarch="arm64"
    asset_raw="paqet-linux-arm64"
    asset_tgz="paqet-linux-arm64.tar.gz"
    asset_in_tgz="paqet_linux_arm64"
    ;;
  *)
    echo "unsupported arch: ${arch}" >&2
    exit 1
    ;;
esac

detect_bin_path() {
  local b
  b="$(systemctl show "${SERVICE}" -p ExecStart --value 2>/dev/null | awk '{print $1}' | tr -d '"' || true)"
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

is_elf() {
  local f="$1"
  if command -v file >/dev/null 2>&1; then
    file -b "${f}" | grep -q "ELF"
    return
  fi
  [[ "$(head -c4 "${f}" | od -An -t x1 | tr -d ' \n')" == "7f454c46" ]]
}

download() {
  local url="$1"
  local out="$2"
  curl -fL --retry 3 --retry-delay 1 --connect-timeout 10 --max-time 300 "${url}" -o "${out}"
}

tmp="$(mktemp -d)"
cleanup() { rm -rf "${tmp}"; }
trap cleanup EXIT

if [[ "${TAG}" == "latest" ]]; then
  raw_url="https://github.com/${REPO}/releases/latest/download/${asset_raw}"
  tgz_url="https://github.com/${REPO}/releases/latest/download/${asset_tgz}"
else
  raw_url="https://github.com/${REPO}/releases/download/${TAG}/${asset_raw}"
  tgz_url="https://github.com/${REPO}/releases/download/${TAG}/${asset_tgz}"
fi

echo "Downloading: ${raw_url}"
bin_candidate="${tmp}/paqet"
if ! download "${raw_url}" "${bin_candidate}"; then
  echo "raw asset unavailable, trying tarball: ${tgz_url}"
  tgz_path="${tmp}/paqet.tgz"
  download "${tgz_url}" "${tgz_path}"
  tar -xzf "${tgz_path}" -C "${tmp}"

  if [[ -f "${tmp}/${asset_raw}" ]]; then
    bin_candidate="${tmp}/${asset_raw}"
  elif [[ -f "${tmp}/${asset_in_tgz}" ]]; then
    bin_candidate="${tmp}/${asset_in_tgz}"
  else
    found="$(find "${tmp}" -maxdepth 3 -type f \( -name "${asset_raw}" -o -name "${asset_in_tgz}" \) | head -n1 || true)"
    if [[ -z "${found}" ]]; then
      echo "could not find binary in tarball (${asset_raw} or ${asset_in_tgz})" >&2
      exit 1
    fi
    bin_candidate="${found}"
  fi
fi

if ! is_elf "${bin_candidate}"; then
  echo "downloaded file is not an ELF binary: ${bin_candidate}" >&2
  exit 1
fi

BIN="$(detect_bin_path)"
cp -a "${BIN}" "${BIN}.bak.$(date +%s)" 2>/dev/null || true
install -m 0755 "${bin_candidate}" "${BIN}"

if command -v systemctl >/dev/null 2>&1; then
  systemctl restart "${SERVICE}" || true
fi

"${BIN}" version || true
echo "OK: updated ${BIN} from repo=${REPO} tag=${TAG} arch=${goarch}"
