package socket

import (
	"fmt"
	"paqet/internal/conf"
	"runtime"
	"time"

	"github.com/gopacket/gopacket/pcap"
)

func newHandle(cfg *conf.Network) (*pcap.Handle, error) {
	// On Windows, use the GUID field to construct the NPF device name
	// On other platforms, use the interface name directly
	ifaceName := cfg.Interface.Name
	if runtime.GOOS == "windows" {
		ifaceName = cfg.GUID
	}

	inactive, err := pcap.NewInactiveHandle(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to create inactive pcap handle for %s: %v", cfg.Interface.Name, err)
	}
	defer inactive.CleanUp()

	if err = inactive.SetBufferSize(cfg.PCAP.Sockbuf); err != nil {
		return nil, fmt.Errorf("failed to set pcap buffer size to %d: %v", cfg.PCAP.Sockbuf, err)
	}

	if err = inactive.SetSnapLen(cfg.PCAP.Snaplen); err != nil {
		return nil, fmt.Errorf("failed to set pcap snap length: %v", err)
	}
	if err = inactive.SetPromisc(cfg.PCAP.Promisc); err != nil {
		return nil, fmt.Errorf("failed to set promiscuous mode: %v", err)
	}
	timeout := pcap.BlockForever
	if cfg.PCAP.TimeoutMs > 0 {
		timeout = time.Duration(cfg.PCAP.TimeoutMs) * time.Millisecond
	}
	if err = inactive.SetTimeout(timeout); err != nil {
		return nil, fmt.Errorf("failed to set pcap timeout: %v", err)
	}
	immediate := true
	if cfg.PCAP.Immediate != nil {
		immediate = *cfg.PCAP.Immediate
	}
	if err = inactive.SetImmediateMode(immediate); err != nil {
		return nil, fmt.Errorf("failed to set immediate mode: %v", err)
	}

	handle, err := inactive.Activate()
	if err != nil {
		return nil, fmt.Errorf("failed to activate pcap handle on %s: %v", cfg.Interface.Name, err)
	}

	return handle, nil
}
