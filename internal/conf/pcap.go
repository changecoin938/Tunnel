package conf

import (
	"fmt"
	"paqet/internal/flog"
)

type PCAP struct {
	Sockbuf   int   `yaml:"sockbuf"`
	Snaplen   int   `yaml:"snaplen"`
	Promisc   *bool `yaml:"promisc"`
	Immediate *bool `yaml:"immediate"`
	TimeoutMs int   `yaml:"timeout_ms"`
}

func (p *PCAP) setDefaults(role string) {
	if p.Sockbuf == 0 {
		if role == "server" {
			p.Sockbuf = 64 * 1024 * 1024
		} else {
			p.Sockbuf = 4 * 1024 * 1024
		}
	}
	if p.Snaplen == 0 {
		p.Snaplen = 65535
	}
	if p.Promisc == nil {
		v := false
		p.Promisc = &v
	}
	if p.Immediate == nil {
		v := true
		p.Immediate = &v
	}
}

func (p *PCAP) validate() []error {
	var errors []error

	if p.Sockbuf < 1024 {
		errors = append(errors, fmt.Errorf("PCAP sockbuf must be >= 1024 bytes"))
	}

	if p.Sockbuf > 256*1024*1024 {
		errors = append(errors, fmt.Errorf("PCAP sockbuf too large (max 256MB)"))
	}
	if p.Snaplen < 64 || p.Snaplen > 65535 {
		errors = append(errors, fmt.Errorf("PCAP snaplen must be between 64-65535"))
	}
	if p.TimeoutMs < 0 || p.TimeoutMs > 60000 {
		errors = append(errors, fmt.Errorf("PCAP timeout_ms must be between 0-60000"))
	}

	// Should be power of 2 for optimal performance, but not required
	if p.Sockbuf&(p.Sockbuf-1) != 0 {
		flog.Warnf("PCAP sockbuf (%d bytes) is not a power of 2 - consider using values like 4MB, 8MB, or 16MB for better performance", p.Sockbuf)
	}

	return errors
}
