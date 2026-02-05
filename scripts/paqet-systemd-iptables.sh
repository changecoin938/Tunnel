#!/usr/bin/env bash
set -euo pipefail

# Helper for systemd ExecStartPre/ExecStopPost.
# Reads paqet config and applies/removes iptables rules to prevent kernel/conntrack interference.
# - server: uses listen.addr (":PORT")
# - client: uses network.ipv4.addr / network.ipv6.addr ("IP:PORT") if port != 0
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
port=""
case "${role}" in
  server)
    # Extract server listen port from lines like:
    #   addr: ":9999"
    # (we intentionally ignore network.ipv4.addr like "1.2.3.4:9999")
    port="$({ grep -E '^[[:space:]]*addr:[[:space:]]*"?:[0-9]+' "${config}" || true; } | head -n1 | tr -cd 0-9)"
    ;;
  client)
    # Extract client local port from:
    #   network:
    #     ipv4:
    #       addr: "1.2.3.4:20000"
    # (or ipv6 addr: "[::1]:20000")
    client_addr="$(
      awk '
        BEGIN { in_net=0; in_ip=0 }
        /^[[:space:]]*network:[[:space:]]*$/ { in_net=1; next }
        in_net && /^[^[:space:]][^:]*:/ { in_net=0; in_ip=0 }
        in_net && /^[[:space:]]*ipv4:[[:space:]]*$/ { in_ip=1; next }
        in_net && /^[[:space:]]*ipv6:[[:space:]]*$/ { in_ip=1; next }
        in_net && in_ip && /^[[:space:]]*addr:[[:space:]]*/ {
          line=$0
          sub(/^[[:space:]]*addr:[[:space:]]*/, "", line)
          sub(/[[:space:]]+#.*/, "", line)
          gsub(/^"/, "", line); gsub(/"$/, "", line)
          print line
          exit
        }
      ' "${config}" 2>/dev/null || true
    )"
    if [[ -n "${client_addr}" ]]; then
      port="${client_addr##*:}"
      port="${port//[^0-9]/}"
    fi
    # If port is 0 (meaning "random"), skip iptables rules for client.
    if [[ "${port}" == "0" ]]; then
      exit 0
    fi
    ;;
  *)
    exit 0
    ;;
esac

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


