package client

import (
	"errors"
	"paqet/internal/flog"
	"paqet/internal/tnet"
	"time"
)

var errNoTunnelConnections = errors.New("no tunnel connections available")

func (c *Client) newStrm() (tnet.Strm, error) {
	if c == nil || c.iter == nil || len(c.iter.Items) == 0 {
		return nil, errNoTunnelConnections
	}

	// Try each tunnel connection once per attempt. On cold start or during reconnects,
	// briefly wait for any tunnel to come up to avoid immediate user-visible failures.
	//
	// This is especially important during reinstall/restart where tunnel dials happen
	// in background and may take a few seconds.
	deadline := time.Now().Add(3 * time.Second)
	for {
		n := len(c.iter.Items)
		for i := 0; i < n; i++ {
			tc, ok := c.iter.Next()
			if !ok || tc == nil {
				continue
			}

			conn := tc.getConn()
			if conn == nil {
				tc.kickReconnect()
				continue
			}

			strm, err := conn.OpenStrm()
			if err == nil {
				return strm, nil
			}

			flog.Debugf("failed to open stream, reconnecting in background: %v", err)
			tc.markBroken(conn)
		}

		if time.Now().After(deadline) {
			return nil, errNoTunnelConnections
		}
		time.Sleep(50 * time.Millisecond)
	}
}
