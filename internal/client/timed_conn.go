package client

import (
	"context"
	"fmt"
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

	mu   sync.RWMutex
	conn tnet.Conn
}

func newTimedConn(ctx context.Context, cfg *conf.Conf) (*timedConn, error) {
	tc := &timedConn{cfg: cfg, ctx: ctx}
	conn, err := tc.createConn()
	if err != nil {
		return nil, err
	}
	tc.conn = conn
	return tc, nil
}

func (tc *timedConn) createConn() (tnet.Conn, error) {
	netCfg := tc.cfg.Network
	pConn, err := socket.New(tc.ctx, &netCfg)
	if err != nil {
		return nil, fmt.Errorf("could not create raw packet conn: %w", err)
	}

	conn, err := kcp.Dial(tc.cfg.Server.Addr, tc.cfg.Transport.KCP, pConn)
	if err != nil {
		return nil, err
	}

	if err := tc.sendTCPF(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
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
