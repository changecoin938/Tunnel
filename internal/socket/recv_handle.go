package socket

import (
	"encoding/binary"
	"fmt"
	"net"
	"paqet/internal/conf"
	"runtime"
	"time"

	"github.com/gopacket/gopacket/pcap"
)

type RecvHandle struct {
	handle *pcap.Handle
}

func NewRecvHandle(cfg *conf.Network) (*RecvHandle, error) {
	handle, err := newHandle(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open pcap handle: %w", err)
	}

	// SetDirection is not fully supported on Windows Npcap, so skip it
	if runtime.GOOS != "windows" {
		if err := handle.SetDirection(pcap.DirectionIn); err != nil {
			return nil, fmt.Errorf("failed to set pcap direction in: %v", err)
		}
	}

	filter := fmt.Sprintf("tcp and dst port %d", cfg.Port)
	if err := handle.SetBPFFilter(filter); err != nil {
		return nil, fmt.Errorf("failed to set BPF filter: %w", err)
	}

	return &RecvHandle{handle: handle}, nil
}

func (h *RecvHandle) Read() ([]byte, net.Addr, error) {
	for {
		data, _, err := h.handle.ZeroCopyReadPacketData()
		if err != nil {
			if err == pcap.NextErrorTimeoutExpired {
				time.Sleep(100 * time.Microsecond)
				continue
			}
			return nil, nil, err
		}

		srcIP, srcPort, payload, ok := parseEtherIPTCP(data)
		if !ok || len(payload) == 0 {
			continue
		}

		addr := &net.UDPAddr{
			IP:   append(net.IP(nil), srcIP...),
			Port: int(srcPort),
		}
		return payload, addr, nil
	}
}

func parseEtherIPTCP(frame []byte) (srcIP []byte, srcPort uint16, payload []byte, ok bool) {
	if len(frame) < 14 {
		return nil, 0, nil, false
	}

	etherType := binary.BigEndian.Uint16(frame[12:14])
	off := 14
	if etherType == 0x8100 || etherType == 0x88A8 {
		if len(frame) < 18 {
			return nil, 0, nil, false
		}
		etherType = binary.BigEndian.Uint16(frame[16:18])
		off += 4
	}

	switch etherType {
	case 0x0800: // IPv4
		if len(frame) < off+20 {
			return nil, 0, nil, false
		}
		ihl := int(frame[off]&0x0F) * 4
		if ihl < 20 || len(frame) < off+ihl {
			return nil, 0, nil, false
		}
		if frame[off+9] != 6 { // TCP
			return nil, 0, nil, false
		}
		src := frame[off+12 : off+16]
		tcpOff := off + ihl
		if len(frame) < tcpOff+20 {
			return nil, 0, nil, false
		}
		dataOff := int(frame[tcpOff+12]>>4) * 4
		if dataOff < 20 || len(frame) < tcpOff+dataOff {
			return nil, 0, nil, false
		}
		sport := binary.BigEndian.Uint16(frame[tcpOff : tcpOff+2])
		return src, sport, frame[tcpOff+dataOff:], true

	case 0x86DD: // IPv6 (no ext header walk)
		if len(frame) < off+40 {
			return nil, 0, nil, false
		}
		if frame[off+6] != 6 { // TCP
			return nil, 0, nil, false
		}
		src := frame[off+8 : off+24]
		tcpOff := off + 40
		if len(frame) < tcpOff+20 {
			return nil, 0, nil, false
		}
		dataOff := int(frame[tcpOff+12]>>4) * 4
		if dataOff < 20 || len(frame) < tcpOff+dataOff {
			return nil, 0, nil, false
		}
		sport := binary.BigEndian.Uint16(frame[tcpOff : tcpOff+2])
		return src, sport, frame[tcpOff+dataOff:], true
	}

	return nil, 0, nil, false
}

func (h *RecvHandle) Close() {
	if h.handle != nil {
		h.handle.Close()
	}
}
