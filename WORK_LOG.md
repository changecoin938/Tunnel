# Work Log

This file is updated after each push/release.

## 2026-02-18

### Release v1.0.19
- Pushed commit `58e90f49c5960ec7841f858a9a0b60b766dfe336` to `main`.
- Created tag `v1.0.19`.
- Created GitHub release: <https://github.com/changecoin938/Tunnel/releases/tag/v1.0.19>
- Triggered workflow: <https://github.com/changecoin938/Tunnel/actions/runs/22118787845>

### Release v1.0.20
- Added new root installer `/install.sh` compatible with current clean base config schema.
- Installer now only writes valid fields from current `internal/conf/*` structs.
- Added robust network autodetect (iface/IP/gateway MAC with fallbacks).
- Added release binary download/install flow from GitHub Releases.
- Added server-only iptables bootstrap + systemd `ExecStartPre`/`ExecStopPost` helper.
- Added server/client role-aware config generation and summary output.
- Re-added `._*` ignore to avoid macOS sidecar file pollution.

### Release v1.0.21
- Added reinstall/upgrade-safe flow in `install.sh` (`handle_existing`).
- Installer now stops old service, backs up existing config, and cleans old iptables rules before apply.
- Server reinstall now reuses existing key from old config when available.
- Updated key generation logic to avoid overwriting recovered key.
- Added backup notice in installer summary output.
- Updated execution order to: detect network -> handle existing -> install dependencies -> continue setup.

### Release v1.0.22
- Added resilient download fallback flow in `install.sh` for release assets.
- Download now tries: direct release URL -> GitHub API asset endpoint -> `wget` fallback.
- Added curl timeout controls and explicit failure guidance for blocked CDN environments.
- Added `wget` to dependency install list for apt/yum/apk paths.

### Release v1.0.23
- Improved release selection in `install.sh` to prefer the newest release that actually has an asset for the current architecture.
- Added pre-download asset existence validation using HTTP HEAD checks.
- Added automatic fallback to older releases when the newest tag exists but its assets are not yet published (or workflow failed).
- Kept multi-method download fallback chain (direct URL -> API asset -> wget).

### Release v1.0.24
- Fixed installer-generated `iptables.sh` variable expansion issue in heredoc (escaped runtime variables so `$1` is not expanded by parent script).
- Added safe action parsing in `iptables.sh` with `action="${1:-}"` and empty-arg no-op guard.
- Updated systemd hooks to run helper via `/bin/bash` in `ExecStartPre`/`ExecStopPost`.
- Prevented installer crash path that blocked summary output (client command/key visibility restored).

### Main Push (Post v1.0.24)
- Applied a minimal installer hotfix in `install.sh` based on live server feedback.
- In generated `iptables.sh`, removed `set -euo pipefail` to avoid `$1` nounset crashes in edge invocations.
- Reordered helper variable parsing so `action` is resolved before `PORT` assignment.

### Release v1.0.25
- Client config generation in `install.sh` switched from SOCKS5 to 7-port TCP forward mapping (443/8080/8880/2053/2083/2087/2096).
- Server summary output now prints full client→server port mapping table and Xray local listen reminder (`127.0.0.1:2443-2449`).
- Added new Persian documentation file `README.fa.md` with install guide, port mapping, architecture overview, and common commands.

### Release v1.0.26
- Increased transport defaults for gRPC-heavy deployments:
  - `transport.conn`: `NumCPU()*2` (clamped `2..16`) in `internal/conf/transport.go`.
  - `transport.tcpbuf`: default raised from `8KB` to `128KB` in `internal/conf/transport.go`.
- Increased KCP defaults for higher aggregate throughput in `internal/conf/kcp.go`:
  - `rcvwnd/sndwnd`: `2048/2048` (was `512/512`).
  - `smuxbuf`: `4MB` (was `1MB`).
  - `streambuf`: `256KB` (was `64KB`).
- Updated installer-generated configs in `install.sh` (both server and client):
  - Added explicit `transport.tcpbuf: 131072`.
  - Updated generated `kcp` values to `rcvwnd/sndwnd=2048`, `smuxbuf=4194304`, `streambuf=262144`.
  - Updated generated `conn` logic to `CPU*2` (clamped `2..16`) for parity with code defaults.
- Validation:
  - `go build ./...` passed.
  - `go vet ./...` passed.

### Release v1.0.27
- Updated `install.sh` to use fixed, production-tested tunnel values for server/client generated configs:
  - `conn: 10` (removed dynamic `calc_conn` path, now hardcoded for stable parallelism).
  - `mode: fast3`, `mtu: 1350`.
  - `rcvwnd/sndwnd: 4096/4096`.
  - `smuxbuf: 4194304`, `streambuf: 262144`.
  - `tcpbuf: 131072`.
- Server generated config keeps `pcap.sockbuf: 67108864` (64MB).
- Client generated config changed `pcap.sockbuf` from `4194304` to `8388608` (8MB).
- Installer-generated `log.level` is now explicit `"info"` for both roles.
- Validation:
  - `go build ./...` passed.
  - `go vet ./...` passed.
