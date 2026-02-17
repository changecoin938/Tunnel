#!/usr/bin/env bash
set -euo pipefail

REPO="changecoin938/Tunnel"
BINARY="paqet"
CONFIG_DIR="/etc/paqet"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"
INSTALL_DIR="/usr/local/bin"
SERVICE_NAME="paqet"
PORT=9999
LOG_LEVEL="info"
CONFIG_BACKUP_PATH=""

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

usage() {
    echo "Usage:"
    echo "  Server:  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo bash -s server"
    echo "  Client:  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo bash -s client SERVER_IP KEY"
    exit 1
}

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
    echo -e "${RED}Error: run as root (use sudo)${NC}" >&2
    exit 1
fi

ROLE="${1:-}"
if [[ -z "$ROLE" ]]; then
    usage
fi

if [[ "$ROLE" == "server" ]]; then
    SERVER_IP=""
    KEY=""
elif [[ "$ROLE" == "client" ]]; then
    SERVER_IP="${2:-}"
    KEY="${3:-}"
    if [[ -z "$SERVER_IP" || -z "$KEY" ]]; then
        echo -e "${RED}Error: client mode requires SERVER_IP and KEY${NC}" >&2
        usage
    fi
else
    echo -e "${RED}Error: role must be 'server' or 'client'${NC}" >&2
    usage
fi

detect_arch() {
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        armv7*|armhf) echo "arm32" ;;
        mips) echo "mips" ;;
        mipsel|mipsle) echo "mipsle" ;;
        mips64) echo "mips64" ;;
        mips64el|mips64le) echo "mips64le" ;;
        *)
            echo -e "${RED}Unsupported architecture: $arch${NC}" >&2
            exit 1
            ;;
    esac
}

ARCH=$(detect_arch)
echo -e "${CYAN}Architecture: ${ARCH}${NC}"

detect_network() {
    IFACE=$(ip -4 route show default | awk '{print $5; exit}')
    if [[ -z "${IFACE}" ]]; then
        echo -e "${RED}Error: cannot detect default network interface${NC}" >&2
        exit 1
    fi
    echo -e "${GREEN}Interface: ${IFACE}${NC}"

    LOCAL_IP=$(ip -4 addr show "$IFACE" | grep -oE 'inet [0-9.]+' | awk '{print $2}' | head -1)
    if [[ -z "${LOCAL_IP}" ]]; then
        echo -e "${RED}Error: cannot detect IPv4 address on ${IFACE}${NC}" >&2
        exit 1
    fi
    echo -e "${GREEN}Local IP: ${LOCAL_IP}${NC}"

    GATEWAY_IP=$(ip -4 route show default | awk '{print $3; exit}')
    if [[ -z "${GATEWAY_IP}" ]]; then
        echo -e "${RED}Error: cannot detect gateway IP${NC}" >&2
        exit 1
    fi
    echo -e "${GREEN}Gateway IP: ${GATEWAY_IP}${NC}"

    ROUTER_MAC=""

    ROUTER_MAC=$(ip neigh show "$GATEWAY_IP" dev "$IFACE" 2>/dev/null | awk '{print $5; exit}')

    if [[ -z "$ROUTER_MAC" || "$ROUTER_MAC" == "FAILED" ]]; then
        ping -c 1 -W 1 "$GATEWAY_IP" >/dev/null 2>&1 || true
        sleep 0.5
        ROUTER_MAC=$(ip neigh show "$GATEWAY_IP" dev "$IFACE" 2>/dev/null | awk '{print $5; exit}')
    fi

    if [[ -z "$ROUTER_MAC" || "$ROUTER_MAC" == "FAILED" ]]; then
        if command -v arp >/dev/null 2>&1; then
            ROUTER_MAC=$(arp -n "$GATEWAY_IP" 2>/dev/null | awk '/ether/{print $3; exit}')
        fi
    fi

    if [[ -z "$ROUTER_MAC" || "$ROUTER_MAC" == "FAILED" ]]; then
        ROUTER_MAC=$(grep "$GATEWAY_IP " /proc/net/arp 2>/dev/null | awk '{print $4; exit}')
    fi

    if [[ -z "$ROUTER_MAC" || "$ROUTER_MAC" == "FAILED" || "$ROUTER_MAC" == "00:00:00:00:00:00" ]]; then
        if command -v arping >/dev/null 2>&1; then
            ROUTER_MAC=$(arping -c 1 -I "$IFACE" "$GATEWAY_IP" 2>/dev/null | grep -oE '\[[0-9a-fA-F:]+\]' | tr -d '[]' | head -1)
        fi
    fi

    if [[ -z "$ROUTER_MAC" || "$ROUTER_MAC" == "FAILED" || "$ROUTER_MAC" == "00:00:00:00:00:00" ]]; then
        echo -e "${RED}Error: cannot detect gateway MAC address${NC}" >&2
        echo -e "${YELLOW}Try: ping ${GATEWAY_IP} && ip neigh show ${GATEWAY_IP}${NC}" >&2
        exit 1
    fi

    echo -e "${GREEN}Router MAC: ${ROUTER_MAC}${NC}"
}

handle_existing() {
    local existing=false

    if [[ -f "${INSTALL_DIR}/${BINARY}" ]]; then
        existing=true
        echo -e "${YELLOW}Existing paqet installation detected${NC}"
        "${INSTALL_DIR}/${BINARY}" version 2>/dev/null || true
    fi

    if systemctl is-active "$SERVICE_NAME" >/dev/null 2>&1; then
        echo -e "${YELLOW}Stopping existing service...${NC}"
        systemctl stop "$SERVICE_NAME"
    fi

    if [[ -f "$CONFIG_FILE" ]]; then
        CONFIG_BACKUP_PATH="${CONFIG_FILE}.bak.$(date +%s)"
        cp "$CONFIG_FILE" "$CONFIG_BACKUP_PATH"
        echo -e "${GREEN}Config backed up: ${CONFIG_BACKUP_PATH}${NC}"

        if [[ "$ROLE" == "server" && -z "${KEY:-}" ]]; then
            local old_key
            old_key=$(grep -E '^[[:space:]]*key:[[:space:]]*' "$CONFIG_FILE" 2>/dev/null | head -1 | sed -E "s/^[^:]*:[[:space:]]*//; s/[\"']//g" | tr -d '[:space:]')
            if [[ -n "$old_key" && "$old_key" != "your-secret-key-here" ]]; then
                KEY="$old_key"
                echo -e "${GREEN}Reusing existing key from config${NC}"
            fi
        fi
    fi

    if [[ "$ROLE" == "server" ]]; then
        iptables -t raw -D PREROUTING -p tcp --dport "$PORT" -j NOTRACK 2>/dev/null || true
        iptables -t raw -D OUTPUT -p tcp --sport "$PORT" -j NOTRACK 2>/dev/null || true
        iptables -t mangle -D OUTPUT -p tcp --sport "$PORT" --tcp-flags RST RST -j DROP 2>/dev/null || true
    fi

    if [[ "$existing" == true ]]; then
        echo -e "${GREEN}Old installation cleaned up${NC}"
    fi
}

install_deps() {
    echo -e "${CYAN}Installing dependencies...${NC}"
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update -qq
        apt-get install -y -qq libpcap-dev tar gzip curl wget iproute2 iptables iputils-ping net-tools >/dev/null 2>&1
    elif command -v yum >/dev/null 2>&1; then
        yum install -y -q libpcap-devel tar gzip curl wget iproute iptables iputils net-tools >/dev/null 2>&1
    elif command -v apk >/dev/null 2>&1; then
        apk add --no-cache libpcap-dev tar gzip curl wget iproute2 iptables iputils net-tools >/dev/null 2>&1
    fi
    echo -e "${GREEN}Dependencies installed${NC}"
}

calc_conn() {
    local n
    n=$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null || echo 2)
    if [[ -z "$n" || "$n" -lt 2 ]]; then
        n=2
    fi
    if [[ "$n" -gt 16 ]]; then
        n=16
    fi
    echo "$n"
}

CONN=$(calc_conn)

download_binary() {
    echo -e "${CYAN}Downloading paqet binary...${NC}"

    local filename_base="paqet-linux-${ARCH}"
    local tag=""
    local releases_json
    releases_json=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases?per_page=10" 2>/dev/null || true)

    if [[ -n "$releases_json" ]]; then
        local candidate
        while IFS= read -r candidate; do
            [[ -z "$candidate" ]] && continue
            local check_url="https://github.com/${REPO}/releases/download/${candidate}/${filename_base}-${candidate}.tar.gz"
            if curl -fsSLI --connect-timeout 10 --max-time 20 "$check_url" >/dev/null 2>&1; then
                tag="$candidate"
                break
            fi
        done < <(printf '%s\n' "$releases_json" | grep '"tag_name"' | cut -d'"' -f4)
    fi

    if [[ -z "$tag" ]]; then
        tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | cut -d'"' -f4 || true)
    fi

    if [[ -z "$tag" ]]; then
        echo -e "${RED}Error: cannot find any release${NC}" >&2
        exit 1
    fi

    echo -e "${GREEN}Release: ${tag}${NC}"

    local filename="${filename_base}-${tag}.tar.gz"
    local url="https://github.com/${REPO}/releases/download/${tag}/${filename}"
    local tmp_dir
    tmp_dir=$(mktemp -d)
    local ok=false

    # Validate selected release has the binary asset. If not, walk older releases.
    if ! curl -fsSLI --connect-timeout 10 --max-time 20 "$url" >/dev/null 2>&1; then
        echo -e "${YELLOW}Release ${tag} has no binary, searching older releases...${NC}"
        local found=false
        if [[ -z "$releases_json" ]]; then
            releases_json=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases?per_page=10" 2>/dev/null || true)
        fi
        if [[ -n "$releases_json" ]]; then
            local older_tag
            while IFS= read -r older_tag; do
                [[ -z "$older_tag" || "$older_tag" == "$tag" ]] && continue
                local check="https://github.com/${REPO}/releases/download/${older_tag}/${filename_base}-${older_tag}.tar.gz"
                if curl -fsSLI --connect-timeout 10 --max-time 20 "$check" >/dev/null 2>&1; then
                    tag="$older_tag"
                    filename="${filename_base}-${tag}.tar.gz"
                    url="https://github.com/${REPO}/releases/download/${tag}/${filename}"
                    found=true
                    echo -e "${GREEN}Found working release: ${tag}${NC}"
                    break
                fi
            done < <(printf '%s\n' "$releases_json" | grep '"tag_name"' | cut -d'"' -f4)
        fi
        if [[ "$found" == false ]]; then
            echo -e "${RED}Error: no release has binary for ${ARCH}${NC}" >&2
            rm -rf "$tmp_dir"
            exit 1
        fi
    fi

    echo -e "${CYAN}Downloading: ${url}${NC}"
    if curl -fsSL --connect-timeout 15 --max-time 120 "$url" -o "${tmp_dir}/paqet.tar.gz" 2>/dev/null; then
        ok=true
    fi

    if [[ "$ok" == false ]]; then
        echo -e "${YELLOW}Direct download failed, trying API method...${NC}"
        local asset_url
        asset_url=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/tags/${tag}" 2>/dev/null | awk -v f="\"name\": \"${filename}\"" '
            /"url":/ {u=$0; gsub(/.*"url":[[:space:]]*"/, "", u); gsub(/".*/, "", u)}
            $0 ~ f {print u; exit}
        ')
        if [[ -n "$asset_url" ]]; then
            if curl -fsSL --connect-timeout 15 --max-time 120 -H "Accept: application/octet-stream" "$asset_url" -o "${tmp_dir}/paqet.tar.gz" 2>/dev/null; then
                ok=true
            fi
        fi
    fi

    if [[ "$ok" == false ]] && command -v wget >/dev/null 2>&1; then
        echo -e "${YELLOW}curl failed, trying wget...${NC}"
        if wget -q --timeout=30 "$url" -O "${tmp_dir}/paqet.tar.gz" 2>/dev/null; then
            ok=true
        fi
    fi

    if [[ "$ok" == false ]]; then
        echo -e "${RED}Error: all download methods failed${NC}" >&2
        echo -e "${YELLOW}Try manually: wget ${url}${NC}" >&2
        rm -rf "$tmp_dir"
        exit 1
    fi

    if ! tar -xzf "${tmp_dir}/paqet.tar.gz" -C "$tmp_dir"; then
        echo -e "${RED}Error: invalid release archive${NC}" >&2
        rm -rf "$tmp_dir"
        exit 1
    fi

    local bin_file
    bin_file=$(find "$tmp_dir" -name "paqet_linux_*" -type f | head -1)
    if [[ -z "$bin_file" ]]; then
        bin_file=$(find "$tmp_dir" -name "paqet" -type f | head -1)
    fi

    if [[ -z "$bin_file" ]]; then
        echo -e "${RED}Error: binary not found in tarball${NC}" >&2
        rm -rf "$tmp_dir"
        exit 1
    fi

    systemctl stop "$SERVICE_NAME" 2>/dev/null || true

    mkdir -p "$INSTALL_DIR"
    cp "$bin_file" "${INSTALL_DIR}/${BINARY}"
    chmod +x "${INSTALL_DIR}/${BINARY}"
    rm -rf "$tmp_dir"

    echo -e "${GREEN}Binary installed: ${INSTALL_DIR}/${BINARY}${NC}"
    "${INSTALL_DIR}/${BINARY}" version || true
}

generate_key() {
    if [[ "$ROLE" == "server" && -z "${KEY:-}" ]]; then
        KEY=$("${INSTALL_DIR}/${BINARY}" secret)
        echo -e "${GREEN}Generated NEW key: ${KEY}${NC}"
    elif [[ "$ROLE" == "server" ]]; then
        echo -e "${GREEN}Using existing key: ${KEY}${NC}"
    fi
}

generate_config() {
    echo -e "${CYAN}Generating config...${NC}"
    mkdir -p "$CONFIG_DIR"

    if [[ "$ROLE" == "server" ]]; then
        cat > "$CONFIG_FILE" <<YAML
role: "server"

log:
  level: "${LOG_LEVEL}"

listen:
  addr: ":${PORT}"

network:
  interface: "${IFACE}"
  ipv4:
    addr: "${LOCAL_IP}:${PORT}"
    router_mac: "${ROUTER_MAC}"
  pcap:
    sockbuf: 67108864

transport:
  protocol: "kcp"
  conn: ${CONN}
  kcp:
    mode: "fast"
    block: "aes"
    key: "${KEY}"
    rcvwnd: 512
    sndwnd: 512
    smuxbuf: 1048576
    streambuf: 65536
YAML
    else
        cat > "$CONFIG_FILE" <<YAML
role: "client"

log:
  level: "${LOG_LEVEL}"

socks5:
  - listen: "127.0.0.1:1080"

network:
  interface: "${IFACE}"
  ipv4:
    addr: "${LOCAL_IP}:0"
    router_mac: "${ROUTER_MAC}"
  pcap:
    sockbuf: 4194304

server:
  addr: "${SERVER_IP}:${PORT}"

transport:
  protocol: "kcp"
  conn: ${CONN}
  kcp:
    mode: "fast"
    block: "aes"
    key: "${KEY}"
    rcvwnd: 512
    sndwnd: 512
    smuxbuf: 1048576
    streambuf: 65536
YAML
    fi

    echo -e "${GREEN}Config written: ${CONFIG_FILE}${NC}"
}

setup_iptables() {
    if [[ "$ROLE" != "server" ]]; then
        return
    fi

    echo -e "${CYAN}Configuring iptables...${NC}"

    iptables -t raw -D PREROUTING -p tcp --dport "$PORT" -j NOTRACK 2>/dev/null || true
    iptables -t raw -D OUTPUT -p tcp --sport "$PORT" -j NOTRACK 2>/dev/null || true
    iptables -t mangle -D OUTPUT -p tcp --sport "$PORT" --tcp-flags RST RST -j DROP 2>/dev/null || true

    iptables -t raw -A PREROUTING -p tcp --dport "$PORT" -j NOTRACK
    iptables -t raw -A OUTPUT -p tcp --sport "$PORT" -j NOTRACK
    iptables -t mangle -A OUTPUT -p tcp --sport "$PORT" --tcp-flags RST RST -j DROP

    echo -e "${GREEN}iptables configured for port ${PORT}${NC}"
}

create_service() {
    echo -e "${CYAN}Creating systemd service...${NC}"

    if [[ "$ROLE" == "server" ]]; then
        cat > "${CONFIG_DIR}/iptables.sh" <<IPTSH
#!/usr/bin/env bash
action="\${1:-}"
if [[ -z "\$action" ]]; then exit 0; fi
PORT=${PORT}
if [[ "\$action" == "start" ]]; then
    iptables -t raw -C PREROUTING -p tcp --dport "\$PORT" -j NOTRACK 2>/dev/null || iptables -t raw -A PREROUTING -p tcp --dport "\$PORT" -j NOTRACK
    iptables -t raw -C OUTPUT -p tcp --sport "\$PORT" -j NOTRACK 2>/dev/null || iptables -t raw -A OUTPUT -p tcp --sport "\$PORT" -j NOTRACK
    iptables -t mangle -C OUTPUT -p tcp --sport "\$PORT" --tcp-flags RST RST -j DROP 2>/dev/null || iptables -t mangle -A OUTPUT -p tcp --sport "\$PORT" --tcp-flags RST RST -j DROP
elif [[ "\$action" == "stop" ]]; then
    iptables -t raw -D PREROUTING -p tcp --dport "\$PORT" -j NOTRACK 2>/dev/null || true
    iptables -t raw -D OUTPUT -p tcp --sport "\$PORT" -j NOTRACK 2>/dev/null || true
    iptables -t mangle -D OUTPUT -p tcp --sport "\$PORT" --tcp-flags RST RST -j DROP 2>/dev/null || true
fi
IPTSH
        chmod +x "${CONFIG_DIR}/iptables.sh"
    fi

    local extra_exec=""
    if [[ "$ROLE" == "server" ]]; then
        extra_exec="ExecStartPre=/bin/bash ${CONFIG_DIR}/iptables.sh start
ExecStopPost=/bin/bash ${CONFIG_DIR}/iptables.sh stop"
    fi

    cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=Paqet Tunnel (${ROLE})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
${extra_exec}
ExecStart=${INSTALL_DIR}/${BINARY} run -c ${CONFIG_FILE}
Restart=always
RestartSec=3
LimitNOFILE=65535
LimitNPROC=65535

[Install]
WantedBy=multi-user.target
UNIT

    systemctl daemon-reload
    systemctl enable "$SERVICE_NAME"
    systemctl restart "$SERVICE_NAME"

    echo -e "${GREEN}Service created and started${NC}"
}

print_summary() {
    echo
    echo -e "${CYAN}========================================${NC}"
    echo -e "${GREEN}  Paqet ${ROLE} installed successfully!${NC}"
    echo -e "${CYAN}========================================${NC}"
    echo
    echo -e "  Config:  ${CONFIG_FILE}"
    echo -e "  Binary:  ${INSTALL_DIR}/${BINARY}"
    echo -e "  Service: systemctl status ${SERVICE_NAME}"
    if [[ -n "${CONFIG_BACKUP_PATH}" ]]; then
        echo -e "${YELLOW}  NOTE: Previous config backed up to ${CONFIG_BACKUP_PATH}${NC}"
    fi
    echo

    if [[ "$ROLE" == "server" ]]; then
        echo -e "${YELLOW}Run this on the CLIENT (Iran):${NC}"
        echo
        echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo bash -s client ${LOCAL_IP} ${KEY}"
        echo
        echo -e "${YELLOW}Key: ${KEY}${NC}"
    fi

    echo -e "${CYAN}Useful commands:${NC}"
    echo "  systemctl status ${SERVICE_NAME}"
    echo "  systemctl restart ${SERVICE_NAME}"
    echo "  journalctl -u ${SERVICE_NAME} -f"
    echo

    sleep 1
    systemctl status "$SERVICE_NAME" --no-pager -l 2>/dev/null || true
}

detect_network
handle_existing
install_deps
download_binary
generate_key
generate_config
setup_iptables
create_service
print_summary
