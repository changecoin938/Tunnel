package conf

import (
	"fmt"
	"paqet/internal/flog"
)

type PCAP struct {
	Sockbuf   int   `yaml:"sockbuf"`
	Snaplen   int   `yaml:"snaplen"`
	Promisc   bool  `yaml:"promisc"`
	Immediate *bool `yaml:"immediate"`
	TimeoutMs int   `yaml:"timeout_ms"`
}

func (p *PCAP) setDefaults(role string) {
	if p.Sockbuf == 0 {
		if role == "server" {
			p.Sockbuf = 8 * 1024 * 1024
		} else {
			p.Sockbuf = 4 * 1024 * 1024
		}
	}
	if p.Snaplen == 0 {
		// Bounds per-packet capture size. Keep this large enough to safely capture
		// GRO/LRO-coalesced TCP payloads on some kernels/NICs; otherwise libpcap may
		// truncate frames and corrupt higher-layer packets.
		p.Snaplen = 65535
	}
	// Default to non-promiscuous capture (lower overhead). This tunnel only needs
	// traffic destined for this host anyway (BPF filters further narrow it down).
	//
	// If you run in an unusual L2 environment and packets aren't captured, set:
	//   network.pcap.promisc: true
	//
	// Promisc default is false, so only set it if you know you need it.

	// Immediate mode trades CPU for lower latency. Keep current behavior by default.
	if p.Immediate == nil {
		v := true
		p.Immediate = &v
	}
	// Timeout controls buffering when immediate=false. 0 means BlockForever (pcap default).
}

func (p *PCAP) validate() []error {
	var errors []error

	if p.Sockbuf < 1024 {
		errors = append(errors, fmt.Errorf("PCAP sockbuf must be >= 1024 bytes"))
	}

	if p.Sockbuf > 100*1024*1024 {
		errors = append(errors, fmt.Errorf("PCAP sockbuf too large (max 100MB)"))
	}
	if p.Snaplen < 256 {
		errors = append(errors, fmt.Errorf("PCAP snaplen must be >= 256 bytes"))
	}
	if p.Snaplen > 65536 {
		errors = append(errors, fmt.Errorf("PCAP snaplen too large (max 65536)"))
	}
	if p.TimeoutMs < 0 || p.TimeoutMs > 60_000 {
		errors = append(errors, fmt.Errorf("PCAP timeout_ms must be between 0-60000"))
	}

	// Should be power of 2 for optimal performance, but not required
	if p.Sockbuf&(p.Sockbuf-1) != 0 {
		flog.Warnf("PCAP sockbuf (%d bytes) is not a power of 2 - consider using values like 4MB, 8MB, or 16MB for better performance", p.Sockbuf)
	}

	return errors
}
