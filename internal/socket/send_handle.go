package socket

import (
	"encoding/binary"
	"fmt"
	"net"
	"paqet/internal/conf"
	"paqet/internal/pkg/hash"
	"paqet/internal/pkg/iterator"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopacket/gopacket/pcap"
)

type TCPF struct {
	tcpF       iterator.Iterator[conf.TCPF]
	clientTCPF map[uint64]*iterator.Iterator[conf.TCPF]
	mu         sync.RWMutex
}

type SendHandle struct {
	handle      *pcap.Handle
	srcMAC      net.HardwareAddr
	srcIPv4     net.IP
	srcIPv4RHWA net.HardwareAddr
	srcIPv6     net.IP
	srcIPv6RHWA net.HardwareAddr
	srcPort     uint16
	time        uint32
	tsCounter   uint32
	tcpF        TCPF
	framePool   sync.Pool
	optPool     sync.Pool
}

type tcpOptBuf struct {
	// SYN opts (20 bytes):
	//  MSS(4) + SACKPermitted(2) + TS(10) + NOP(1) + WindowScale(3)
	//    2,4,0x05,0xb4, 4,2, 8,10, tsVal(4), tsEcr(4), 1, 3,3,8
	syn [20]byte
	// ACK opts (12 bytes): NOP(1) + NOP(1) + TS(10)
	//    1,1, 8,10, tsVal(4), tsEcr(4)
	ack [12]byte
}

func newTCPOptBuf() *tcpOptBuf {
	b := &tcpOptBuf{}
	b.syn = [20]byte{
		2, 4, 0x05, 0xB4, // MSS = 1460
		4, 2, // SACK permitted
		8, 10, // Timestamps
		0, 0, 0, 0, // tsval
		0, 0, 0, 0, // tsecr
		1,       // NOP
		3, 3, 8, // Window scale = 8
	}
	b.ack = [12]byte{
		1, 1, // NOP, NOP
		8, 10, // Timestamps
		0, 0, 0, 0, // tsval
		0, 0, 0, 0, // tsecr
	}
	return b
}

func NewSendHandle(cfg *conf.Network) (*SendHandle, error) {
	handle, err := newHandle(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open pcap handle: %w", err)
	}

	// SetDirection is not fully supported on Windows Npcap, so skip it
	if runtime.GOOS != "windows" {
		if err := handle.SetDirection(pcap.DirectionOut); err != nil {
			return nil, fmt.Errorf("failed to set pcap direction out: %v", err)
		}
	}

	sh := &SendHandle{
		handle:  handle,
		srcMAC:  cfg.Interface.HardwareAddr,
		srcPort: uint16(cfg.Port),
		tcpF:    TCPF{tcpF: iterator.Iterator[conf.TCPF]{Items: cfg.TCP.LF}, clientTCPF: make(map[uint64]*iterator.Iterator[conf.TCPF])},
		time:    uint32(time.Now().UnixNano() / int64(time.Millisecond)),
		framePool: sync.Pool{
			New: func() any {
				return make([]byte, 0, 2048)
			},
		},
		optPool: sync.Pool{
			New: func() any {
				return newTCPOptBuf()
			},
		},
	}
	if cfg.IPv4.Addr != nil {
		sh.srcIPv4 = cfg.IPv4.Addr.IP
		sh.srcIPv4RHWA = cfg.IPv4.Router
	}
	if cfg.IPv6.Addr != nil {
		sh.srcIPv6 = cfg.IPv6.Addr.IP
		sh.srcIPv6RHWA = cfg.IPv6.Router
	}
	return sh, nil
}

func (h *SendHandle) Write(payload []byte, addr *net.UDPAddr) error {
	return h.WriteParts(nil, payload, addr)
}

// WriteParts writes prefix+payload as a single TCP segment payload, without allocating
// a combined buffer. This is used by the KCP guard fast-path.
func (h *SendHandle) WriteParts(prefix []byte, payload []byte, addr *net.UDPAddr) error {
	if addr == nil {
		return fmt.Errorf("invalid destination address")
	}

	opt := h.optPool.Get().(*tcpOptBuf)
	frame := h.framePool.Get().([]byte)
	defer func() {
		h.optPool.Put(opt)
		frame = frame[:0]
		h.framePool.Put(frame)
	}()

	dstIP := addr.IP
	dstPort := uint16(addr.Port)

	f := h.getClientTCPF(dstIP, dstPort)

	// Timestamp + seq/ack generation (keep behavior identical to previous gopacket path).
	counter := atomic.AddUint32(&h.tsCounter, 1)
	tsVal := h.time + (counter >> 3)

	var seq, ack uint32
	var opts []byte
	if f.SYN {
		binary.BigEndian.PutUint32(opt.syn[8:12], tsVal)
		binary.BigEndian.PutUint32(opt.syn[12:16], 0)
		opts = opt.syn[:]
		seq = 1 + (counter & 0x7)
		ack = 0
		if f.ACK {
			ack = seq + 1
		}
	} else {
		tsEcr := tsVal - (counter%200 + 50)
		binary.BigEndian.PutUint32(opt.ack[4:8], tsVal)
		binary.BigEndian.PutUint32(opt.ack[8:12], tsEcr)
		opts = opt.ack[:]
		seq = h.time + (counter << 7)
		ack = seq - (counter & 0x3FF) + 1400
	}

	tcpHdrLen := 20 + len(opts)
	payloadLen := len(prefix) + len(payload)
	tcpLen := tcpHdrLen + payloadLen

	if dst4 := dstIP.To4(); dst4 != nil {
		src4 := h.srcIPv4.To4()
		if src4 == nil || len(h.srcIPv4RHWA) != 6 || len(h.srcMAC) != 6 {
			return fmt.Errorf("IPv4 send not configured correctly")
		}
		need := 14 + 20 + tcpLen
		if cap(frame) < need {
			frame = make([]byte, need)
		} else {
			frame = frame[:need]
		}

		// Ethernet
		copy(frame[0:6], h.srcIPv4RHWA)
		copy(frame[6:12], h.srcMAC)
		frame[12], frame[13] = 0x08, 0x00 // IPv4

		// IPv4 header
		ip := frame[14 : 14+20]
		writeIPv4Header(ip, src4, dst4, uint16(20+tcpLen))

		// TCP header + opts + payload
		tcpOff := 14 + 20
		seg := frame[tcpOff:]
		tcp := seg[:tcpHdrLen]
		writeTCPHeader(tcp[:20], h.srcPort, dstPort, seq, ack, f, uint8(tcpHdrLen/4))
		copy(tcp[20:], opts)
		poff := tcpHdrLen
		if len(prefix) > 0 {
			copy(seg[poff:], prefix)
			poff += len(prefix)
		}
		copy(seg[poff:], payload)

		// TCP checksum (includes payload)
		binary.BigEndian.PutUint16(tcp[16:18], 0)
		binary.BigEndian.PutUint16(tcp[16:18], tcpChecksumIPv4(src4, dst4, seg))

		return h.handle.WritePacketData(frame)
	}

	if dst16 := dstIP.To16(); dst16 != nil {
		src16 := h.srcIPv6.To16()
		if src16 == nil || len(h.srcIPv6RHWA) != 6 || len(h.srcMAC) != 6 {
			return fmt.Errorf("IPv6 send not configured correctly")
		}
		if tcpLen > 0xFFFF {
			return fmt.Errorf("IPv6 payload too large: %d", tcpLen)
		}
		need := 14 + 40 + tcpLen
		if cap(frame) < need {
			frame = make([]byte, need)
		} else {
			frame = frame[:need]
		}

		// Ethernet
		copy(frame[0:6], h.srcIPv6RHWA)
		copy(frame[6:12], h.srcMAC)
		frame[12], frame[13] = 0x86, 0xDD // IPv6

		// IPv6 header
		ip := frame[14 : 14+40]
		writeIPv6Header(ip, src16, dst16, uint16(tcpLen))

		// TCP header + opts + payload
		tcpOff := 14 + 40
		seg := frame[tcpOff:]
		tcp := seg[:tcpHdrLen]
		writeTCPHeader(tcp[:20], h.srcPort, dstPort, seq, ack, f, uint8(tcpHdrLen/4))
		copy(tcp[20:], opts)
		poff := tcpHdrLen
		if len(prefix) > 0 {
			copy(seg[poff:], prefix)
			poff += len(prefix)
		}
		copy(seg[poff:], payload)

		binary.BigEndian.PutUint16(tcp[16:18], 0)
		binary.BigEndian.PutUint16(tcp[16:18], tcpChecksumIPv6(src16, dst16, seg))

		return h.handle.WritePacketData(frame)
	}

	return fmt.Errorf("invalid destination IP: %v", dstIP)
}

func writeIPv4Header(hdr []byte, src4, dst4 []byte, totalLen uint16) {
	// 20 bytes, no options.
	hdr[0] = 0x45 // Version=4, IHL=5
	hdr[1] = 184  // TOS (DSCP=46)
	hdr[2] = byte(totalLen >> 8)
	hdr[3] = byte(totalLen)
	hdr[4], hdr[5] = 0, 0 // ID
	hdr[6], hdr[7] = 0x40, 0x00
	hdr[8] = 64 // TTL
	hdr[9] = 6  // TCP
	hdr[10], hdr[11] = 0, 0
	copy(hdr[12:16], src4)
	copy(hdr[16:20], dst4)
	binary.BigEndian.PutUint16(hdr[10:12], csumFinalize(csum16(hdr)))
}

func writeIPv6Header(hdr []byte, src16, dst16 []byte, payloadLen uint16) {
	// 40 bytes header.
	// Version=6, TrafficClass=184, FlowLabel=0.
	binary.BigEndian.PutUint32(hdr[0:4], 0x6B800000)
	binary.BigEndian.PutUint16(hdr[4:6], payloadLen)
	hdr[6] = 6  // NextHeader=TCP
	hdr[7] = 64 // HopLimit
	copy(hdr[8:24], src16)
	copy(hdr[24:40], dst16)
}

func writeTCPHeader(hdr []byte, srcPort, dstPort uint16, seq, ack uint32, f conf.TCPF, dataOffWords uint8) {
	binary.BigEndian.PutUint16(hdr[0:2], srcPort)
	binary.BigEndian.PutUint16(hdr[2:4], dstPort)
	binary.BigEndian.PutUint32(hdr[4:8], seq)
	binary.BigEndian.PutUint32(hdr[8:12], ack)

	var offFlags uint16 = uint16(dataOffWords) << 12
	if f.NS {
		offFlags |= 0x0100
	}
	if f.CWR {
		offFlags |= 0x0080
	}
	if f.ECE {
		offFlags |= 0x0040
	}
	if f.URG {
		offFlags |= 0x0020
	}
	if f.ACK {
		offFlags |= 0x0010
	}
	if f.PSH {
		offFlags |= 0x0008
	}
	if f.RST {
		offFlags |= 0x0004
	}
	if f.SYN {
		offFlags |= 0x0002
	}
	if f.FIN {
		offFlags |= 0x0001
	}

	binary.BigEndian.PutUint16(hdr[12:14], offFlags)
	binary.BigEndian.PutUint16(hdr[14:16], 65535) // window
	hdr[16], hdr[17] = 0, 0                       // checksum (filled later)
	hdr[18], hdr[19] = 0, 0                       // urgent pointer
}

func tcpChecksumIPv4(src4, dst4 []byte, tcpSeg []byte) uint16 {
	sum := csum16(src4)
	sum += csum16(dst4)
	sum += uint32(6) // protocol
	sum += uint32(len(tcpSeg))
	sum += csum16(tcpSeg)
	return csumFinalize(sum)
}

func tcpChecksumIPv6(src16, dst16 []byte, tcpSeg []byte) uint16 {
	sum := csum16(src16)
	sum += csum16(dst16)
	var l4 [4]byte
	binary.BigEndian.PutUint32(l4[:], uint32(len(tcpSeg)))
	sum += csum16(l4[:])
	sum += uint32(6) // next header
	sum += csum16(tcpSeg)
	return csumFinalize(sum)
}

func (h *SendHandle) getClientTCPF(dstIP net.IP, dstPort uint16) conf.TCPF {
	h.tcpF.mu.RLock()
	defer h.tcpF.mu.RUnlock()
	if ff := h.tcpF.clientTCPF[hash.IPAddr(dstIP, dstPort)]; ff != nil {
		return ff.Next()
	}
	return h.tcpF.tcpF.Next()
}

func (h *SendHandle) setClientTCPF(addr net.Addr, f []conf.TCPF) {
	a := *addr.(*net.UDPAddr)
	h.tcpF.mu.Lock()
	h.tcpF.clientTCPF[hash.IPAddr(a.IP, uint16(a.Port))] = &iterator.Iterator[conf.TCPF]{Items: f}
	h.tcpF.mu.Unlock()
}

func (h *SendHandle) Close() {
	if h.handle != nil {
		h.handle.Close()
	}
}
