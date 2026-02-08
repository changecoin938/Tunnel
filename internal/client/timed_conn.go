package client

import (
	"context"
	"fmt"
	"net"
	"paqet/internal/conf"
	"paqet/internal/protocol"
	"paqet/internal/socket"
	"paqet/internal/tnet"
	"paqet/internal/tnet/kcp"
	"sync"
	"time"
)

type timedConn struct {
	cfg *conf.Conf
	ctx context.Context

	netCfg conf.Network
	connIndex int

	mu   sync.RWMutex
	conn tnet.Conn
}

func newTimedConn(ctx context.Context, cfg *conf.Conf, connIndex int) (*timedConn, error) {
	tc := &timedConn{cfg: cfg, ctx: ctx, netCfg: cfg.Network, connIndex: connIndex}
	if err := tc.applyConnIndex(connIndex); err != nil {
		return nil, err
	}
	conn, err := tc.createConn()
	if err != nil {
		return nil, err
	}
	tc.conn = conn
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
		timeout := 5 * time.Second
		if i == 0 && len(candidates) > 1 {
			// First candidate is the "offset port" probe. Fail fast to avoid slow startups.
			timeout = 1 * time.Second
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
	if kc, ok := conn.(*kcp.Conn); ok {
		if kc.UDPSession != nil {
			kc.UDPSession.Close()
		}
		if kc.Session != nil {
			kc.Session.Close()
		}
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

func (tc *timedConn) reconnect() error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tc.ctx.Err() != nil {
		return tc.ctx.Err()
	}
	if tc.conn != nil {
		_ = tc.conn.Close()
		tc.conn = nil
	}

	backoff := 200 * time.Millisecond
	for {
		if tc.ctx.Err() != nil {
			return tc.ctx.Err()
		}
		conn, err := tc.createConn()
		if err == nil {
			tc.conn = conn
			return nil
		}
		time.Sleep(backoff)
		if backoff < 5*time.Second {
			backoff *= 2
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
