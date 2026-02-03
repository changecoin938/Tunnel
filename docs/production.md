# Production runbook (high-throughput + stability)

This is a practical checklist to run `paqet` under sustained high traffic. Tune based on *measurements* (CPU, drops, retransmits, RTT/loss).

> This project uses `pcap` + raw injection. Many classic TCP tuning knobs are less relevant, but NIC/IRQ/socket buffer/limits still matter.

## Host prerequisites

- Run on a **dedicated** host/VM with stable CPU frequency (avoid aggressive power saving).
- Ensure NIC capacity is realistic for your target volume (e.g. multi-Gbps requires enough cores and PCIe bandwidth).
- Use a recent kernel + drivers; keep `libpcap` up to date.

## OS limits

- **File descriptors**: increase `ulimit -n` (server and client). High stream counts need high fd limits.
- **Process limits**: ensure enough threads/process limits for Go runtime and networking.

## NIC & IRQ (Linux)

- **Ring buffers**: increase RX/TX ring sizes (driver dependent).
- **IRQ affinity / RSS**: ensure interrupts are spread across cores; avoid a single-core bottleneck.
- **RPS/RFS**: consider enabling Receive Packet Steering for high PPS workloads.
- **Offloads**: test with/without GRO/LRO/TSO depending on your environment. Validate via metrics and packet drops.

## sysctl (Linux) – common high-throughput baselines

These are *starting points* (values depend on RAM/NIC):

- **Socket buffers**:
  - `net.core.rmem_max`
  - `net.core.wmem_max`
  - `net.core.rmem_default`
  - `net.core.wmem_default`
- **Backlog**:
  - `net.core.netdev_max_backlog`
- **Busy polling** (optional, latency/CPU trade-off):
  - `net.core.busy_poll`
  - `net.core.busy_read`

Apply changes carefully and confirm with `dmesg`, drops counters, and pprof.

## paqet configuration knobs

### PCAP buffer

- `network.pcap.sockbuf`: increase on servers under high PPS. Typical values: 8MB → 16MB/32MB.

### KCP tuning

- `transport.conn`: use multiple parallel connections to scale across cores (measure!).
- `transport.kcp.rcvwnd` / `sndwnd`: increase for higher bandwidth-delay product networks.
- `transport.kcp.mtu`: keep below path MTU (1350 is usually safe).
- `transport.kcp.mode`: `fast2/fast3` can reduce latency but may increase CPU/bandwidth.
- `transport.kcp.smuxbuf` / `streambuf`: increase if you see backpressure (but watch memory).

### Hardening / disruption resistance

- `transport.kcp.guard`: keep enabled unless you explicitly want a keyless setup.
- `transport.kcp.max_sessions`, `max_streams_total`, `max_streams_per_session`: cap resource usage to survive spikes.
- `transport.kcp.header_timeout`: keeps stalled stream setup from pinning resources.

## Observability

- Enable `pprof` via `debug.pprof: "127.0.0.1:6060"` and capture:
  - CPU profile under load
  - heap profile after long runs
- Track kernel counters for drops (NIC and pcap), plus Go GC stats.


