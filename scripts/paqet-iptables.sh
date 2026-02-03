#!/usr/bin/env bash
set -euo pipefail

# Idempotent iptables rules for paqet (Linux).
# This tool uses pcap/raw injection; the kernel may emit TCP RST and conntrack may interfere.
#
# Usage:
#   paqet-iptables.sh apply <port>
#   paqet-iptables.sh remove <port>

action="${1:-}"
port="${2:-}"

if [[ -z "${action}" || -z "${port}" ]]; then
  echo "usage: $0 apply|remove <port>" >&2
  exit 2
fi

if ! command -v iptables >/dev/null 2>&1; then
  echo "iptables not found" >&2
  exit 1
fi

is_number() { [[ "$1" =~ ^[0-9]+$ ]]; }
if ! is_number "${port}" || (( port < 1 || port > 65535 )); then
  echo "invalid port: ${port}" >&2
  exit 2
fi

iptables_add() {
  local table="$1"; shift
  local chain="$1"; shift
  if iptables -t "${table}" -C "${chain}" "$@" >/dev/null 2>&1; then
    return 0
  fi
  iptables -t "${table}" -A "${chain}" "$@"
}

iptables_del() {
  local table="$1"; shift
  local chain="$1"; shift
  if ! iptables -t "${table}" -C "${chain}" "$@" >/dev/null 2>&1; then
    return 0
  fi
  iptables -t "${table}" -D "${chain}" "$@"
}

apply_rules() {
  # 1) Bypass conntrack for the port (server-side recommended)
  iptables_add raw PREROUTING -p tcp --dport "${port}" -j NOTRACK
  iptables_add raw OUTPUT -p tcp --sport "${port}" -j NOTRACK

  # 2) Drop kernel-generated RST from that port (prevents session disruption)
  iptables_add mangle OUTPUT -p tcp --sport "${port}" --tcp-flags RST RST -j DROP
}

remove_rules() {
  iptables_del mangle OUTPUT -p tcp --sport "${port}" --tcp-flags RST RST -j DROP
  iptables_del raw OUTPUT -p tcp --sport "${port}" -j NOTRACK
  iptables_del raw PREROUTING -p tcp --dport "${port}" -j NOTRACK
}

case "${action}" in
  apply) apply_rules ;;
  remove) remove_rules ;;
  *) echo "unknown action: ${action}" >&2; exit 2 ;;
esac


