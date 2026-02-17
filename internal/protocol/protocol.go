package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
	"paqet/internal/conf"
	"paqet/internal/tnet"
)

type PType = byte

const (
	PPING PType = 0x01
	PPONG PType = 0x02
	PTCPF PType = 0x03
	PTCP  PType = 0x04
	PUDP  PType = 0x05
)

// maxProtoSize caps the maximum decoded message to prevent OOM from malformed input.
const maxProtoSize = 16 * 1024

type Proto struct {
	Type PType
	Addr *tnet.Addr
	TCPF []conf.TCPF
}

// Wire format (all big-endian):
//
//	[1 byte]  Type
//	[1 byte]  flags  (bit 0 = has Addr, bit 1 = has TCPF)
//	--- if has Addr ---
//	[2 bytes] host length (N)
//	[N bytes] host string (UTF-8)
//	[2 bytes] port
//	--- if has TCPF ---
//	[1 byte]  TCPF count (max 255)
//	per entry:
//	  [2 bytes] flag bitmask (FIN=0x01,SYN=0x02,RST=0x04,PSH=0x08,ACK=0x10,URG=0x20,ECE=0x40,CWR=0x80,NS=0x100)

func (p *Proto) Write(w io.Writer) error {
	var buf [512]byte
	n := 0

	buf[n] = p.Type
	n++

	var flags byte
	if p.Addr != nil {
		flags |= 0x01
	}
	if len(p.TCPF) > 0 {
		flags |= 0x02
	}
	buf[n] = flags
	n++

	if p.Addr != nil {
		host := p.Addr.Host
		if len(host) > 253 {
			return fmt.Errorf("proto: host too long (%d bytes)", len(host))
		}
		binary.BigEndian.PutUint16(buf[n:], uint16(len(host)))
		n += 2
		n += copy(buf[n:], host)
		binary.BigEndian.PutUint16(buf[n:], uint16(p.Addr.Port))
		n += 2
	}

	if len(p.TCPF) > 0 {
		if len(p.TCPF) > 255 {
			return fmt.Errorf("proto: too many TCPF entries (%d)", len(p.TCPF))
		}
		buf[n] = byte(len(p.TCPF))
		n++
		for _, f := range p.TCPF {
			var bits uint16
			if f.FIN {
				bits |= 0x01
			}
			if f.SYN {
				bits |= 0x02
			}
			if f.RST {
				bits |= 0x04
			}
			if f.PSH {
				bits |= 0x08
			}
			if f.ACK {
				bits |= 0x10
			}
			if f.URG {
				bits |= 0x20
			}
			if f.ECE {
				bits |= 0x40
			}
			if f.CWR {
				bits |= 0x80
			}
			if f.NS {
				bits |= 0x100
			}
			binary.BigEndian.PutUint16(buf[n:], bits)
			n += 2
		}
	}

	_, err := w.Write(buf[:n])
	return err
}

func (p *Proto) Read(r io.Reader) error {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	p.Type = hdr[0]
	flags := hdr[1]

	p.Addr = nil
	p.TCPF = nil

	if flags&0x01 != 0 {
		var lb [2]byte
		if _, err := io.ReadFull(r, lb[:]); err != nil {
			return err
		}
		hostLen := binary.BigEndian.Uint16(lb[:])
		if hostLen > maxProtoSize {
			return fmt.Errorf("proto: host length %d exceeds max", hostLen)
		}
		host := make([]byte, hostLen)
		if _, err := io.ReadFull(r, host); err != nil {
			return err
		}
		var pb [2]byte
		if _, err := io.ReadFull(r, pb[:]); err != nil {
			return err
		}
		port := binary.BigEndian.Uint16(pb[:])
		p.Addr = &tnet.Addr{Host: string(host), Port: int(port)}
	}

	if flags&0x02 != 0 {
		var cb [1]byte
		if _, err := io.ReadFull(r, cb[:]); err != nil {
			return err
		}
		count := int(cb[0])
		p.TCPF = make([]conf.TCPF, count)
		for i := 0; i < count; i++ {
			var fb [2]byte
			if _, err := io.ReadFull(r, fb[:]); err != nil {
				return err
			}
			bits := binary.BigEndian.Uint16(fb[:])
			p.TCPF[i] = conf.TCPF{
				FIN: bits&0x01 != 0,
				SYN: bits&0x02 != 0,
				RST: bits&0x04 != 0,
				PSH: bits&0x08 != 0,
				ACK: bits&0x10 != 0,
				URG: bits&0x20 != 0,
				ECE: bits&0x40 != 0,
				CWR: bits&0x80 != 0,
				NS:  bits&0x100 != 0,
			}
		}
	}

	return nil
}
