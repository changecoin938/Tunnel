#!/usr/bin/env bash
set -euo pipefail

# Helper for systemd ExecStartPre/ExecStopPost.
# Reads paqet config, and if role=server extracts listen port and applies/removes iptables rules.
#
# Usage:
#   paqet-systemd-iptables.sh apply [/etc/paqet/config.yaml]
#   paqet-systemd-iptables.sh remove [/etc/paqet/config.yaml]

action="${1:-}"
config="${2:-/etc/paqet/config.yaml}"

if [[ -z "${action}" ]]; then
  echo "usage: $0 apply|remove [config.yaml]" >&2
  exit 2
fi

case "${action}" in
  apply|remove) ;;
  *) echo "unknown action: ${action}" >&2; exit 2 ;;
esac

if [[ ! -f "${config}" ]]; then
  exit 0
fi

role="$({ grep -E '^[[:space:]]*role:[[:space:]]*' "${config}" || true; } | head -n1 | sed -E 's/^[[:space:]]*role:[[:space:]]*"?([^"[:space:]]+)"?.*$/\1/')"
if [[ "${role}" != "server" ]]; then
  exit 0
fi

# Extract server listen port from lines like:
#   addr: ":9999"
# (we intentionally ignore ipv4.addr like "1.2.3.4:9999")
port="$({ grep -E '^[[:space:]]*addr:[[:space:]]*"?:[0-9]+' "${config}" || true; } | head -n1 | tr -cd 0-9)"

if [[ -z "${port}" ]]; then
  exit 0
fi

# Validate port range.
if ! [[ "${port}" =~ ^[0-9]+$ ]]; then
  exit 0
fi
if (( port < 1 || port > 65535 )); then
  exit 0
fi

# Keep service resilient: don't fail unit if rules can't be applied.
/usr/local/lib/paqet/paqet-iptables.sh "${action}" "${port}" || true
exit 0


