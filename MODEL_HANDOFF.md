# MODEL_HANDOFF

> This file is read by AI models at the start of every session.
> It tracks architecture, bugs, fixes, and decisions so no context is lost between sessions.

---

## 1. Project Overview

- **Name**: paqet
- **Purpose**: Bidirectional packet-level tunnel using KCP over raw TCP (pcap)
- **Transport**: KCP + smux multiplexing over raw Ethernet frames (gopacket/pcap)
- **Encryption**: AES-128-GCM (KCP layer) + HMAC-SHA256 guard cookies (packet layer)
- **Use case**: 500 concurrent users via gRPC Reality proxy, server 4GB RAM / 4 Core / 10Gbps
- **Go version**: 1.25

## 2. Architecture

```
Client side:                          Server side:
SOCKS5/Forward → client.TCP/UDP() → KCP+smux stream → server.handleStrm() → dial target
                      ↓                                        ↓
               socket.PacketConn                        socket.PacketConn
               (pcap send/recv)                         (pcap send/recv)
                      ↓                                        ↓
              raw Ethernet+IP+TCP                      raw Ethernet+IP+TCP
              with guard cookies                       with guard cookies
```

### Key directories:
- `cmd/` — CLI commands (run, ping, dump, secret, iface, version, status)
- `internal/client/` — Client tunnel logic, connection pool, UDP pool
- `internal/server/` — Server accept loop, stream handlers
- `internal/socket/` — Raw packet I/O via pcap, guard cookies, checksum
- `internal/tnet/kcp/` — KCP+smux transport layer
- `internal/protocol/` — Wire protocol (binary-encoded headers)
- `internal/socks/` — SOCKS5 proxy handlers
- `internal/forward/` — TCP/UDP port forwarding
- `internal/diag/` — Diagnostics, bidirectional copy, retry logic
- `internal/flog/` — Async logging
- `internal/conf/` — YAML config parsing and validation
- `internal/pkg/` — Shared utilities (buffer pools, hash, iterator)

## 3. Bug Tracker

### Open Bugs

- [ ] No currently open bugs from BUG-001..BUG-012 baseline list

### Fixed Bugs

- [x] BUG-001 fixed in `internal/socks/udp_handle.go` (goroutine now owns its own pooled buffer)
- [x] BUG-002 fixed in `internal/socks/udp_handle.go` (deep-copy of Datagram fields and UDPAddr before goroutine capture)
- [x] BUG-003 fixed in `internal/flog/flog.go` + `internal/flog/error.go` (`minLevel` moved to atomic load/store)
- [x] BUG-004 fixed in `internal/tnet/kcp/conn.go` (`Close()` now aggregates real close errors via `errors.Join`)
- [x] BUG-005 fixed in `internal/client/udp.go` (double-check under write lock prevents TOCTOU overwrite/leak)
- [x] BUG-006 fixed in `internal/socket/socket.go` (`sendHandle.Close()` on `NewRecvHandle` failure path)
- [x] BUG-007 fixed in `internal/tnet/kcp/listen.go` (`conn.Close()` when `smux.Server()` fails)
- [x] BUG-008 fixed in `internal/flog/flog.go` (`Fatalf` now drains logger before `os.Exit`)
- [x] BUG-009 fixed in `internal/tnet/kcp/conn.go` (Ping error now reports unexpected protocol type, not `<nil>`)
- [x] BUG-010 fixed in `internal/diag/bidi.go` (`time.After` replaced with reusable `time.NewTimer`)
- [x] BUG-011 fixed in `internal/forward/udp.go` (removed `CloseUDP(0)` on initial stream-creation error path)
- [x] BUG-012 fixed in `internal/flog/flog.go` (logger has explicit stop/drain lifecycle, no close-channel panic path)

## 4. Optimization Tracker

### Safe Optimizations (no throughput impact)

- [x] OPT-001: Replace gob with binary protocol encoding in `protocol/protocol.go`
- [x] OPT-002: Cache `time.Timer` in `socket/socket.go` ReadFrom/WriteTo instead of alloc per call
- [x] OPT-003: 8-byte-at-a-time checksum in `socket/checksum.go`
- [x] OPT-004: Replace `time.After` with `time.NewTimer` + defer Stop in `diag/bidi.go`
- [x] OPT-005: Reduce flog allocations — use `time.AppendFormat` + `fmt.Appendf` instead of double Sprintf
- [x] OPT-006: Cache `LocalAddr()` result in `socket/socket.go`
- [ ] OPT-007: Build SOCKS5 datagram header manually instead of `socks5.NewDatagram`

### Config Tuning (requires user testing)

- [x] CFG-001: Reduce KCP rcvwnd/sndwnd from 16384 to 1024
- [x] CFG-002: Reduce smuxbuf from 8MB to 2MB
- [x] CFG-003: Reduce streambuf from 256KB to 128KB
- [x] CFG-004: Reduce MaxStreamsTotal from 65536 to 4096
- [x] CFG-005: Reduce MaxSessions from 2048 to 512
- [x] CFG-006: Reduce pcap sockbuf from 64MB to 16MB
- [ ] CFG-007: Reduce pcap snaplen from 65535 to 16384
- [x] CFG-008: Consider fast2 mode (interval=20) instead of fast3 (interval=15)

## 5. Code Rules

- Every `PacketConn` / pcap handle MUST be closed in error paths
- Every goroutine MUST be cancellable via context or deadline
- Pooled buffers (`sync.Pool`) MUST NOT be passed to background goroutines
- Copied pointers from caller args MUST be deep-copied before goroutine capture
- All shared variables accessed from multiple goroutines MUST use atomic or mutex
- `Close()` errors MUST be captured, not discarded
- Protocol encoding MUST have bounded message size

## 6. Design Decisions

- **Why pcap instead of TUN/TAP?** — Operates at Ethernet layer, works without root on some setups, supports arbitrary TCP flag manipulation for censorship evasion
- **Why KCP?** — Reliable UDP-like protocol optimized for lossy cross-border links with configurable retransmission
- **Why smux?** — Stream multiplexing over single KCP connection, reduces handshake overhead
- **Why binary protocol? (replaced gob)** — gob had per-stream encoder/decoder alloc overhead + 50-100 bytes type metadata on wire. Binary encoding uses ~10-20 bytes with zero alloc on write (stack buffer)
- **Why guard cookies?** — HMAC-based packet authentication to reject invalid traffic before KCP processing

## 7. Build & Test

```bash
go build ./...
go vet ./...
go test -race ./...
```

## 8. Session Log

### Session: [DATE]
- Analyzed all 80 Go files
- Identified 12 bugs (2 HIGH, 6 MED, 4 LOW)
- Identified 7 safe optimizations + 8 config tunings
- Created this MODEL_HANDOFF.md

### Session: 2026-02-17 (sync)
- Synchronized this file with applied code changes
- Marked BUG-001..BUG-012 as fixed in codebase
- Marked OPT-004 as done
- Marked CFG-001..CFG-006 and CFG-008 as done (CFG-007 intentionally pending)

### Session: 2026-02-17 (optimizations)
- OPT-001: Replaced gob with hand-rolled binary protocol in `protocol/protocol.go` — zero alloc on write, bounded read, ~10x smaller wire format
- OPT-002: Cached `time.Timer` in `socket/socket.go` ReadFrom/WriteTo — eliminates thousands of timer allocs/sec on hot path
- OPT-003: Optimized `socket/checksum.go` to process 8 bytes per iteration — ~4x fewer loop iterations for 1400-byte packets
- OPT-005: Replaced double `fmt.Sprintf` in `flog/flog.go` with `time.AppendFormat` + `fmt.Appendf` — single allocation instead of three
- OPT-006: Cached `LocalAddr()` result in `socket/socket.go` — computed once at construction, no per-call allocation
- All changes verified: `go build ./...` ✓ `go vet ./...` ✓ `go test ./...` ✓
