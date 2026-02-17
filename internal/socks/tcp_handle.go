package socks

import (
	"fmt"
	"net"
	"paqet/internal/diag"
	"paqet/internal/flog"

	"github.com/txthinking/socks5"
)

func (h *Handler) TCPHandle(server *socks5.Server, conn *net.TCPConn, r *socks5.Request) error {
	if r.Cmd == socks5.CmdUDP {
		flog.Debugf("SOCKS5 UDP_ASSOCIATE from %s", conn.RemoteAddr())
		return h.handleUDPAssociate(conn)
	}

	if r.Cmd == socks5.CmdConnect {
		flog.Debugf("SOCKS5 CONNECT from %s to %s", conn.RemoteAddr(), r.Address())
		return h.handleTCPConnect(conn, r)
	}

	flog.Debugf("unsupported SOCKS5 command %d from %s", r.Cmd, conn.RemoteAddr())
	return nil
}

func (h *Handler) handleTCPConnect(conn *net.TCPConn, r *socks5.Request) error {
	flog.Debugf("SOCKS5 accepted TCP connection %s -> %s", conn.RemoteAddr(), r.Address())
	defer conn.Close()

	la := conn.LocalAddr()
	addr, ok := la.(*net.TCPAddr)
	if !ok || addr == nil {
		return fmt.Errorf("unexpected local address type: %T", la)
	}
	bufp := rPool.Get().(*[]byte)
	buf := (*bufp)[:0]
	defer func() {
		if cap(buf) > socksReplyBufCap {
			b := make([]byte, 0, socksReplyBufCap)
			*bufp = b
		} else {
			*bufp = buf[:0]
		}
		rPool.Put(bufp)
	}()
	buf = append(buf, socks5.Ver)
	buf = append(buf, socks5.RepSuccess)
	buf = append(buf, 0x00)
	if ip4 := addr.IP.To4(); ip4 != nil {
		buf = append(buf, socks5.ATYPIPv4)
		buf = append(buf, ip4...)
	} else if ip6 := addr.IP.To16(); ip6 != nil {
		buf = append(buf, socks5.ATYPIPv6)
		buf = append(buf, ip6...)
	} else {
		host := addr.IP.String()
		buf = append(buf, socks5.ATYPDomain)
		buf = append(buf, byte(len(host)))
		buf = append(buf, host...)
	}
	buf = append(buf, byte(addr.Port>>8), byte(addr.Port&0xff))
	if _, err := conn.Write(buf); err != nil {
		return err
	}

	strm, err := h.client.TCP(h.ctx, r.Address())
	if err != nil {
		flog.Errorf("SOCKS5 failed to establish stream for %s -> %s: %v", conn.RemoteAddr(), r.Address(), err)
		return err
	}
	defer strm.Close()
	flog.Debugf("SOCKS5 stream %d established for %s -> %s", strm.SID(), conn.RemoteAddr(), r.Address())

	errUp, errDown := diag.BidiCopy(
		h.ctx,
		conn,
		strm,
		func() error { return diag.CopyTCPUp(strm, conn) },
		func() error { return diag.CopyTCPDown(conn, strm) },
	)

	if h.ctx.Err() != nil {
		flog.Debugf("SOCKS5 connection %s -> %s closed due to shutdown", conn.RemoteAddr(), r.Address())
		return nil
	}
	if !diag.IsBenignStreamErr(errUp) {
		flog.Errorf("SOCKS5 stream %d failed for %s -> %s (up): %v", strm.SID(), conn.RemoteAddr(), r.Address(), errUp)
	}
	if !diag.IsBenignStreamErr(errDown) {
		flog.Errorf("SOCKS5 stream %d failed for %s -> %s (down): %v", strm.SID(), conn.RemoteAddr(), r.Address(), errDown)
	}

	flog.Debugf("SOCKS5 connection %s -> %s closed", conn.RemoteAddr(), r.Address())
	return nil
}
