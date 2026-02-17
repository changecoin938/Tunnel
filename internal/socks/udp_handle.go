package socks

import (
	"errors"
	"fmt"
	"io"
	"net"
	"paqet/internal/diag"
	"paqet/internal/flog"
	"paqet/internal/pkg/buffer"
	"syscall"
	"time"

	"github.com/txthinking/socks5"
)

func (h *Handler) UDPHandle(server *socks5.Server, addr *net.UDPAddr, d *socks5.Datagram) error {
	strm, new, k, err := h.client.UDP(h.ctx, addr.String(), d.Address())
	if err != nil {
		flog.Errorf("SOCKS5 failed to establish UDP stream for %s -> %s: %v", addr, d.Address(), err)
		return err
	}
	strm.SetWriteDeadline(time.Now().Add(8 * time.Second))
	_, err = strm.Write(d.Data)
	strm.SetWriteDeadline(time.Time{})
	if err != nil {
		flog.Errorf("SOCKS5 failed to forward %d bytes from %s -> %s: %v", len(d.Data), addr, d.Address(), err)
		h.client.CloseUDP(k)
		return err
	}
	diag.AddUDPUp(int64(len(d.Data)))

	if new {
		// Copy mutable request fields before starting a goroutine. Some SOCKS5
		// implementations reuse Datagram/UDPAddr storage between packets.
		atyp := d.Atyp
		dstAddr := append([]byte(nil), d.DstAddr...)
		dstPort := append([]byte(nil), d.DstPort...)
		peer := &net.UDPAddr{
			IP:   append(net.IP(nil), addr.IP...),
			Port: addr.Port,
			Zone: addr.Zone,
		}
		clientAddr := peer.String()
		targetAddr := d.Address()

		flog.Infof("SOCKS5 accepted UDP connection %s -> %s", addr, d.Address())
		go func() {
			bufp := buffer.UPool.Get().(*[]byte)
			defer buffer.UPool.Put(bufp)
			buf := *bufp

			defer func() {
				flog.Debugf("SOCKS5 UDP stream %d closed for %s -> %s", strm.SID(), clientAddr, targetAddr)
				h.client.CloseUDP(k)
			}()
			for {
				select {
				case <-h.ctx.Done():
					return
				default:
					strm.SetDeadline(time.Now().Add(8 * time.Second))
					n, err := strm.Read(buf)
					strm.SetDeadline(time.Time{})
					if err != nil {
						flog.Debugf("SOCKS5 UDP stream %d read error for %s -> %s: %v", strm.SID(), clientAddr, targetAddr, err)
						return
					}
					dd := socks5.NewDatagram(atyp, dstAddr, dstPort, buf[:n])
					ddBytes := dd.Bytes()
					written := false
					backoff := 200 * time.Microsecond
					for attempt := 0; attempt < 8; attempt++ {
						_, err = server.UDPConn.WriteToUDP(ddBytes, peer)
						if err == nil {
							written = true
							break
						}
						if errors.Is(err, syscall.ENOBUFS) || errors.Is(err, syscall.ENOMEM) {
							time.Sleep(backoff)
							if backoff < 10*time.Millisecond {
								backoff *= 2
							}
							continue
						}
						break // non-transient error
					}
					if !written && err != nil &&
						!errors.Is(err, syscall.ENOBUFS) && !errors.Is(err, syscall.ENOMEM) {
						flog.Errorf("SOCKS5 failed to write UDP response %d bytes to %s: %v", len(ddBytes), clientAddr, err)
						return
					}
					// ENOBUFS after retries = acceptable UDP packet loss; don't tear down relay.
					diag.AddUDPDown(int64(n))
				}
			}
		}()
	}
	return nil
}

func (h *Handler) handleUDPAssociate(conn *net.TCPConn) error {
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
	buf = append(buf, 0x00) // reserved
	if ip4 := addr.IP.To4(); ip4 != nil {
		// IPv4
		buf = append(buf, socks5.ATYPIPv4)
		buf = append(buf, ip4...)
	} else if ip6 := addr.IP.To16(); ip6 != nil {
		// IPv6
		buf = append(buf, socks5.ATYPIPv6)
		buf = append(buf, ip6...)
	} else {
		// Domain name
		host := addr.IP.String()
		buf = append(buf, socks5.ATYPDomain)
		buf = append(buf, byte(len(host)))
		buf = append(buf, host...)
	}
	buf = append(buf, byte(addr.Port>>8), byte(addr.Port&0xff))

	if _, err := conn.Write(buf); err != nil {
		return err
	}
	flog.Debugf("SOCKS5 accepted UDP_ASSOCIATE from %s, waiting for TCP connection to close", conn.RemoteAddr())

	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, conn)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil && h.ctx.Err() == nil {
			flog.Errorf("SOCKS5 TCP connection for UDP associate closed with: %v", err)
		}
	case <-h.ctx.Done():
		conn.Close() // Force close the connection to unblock io.Copy
		<-done       // Wait for the goroutine to finish
		flog.Debugf("SOCKS5 UDP_ASSOCIATE connection %s closed due to shutdown", conn.RemoteAddr())
	}

	flog.Debugf("SOCKS5 UDP_ASSOCIATE TCP connection %s closed", conn.RemoteAddr())
	return nil
}
