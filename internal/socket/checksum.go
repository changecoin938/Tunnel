package socket

import "encoding/binary"

// csum16 returns the 16-bit one's-complement sum of all 16-bit words in b.
// It does NOT fold/carry and does NOT invert. Use csumFinalize at the end.
func csum16(b []byte) uint32 {
	var sum uint32
	n := len(b)
	i := 0
	for n-i >= 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
		i += 2
	}
	if i < n {
		// odd trailing byte, big-endian: high byte
		sum += uint32(b[i]) << 8
	}
	return sum
}

func csumFinalize(sum uint32) uint16 {
	for (sum >> 16) != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
}
