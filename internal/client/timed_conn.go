package client

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"paqet/internal/conf"
	"paqet/internal/flog"
	"paqet/internal/protocol"
	"paqet/internal/socket"
	"paqet/internal/tnet"
	"paqet/internal/tnet/kcp"
	"sync"
	"time"
)

type timedConn struct {
	cfg    *conf.Conf
	ctx    context.Context
	client *Client // back-reference to notify when reconnect succeeds

	netCfg    conf.Network
	connIndex int

	mu          sync.RWMutex
	conn        tnet.Conn
	reconnectCh chan struct{}
	kickCh      chan struct{}
}

func newTimedConn(ctx context.Context, cfg *conf.Conf, connIndex int, client *Client) (*timedConn, error) {
	tc := &timedConn{cfg: cfg, ctx: ctx, client: client, netCfg: cfg.Network, connIndex: connIndex, kickCh: make(chan struct{}, 1)}
	if err := tc.applyConnIndex(connIndex); err != nil {
		return nil, err
	}
	return tc, nil
}

func (tc *timedConn) createConn() (tnet.Conn, error) {
	pConn, err := socket.New(tc.ctx, &tc.netCfg)
	if err != nil {
		return nil, fmt.Errorf("could not create raw packet conn (port=%d): %w", tc.netCfg.Port, err)
	}

	base := tc.cfg.Server.Addr
	if base == nil {
		_ = pConn.Close()
		return nil, fmt.Errorf("server address is not configured")
	}

	candidates := []*net.UDPAddr{base}
	if tc.connIndex > 0 && base.Port > 0 {
		port := base.Port + tc.connIndex
		if port > 0 && port <= 65535 {
			candidates = []*net.UDPAddr{cloneUDPAddrPort(base, port), base}
		}
	}

	var lastErr error
	for i, dst := range candidates {
		conn, err := kcp.Dial(dst, tc.cfg.Transport.KCP, pConn)
		if err != nil {
			lastErr = err
			continue
		}

		// Verify the tunnel is actually up (and the key matches) before exposing it.
		timeout := 10 * time.Second
		if i == 0 && len(candidates) > 1 {
			timeout = 3 * time.Second
		}
		if err := pingConn(conn, timeout); err != nil {
			lastErr = err
			closeConnKeepPacket(conn)
			continue
		}

		if err := tc.sendTCPF(conn); err != nil {
			lastErr = err
			closeConnKeepPacket(conn)
			continue
		}
		return conn, nil
	}

	_ = pConn.Close()
	if lastErr == nil {
		lastErr = fmt.Errorf("failed to establish tunnel connection")
	}
	return nil, lastErr
}

func cloneUDPAddrPort(a *net.UDPAddr, port int) *net.UDPAddr {
	if a == nil {
		return nil
	}
	out := *a
	if a.IP != nil {
		out.IP = append([]byte(nil), a.IP...)
	}
	out.Port = port
	return &out
}

func pingConn(conn tnet.Conn, timeout time.Duration) error {
	strm, err := conn.OpenStrm()
	if err != nil {
		return fmt.Errorf("ping failed: %v", err)
	}
	defer strm.Close()

	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	_ = strm.SetDeadline(time.Now().Add(timeout))
	defer strm.SetDeadline(time.Time{})

	p := protocol.Proto{Type: protocol.PPING}
	if err := p.Write(strm); err != nil {
		return fmt.Errorf("connection test failed: %v", err)
	}
	if err := p.Read(strm); err != nil {
		return fmt.Errorf("connection test failed: %v", err)
	}
	if p.Type != protocol.PPONG {
		return fmt.Errorf("connection test failed: unexpected response")
	}
	return nil
}

func closeConnKeepPacket(conn tnet.Conn) {
	if conn == nil {
		return
	}
	// Avoid closing the raw PacketConn when we're doing "try port A, then port B" fallback.
	// Use the canonical Close() path but skip PacketConn ownership to preserve it for reuse.
	if kc, ok := conn.(*kcp.Conn); ok {
		kc.OwnPacketConn = false
		_ = kc.Close()
		return
	}
	_ = conn.Close()
}

func (tc *timedConn) applyConnIndex(connIndex int) error {
	if connIndex <= 0 {
		return nil
	}
	basePort := tc.netCfg.Port
	if basePort == 0 {
		// Random port mode: leave Port=0 so socket.New picks a random port per timedConn.
		// We intentionally do NOT apply an offset because port=0 is a special "random" value.
		return nil
	}

	port := basePort + connIndex
	if port > 65535 {
		return fmt.Errorf("client port range too large: base=%d conn_index=%d => %d", basePort, connIndex, port)
	}

	tc.netCfg.Port = port
	if tc.netCfg.IPv4.Addr != nil {
		tc.netCfg.IPv4.Addr = cloneUDPAddrPort(tc.netCfg.IPv4.Addr, port)
	}
	if tc.netCfg.IPv6.Addr != nil {
		tc.netCfg.IPv6.Addr = cloneUDPAddrPort(tc.netCfg.IPv6.Addr, port)
	}
	return nil
}

func (tc *timedConn) getConn() tnet.Conn {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.conn
}

func (tc *timedConn) markBroken(conn tnet.Conn) {
	if conn == nil {
		return
	}

	tc.mu.Lock()
	if tc.conn != conn {
		tc.mu.Unlock()
		return
	}
	_ = tc.conn.Close()
	tc.conn = nil
	tc.mu.Unlock()

	tc.kickReconnect()
}

func (tc *timedConn) kickReconnect() {
	if tc == nil || tc.kickCh == nil {
		return
	}
	select {
	case tc.kickCh <- struct{}{}:
	default:
	}
}

func (tc *timedConn) reconnect() error {
	for {
		if err := tc.ctx.Err(); err != nil {
			return err
		}

		tc.mu.Lock()
		if tc.conn != nil {
			tc.mu.Unlock()
			return nil
		}
		if tc.reconnectCh != nil {
			ch := tc.reconnectCh
			tc.mu.Unlock()
			select {
			case <-ch:
				// Re-check connection after another goroutine finishes reconnecting.
				continue
			case <-tc.ctx.Done():
				return tc.ctx.Err()
			}
		}
		ch := make(chan struct{})
		tc.reconnectCh = ch
		tc.mu.Unlock()

		conn, err := tc.reconnectLoop()

		tc.mu.Lock()
		// If we reconnected successfully but someone else already set tc.conn (should be rare),
		// prefer the existing one and close ours to avoid leaks.
		if err == nil {
			if tc.conn == nil {
				tc.conn = conn
			} else if conn != nil {
				_ = conn.Close()
			}
		}
		tc.reconnectCh = nil
		close(ch)
		tc.mu.Unlock()

		// Notify waiting newStrm() goroutines that a connection may be ready.
		if err == nil && tc.client != nil {
			tc.client.notifyConnReady()
		}

		return err
	}
}

func (tc *timedConn) reconnectLoop() (tnet.Conn, error) {
	backoff := 500 * time.Millisecond // Start with 500ms instead of 50ms to reduce connection churn.
	nextLog := time.Now()
	var lastErr error
	for {
		if err := tc.ctx.Err(); err != nil {
			return nil, err
		}
		conn, err := tc.createConn()
		if err == nil {
			return conn, nil
		}
		lastErr = err
		now := time.Now()
		if now.After(nextLog) {
			flog.Warnf("tunnel connection %d reconnect failed (retrying): %v", tc.connIndex+1, lastErr)
			nextLog = now.Add(60 * time.Second)
		}
		// Add jitter (±25%) to prevent thundering-herd when multiple connections reconnect simultaneously.
		jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
		time.Sleep(backoff + jitter - backoff/4)
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (tc *timedConn) maintain() {
	// Establish as soon as possible. Never block caller.
	_ = tc.reconnect()

	for {
		// Use a long interval when connected (reduces CPU wakeups and battery drain),
		// fall back to a shorter interval when the connection is broken so we notice faster.
		interval := 30 * time.Second
		if tc.getConn() == nil {
			interval = 2 * time.Second
		}
		timer := time.NewTimer(interval)
		select {
		case <-tc.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-tc.kickCh:
			timer.Stop()
		}

		if tc.getConn() == nil {
			_ = tc.reconnect()
		}
	}
}

func (tc *timedConn) sendTCPF(conn tnet.Conn) error {
	strm, err := conn.OpenStrm()
	if err != nil {
		return err
	}
	defer strm.Close()

	p := protocol.Proto{Type: protocol.PTCPF, TCPF: tc.cfg.Network.TCP.RF}
	err = p.Write(strm)
	if err != nil {
		return err
	}
	return nil
}

func (tc *timedConn) close() {
	tc.mu.Lock()
	conn := tc.conn
	tc.conn = nil
	tc.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
}
