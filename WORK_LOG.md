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
