package socket

import (
	"context"
	"crypto/hmac"
	"fmt"
	"math/rand"
	"net"
	"os"
	"paqet/internal/conf"
	"paqet/internal/diag"
	"sync/atomic"
	"time"
)

type PacketConn struct {
	cfg           *conf.Network
	sendHandle    *SendHandle
	recvHandle    *RecvHandle
	guard         *guardState
	readDeadline  atomic.Value
	writeDeadline atomic.Value

	ctx    context.Context
	cancel context.CancelFunc
}

// &OpError{Op: "listen", Net: network, Source: nil, Addr: nil, Err: err}
func New(ctx context.Context, cfg *conf.Network) (*PacketConn, error) {
	if cfg.Port == 0 {
		cfg.Port = 32768 + rand.Intn(32768)
	}

	sendHandle, err := NewSendHandle(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create send handle on %s: %v", cfg.Interface.Name, err)
	}

	recvHandle, err := NewRecvHandle(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create receive handle on %s: %v", cfg.Interface.Name, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	conn := &PacketConn{
		cfg:        cfg,
		sendHandle: sendHandle,
		recvHandle: recvHandle,
		ctx:        ctx,
		cancel:     cancel,
	}

	return conn, nil
}

func (c *PacketConn) setGuard(st *guardState) {
	if c == nil {
		return
	}
	c.guard = st
}

func (c *PacketConn) ReadFrom(data []byte) (n int, addr net.Addr, err error) {
	var timer *time.Timer
	var deadline <-chan time.Time
	if d, ok := c.readDeadline.Load().(time.Time); ok && !d.IsZero() {
		timer = time.NewTimer(time.Until(d))
		defer timer.Stop()
		deadline = timer.C
	}

	select {
	case <-c.ctx.Done():
		return 0, nil, c.ctx.Err()
	case <-deadline:
		return 0, nil, os.ErrDeadlineExceeded
	default:
	}

	for {
		select {
		case <-c.ctx.Done():
			return 0, nil, c.ctx.Err()
		case <-deadline:
			return 0, nil, os.ErrDeadlineExceeded
		default:
		}

		payload, addr, err := c.recvHandle.Read()
		if err != nil {
			return 0, nil, err
		}

		if g := c.guard; g != nil {
			// Guard validation + strip (in-place, without extra copy).
			if len(payload) < guardHeaderLen {
				diag.AddGuardDrop()
				continue
			}
			if !hmac.Equal(payload[0:4], g.magic[:]) {
				diag.AddGuardDrop()
				continue
			}
			cookies := g.getCookies()
			ok := false
			for i := range cookies.cookies {
				if hmac.Equal(payload[4:12], cookies.cookies[i][:]) {
					ok = true
					break
				}
			}
			if !ok {
				diag.AddGuardDrop()
				continue
			}
			diag.AddGuardPass()
			payload = payload[guardHeaderLen:]
		}

		n = copy(data, payload)
		rawCount := n
		if c.guard != nil {
			rawCount += guardHeaderLen
		}
		diag.AddRawDown(rawCount)
		return n, addr, nil
	}
}

func (c *PacketConn) WriteTo(data []byte, addr net.Addr) (n int, err error) {
	var timer *time.Timer
	var deadline <-chan time.Time
	if d, ok := c.writeDeadline.Load().(time.Time); ok && !d.IsZero() {
		timer = time.NewTimer(time.Until(d))
		defer timer.Stop()
		deadline = timer.C
	}

	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	case <-deadline:
		return 0, os.ErrDeadlineExceeded
	default:
	}

	daddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return 0, net.InvalidAddrError("invalid address")
	}

	wireLen := len(data)
	if g := c.guard; g != nil {
		var hdr [guardHeaderLen]byte
		copy(hdr[0:4], g.magic[:])
		cookies := g.getCookies()
		copy(hdr[4:12], cookies.cookies[0][:])
		wireLen += guardHeaderLen
		err = c.sendHandle.WriteParts(hdr[:], data, daddr)
	} else {
		err = c.sendHandle.Write(data, daddr)
	}
	if err != nil {
		return 0, err
	}

	diag.AddRawUp(wireLen)
	return len(data), nil
}

func (c *PacketConn) Close() error {
	c.cancel()

	if c.sendHandle != nil {
		go c.sendHandle.Close()
	}
	if c.recvHandle != nil {
		go c.recvHandle.Close()
	}

	return nil
}

func (c *PacketConn) LocalAddr() net.Addr {
	// This is a best-effort informational address. We don't use a kernel UDP socket,
	// but higher layers (KCP/session logs) expect a non-nil LocalAddr.
	if c == nil || c.cfg == nil {
		return nil
	}
	if c.cfg.IPv4.Addr != nil {
		return &net.UDPAddr{
			IP:   append([]byte(nil), c.cfg.IPv4.Addr.IP...),
			Port: c.cfg.Port,
			Zone: c.cfg.IPv4.Addr.Zone,
		}
	}
	if c.cfg.IPv6.Addr != nil {
		return &net.UDPAddr{
			IP:   append([]byte(nil), c.cfg.IPv6.Addr.IP...),
			Port: c.cfg.Port,
			Zone: c.cfg.IPv6.Addr.Zone,
		}
	}
	return &net.UDPAddr{Port: c.cfg.Port}
}

func (c *PacketConn) SetDeadline(t time.Time) error {
	c.readDeadline.Store(t)
	c.writeDeadline.Store(t)
	return nil
}

func (c *PacketConn) SetReadDeadline(t time.Time) error {
	c.readDeadline.Store(t)
	return nil
}

func (c *PacketConn) SetWriteDeadline(t time.Time) error {
	c.writeDeadline.Store(t)
	return nil
}

func (c *PacketConn) SetDSCP(dscp int) error {
	return nil
}

func (c *PacketConn) SetClientTCPF(addr net.Addr, f []conf.TCPF) {
	c.sendHandle.setClientTCPF(addr, f)
}
