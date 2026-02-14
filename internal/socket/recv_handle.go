package socket

import (
	"encoding/binary"
	"fmt"
	"net"
	"paqet/internal/conf"
	"runtime"
	"sync"

	"github.com/gopacket/gopacket/pcap"
)

type RecvHandle struct {
	handle *pcap.Handle

	mu           sync.RWMutex
	addrCache    map[addrKey]*net.UDPAddr
	addrCacheOld map[addrKey]*net.UDPAddr
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

	return &RecvHandle{
		handle:    handle,
		addrCache: make(map[addrKey]*net.UDPAddr, 1024),
	}, nil
}

func (h *RecvHandle) Read() ([]byte, net.Addr, error) {
	for {
		data, _, err := h.handle.ZeroCopyReadPacketData()
		if err != nil {
			// If a finite timeout is configured on the pcap handle, libpcap returns
			// NextErrorTimeoutExpired when no packets arrive within the window.
			// This is NOT fatal; keep waiting.
			if err == pcap.NextErrorTimeoutExpired {
				continue
			}
			return nil, nil, err
		}

		srcIP, srcPort, payload, ok := parseEtherIPTCP(data)
		if !ok || len(payload) == 0 {
			continue
		}

		addr := h.getAddr(srcIP, srcPort)
		return payload, addr, nil
	}
}

func (h *RecvHandle) Close() {
	if h.handle != nil {
		h.handle.Close()
	}
}

// addrKey is an allocation-free map key for caching *net.UDPAddr objects.
// It bounds per-packet allocations when packets arrive from the same peer.
type addrKey struct {
	ip   [16]byte
	port uint16
	v4   bool
}

const maxAddrCache = 65536

func (h *RecvHandle) getAddr(srcIP []byte, srcPort uint16) *net.UDPAddr {
	var k addrKey
	k.port = srcPort
	if len(srcIP) == 4 {
		k.v4 = true
		copy(k.ip[:4], srcIP)
	} else {
		copy(k.ip[:], srcIP)
	}

	h.mu.RLock()
	if a := h.addrCache[k]; a != nil {
		h.mu.RUnlock()
		return a
	}
	if h.addrCacheOld != nil {
		if a := h.addrCacheOld[k]; a != nil {
			h.mu.RUnlock()
			// Promote to hot cache.
			h.mu.Lock()
			if b := h.addrCache[k]; b != nil {
				h.mu.Unlock()
				return b
			}
			if old := h.addrCacheOld; old != nil {
				if b := old[k]; b != nil {
					h.addrCache[k] = b
					delete(old, k)
					h.mu.Unlock()
					return b
				}
			}
			h.mu.Unlock()
			// Fall through to allocation path.
			goto alloc
		}
	}
	h.mu.RUnlock()

	// Create a stable copy (pcap buffers are reused).
alloc:
	var ipCopy net.IP
	if k.v4 {
		ipCopy = make(net.IP, 4)
		copy(ipCopy, srcIP[:4])
	} else {
		ipCopy = make(net.IP, 16)
		copy(ipCopy, srcIP[:16])
	}
	addr := &net.UDPAddr{IP: ipCopy, Port: int(srcPort)}

	h.mu.Lock()
	// If we got flooded with unique spoofed sources, keep memory bounded.
	if len(h.addrCache) >= maxAddrCache {
		h.addrCacheOld = h.addrCache
		h.addrCache = make(map[addrKey]*net.UDPAddr, 1024)
	}
	if a := h.addrCache[k]; a != nil {
		h.mu.Unlock()
		return a
	}
	h.addrCache[k] = addr
	h.mu.Unlock()

	return addr
}

// parseEtherIPTCP parses an Ethernet frame carrying IPv4/IPv6+TCP and returns:
// - srcIP: 4 or 16 bytes (a view into the provided frame)
// - srcPort: TCP source port
// - payload: TCP payload (a view into the provided frame)
//
// It supports 802.1Q VLAN tags. For uncommon IPv6 extension chains, it may return ok=false.
func parseEtherIPTCP(frame []byte) (srcIP []byte, srcPort uint16, payload []byte, ok bool) {
	const (
		etherHdrLen  = 14
		ethIPv4      = 0x0800
		ethIPv6      = 0x86DD
		ethVLAN      = 0x8100
		ethQinQ      = 0x88A8
		ipProtoTCP   = 6
		ipv4MinHdr   = 20
		ipv6HdrLen   = 40
		tcpMinHdrLen = 20
	)

	if len(frame) < etherHdrLen {
		return nil, 0, nil, false
	}
	off := etherHdrLen
	etherType := binary.BigEndian.Uint16(frame[12:14])
	if etherType == ethVLAN || etherType == ethQinQ {
		// VLAN tag: TCI(2) + encapsulated ethertype(2)
		if len(frame) < etherHdrLen+4 {
			return nil, 0, nil, false
		}
		etherType = binary.BigEndian.Uint16(frame[16:18])
		off += 4
	}

	switch etherType {
	case ethIPv4:
		if len(frame) < off+ipv4MinHdr {
			return nil, 0, nil, false
		}
		ihl := int(frame[off]&0x0F) * 4
		if ihl < ipv4MinHdr || len(frame) < off+ihl {
			return nil, 0, nil, false
		}
		if frame[off+9] != ipProtoTCP {
			return nil, 0, nil, false
		}
		src := frame[off+12 : off+16]
		tcpOff := off + ihl
		if len(frame) < tcpOff+tcpMinHdrLen {
			return nil, 0, nil, false
		}
		dataOff := int(frame[tcpOff+12]>>4) * 4
		if dataOff < tcpMinHdrLen || len(frame) < tcpOff+dataOff {
			return nil, 0, nil, false
		}
		sport := binary.BigEndian.Uint16(frame[tcpOff : tcpOff+2])
		return src, sport, frame[tcpOff+dataOff:], true

	case ethIPv6:
		if len(frame) < off+ipv6HdrLen {
			return nil, 0, nil, false
		}
		next := frame[off+6]
		src := frame[off+8 : off+24]
		tcpOff := off + ipv6HdrLen

		// Best-effort skip common extension headers.
		for {
			switch next {
			case ipProtoTCP:
				if len(frame) < tcpOff+tcpMinHdrLen {
					return nil, 0, nil, false
				}
				dataOff := int(frame[tcpOff+12]>>4) * 4
				if dataOff < tcpMinHdrLen || len(frame) < tcpOff+dataOff {
					return nil, 0, nil, false
				}
				sport := binary.BigEndian.Uint16(frame[tcpOff : tcpOff+2])
				return src, sport, frame[tcpOff+dataOff:], true

			// Hop-by-Hop (0), Routing (43), Destination Options (60)
			case 0, 43, 60:
				if len(frame) < tcpOff+2 {
					return nil, 0, nil, false
				}
				extNext := frame[tcpOff]
				extLen := int(frame[tcpOff+1]+1) * 8
				if len(frame) < tcpOff+extLen {
					return nil, 0, nil, false
				}
				next = extNext
				tcpOff += extLen
				continue

			// Fragment (44) is always 8 bytes.
			case 44:
				if len(frame) < tcpOff+8 {
					return nil, 0, nil, false
				}
				next = frame[tcpOff]
				tcpOff += 8
				continue

			// AH (51): length is in 4-byte units, not counting first 2 units.
			case 51:
				if len(frame) < tcpOff+2 {
					return nil, 0, nil, false
				}
				extNext := frame[tcpOff]
				extLen := (int(frame[tcpOff+1]) + 2) * 4
				if len(frame) < tcpOff+extLen {
					return nil, 0, nil, false
				}
				next = extNext
				tcpOff += extLen
				continue

			default:
				// Unknown/unsupported extension chain.
				return nil, 0, nil, false
			}
		}

	default:
		return nil, 0, nil, false
	}
}
