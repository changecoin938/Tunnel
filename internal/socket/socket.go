package socket

import (
	"bytes"
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"paqet/internal/conf"
	"paqet/internal/diag"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type PacketConn struct {
	cfg           *conf.Network
	sendHandle    *SendHandle
	recvHandle    *RecvHandle
	guard         *guardState
	readDeadline  atomic.Value
	writeDeadline atomic.Value

	// Guarded payload de-coalescing:
	// Some NICs/kernels coalesce multiple small TCP segments into a single large frame (GRO/LRO),
	// which would otherwise break KCP (it expects a single packet per ReadFrom).
	pending     [][]byte // slices into pendingBuf (guard header already stripped)
	pendingBuf  []byte
	pendingAddr net.Addr
	bufPool     sync.Pool // []byte, used for pendingBuf

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
		bufPool: sync.Pool{
			New: func() any { return make([]byte, 0, 64*1024) },
		},
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
		// If we previously received a coalesced frame, drain pending packets first.
		if len(c.pending) > 0 {
			seg := c.pending[0]
			c.pending = c.pending[1:]
			addr = c.pendingAddr
			if len(seg) > len(data) {
				// Should never happen (kcp-go reads into a fixed 1500-byte buffer), but don't
				// return an error: kcp-go treats ReadFrom errors as fatal.
				if len(c.pending) == 0 {
					c.recyclePending()
				}
				continue
			}
			copy(data, seg)
			diag.AddRawDown(len(seg) + guardHeaderLen)
			if len(c.pending) == 0 {
				c.recyclePending()
			}
			return len(seg), addr, nil
		}

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

			// Detect and split GRO/LRO-coalesced payloads:
			// payload may contain multiple (guardHeader + encryptedKCP) packets concatenated.
			if next := findNextGuard(payload, guardHeaderLen, g, cookies); next != -1 {
				if c.enqueueCoalesced(payload, addr, g, cookies) {
					// Now drain from pending on the next iteration.
					continue
				}
				// If enqueue failed for any reason, fall back to single-packet behavior below.
			}

			diag.AddGuardPass()
			payload = payload[guardHeaderLen:]
		}

		if len(payload) > len(data) {
			// Never silently truncate. Also, never return an error here because kcp-go treats
			// ReadFrom errors as fatal. Drop and let KCP recover via retransmit.
			rawCount := len(payload)
			if c.guard != nil {
				rawCount += guardHeaderLen
			}
			diag.AddRawDownOversizeDrop(rawCount)
			continue
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

func (c *PacketConn) recyclePending() {
	if c.pendingBuf == nil {
		c.pendingAddr = nil
		return
	}
	buf := c.pendingBuf
	c.pendingBuf = nil
	c.pendingAddr = nil
	c.pending = c.pending[:0]
	if cap(buf) <= 512*1024 {
		buf = buf[:0]
		c.bufPool.Put(buf)
	}
}

func findNextGuard(payload []byte, start int, g *guardState, cookies *guardCookies) int {
	if len(payload) < guardHeaderLen || start < 0 || start >= len(payload) {
		return -1
	}
	magic := g.magic[:]
	i := start
	for {
		j := bytes.Index(payload[i:], magic)
		if j == -1 {
			return -1
		}
		pos := i + j
		if pos+guardHeaderLen > len(payload) {
			return -1
		}
		ok := false
		for k := range cookies.cookies {
			if hmac.Equal(payload[pos+4:pos+12], cookies.cookies[k][:]) {
				ok = true
				break
			}
		}
		if ok {
			return pos
		}
		// Keep scanning after this byte to avoid infinite loops on repeated matches.
		i = pos + 1
		if i >= len(payload) {
			return -1
		}
	}
}

func (c *PacketConn) enqueueCoalesced(payload []byte, addr net.Addr, g *guardState, cookies *guardCookies) bool {
	// Copy payload into a stable buffer; pcap buffers are reused.
	buf := c.bufPool.Get().([]byte)
	if cap(buf) < len(payload) {
		buf = make([]byte, len(payload))
	} else {
		buf = buf[:len(payload)]
	}
	copy(buf, payload)

	// Split on validated guard headers.
	var parts [][]byte
	for pos := 0; pos+guardHeaderLen <= len(buf); {
		if !hmac.Equal(buf[pos:pos+4], g.magic[:]) {
			diag.AddGuardDrop()
			pos++
			continue
		}
		ok := false
		for k := range cookies.cookies {
			if hmac.Equal(buf[pos+4:pos+12], cookies.cookies[k][:]) {
				ok = true
				break
			}
		}
		if !ok {
			diag.AddGuardDrop()
			pos++
			continue
		}
		diag.AddGuardPass()

		start := pos + guardHeaderLen
		next := findNextGuard(buf, start, g, cookies)
		end := len(buf)
		if next != -1 {
			end = next
		}
		if end > start {
			parts = append(parts, buf[start:end])
		}
		if next == -1 {
			break
		}
		pos = next
	}

	if len(parts) == 0 {
		// Give buffer back.
		if cap(buf) <= 512*1024 {
			buf = buf[:0]
			c.bufPool.Put(buf)
		}
		return false
	}

	// Stash for subsequent ReadFrom calls.
	diag.AddRawDownCoalesced(len(parts))
	c.pendingBuf = buf
	c.pending = append(c.pending[:0], parts...)
	c.pendingAddr = addr
	return true
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

	var prefix []byte
	var hdr [guardHeaderLen]byte
	if g := c.guard; g != nil {
		copy(hdr[0:4], g.magic[:])
		cookies := g.getCookies()
		copy(hdr[4:12], cookies.cookies[0][:])
		prefix = hdr[:]
		wireLen += guardHeaderLen
	}

	// Under heavy bursts, pcap injection can transiently fail with ENOBUFS.
	// For KCP this should be treated like packet loss, not a fatal connection error.
	// We do a bounded retry to smooth bursts, then drop (report success) so upper
	// layers can recover via retransmit/backpressure.
	//
	// Note: returning a non-nil error here will cause kcp-go to tear down the
	// session. For transient ENOBUFS/ENOMEM we must keep the session alive.
	const (
		maxTotalSleep = 50 * time.Millisecond
		maxBackoff    = 20 * time.Millisecond
	)
	backoff := 200 * time.Microsecond
	var totalSlept time.Duration
	for {
		if prefix != nil {
			err = c.sendHandle.WriteParts(prefix, data, daddr)
		} else {
			err = c.sendHandle.Write(data, daddr)
		}
		if err == nil {
			diag.AddRawUp(wireLen)
			return len(data), nil
		}
		// libpcap returns plain string errors via pcap_geterr (no errno), so also
		// match by message to detect ENOBUFS/ENOMEM.
		if !errors.Is(err, syscall.ENOBUFS) &&
			!errors.Is(err, syscall.ENOMEM) &&
			!strings.Contains(err.Error(), "No buffer space available") &&
			!strings.Contains(err.Error(), "Cannot allocate memory") {
			return 0, err
		}

		// ENOBUFS/ENOMEM: bounded retry, then drop (as loss).
		if totalSlept >= maxTotalSleep {
			diag.AddRawUpDrop(wireLen)
			return len(data), nil
		}

		select {
		case <-c.ctx.Done():
			return 0, c.ctx.Err()
		case <-deadline:
			return 0, os.ErrDeadlineExceeded
		default:
		}

		time.Sleep(backoff)
		totalSlept += backoff
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
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
