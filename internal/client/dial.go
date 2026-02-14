package client

import (
	"errors"
	"paqet/internal/flog"
	"paqet/internal/tnet"
)

var errNoTunnelConnections = errors.New("no tunnel connections available")

func (c *Client) newStrm() (tnet.Strm, error) {
	if c == nil || c.iter == nil || len(c.iter.Items) == 0 {
		return nil, errNoTunnelConnections
	}

	// Try each currently-live tunnel once (fail-fast). Background reconnect loops keep tunnels up.
	openAttempts := 0
	n := len(c.iter.Items)
	for i := 0; i < n; i++ {
		tc := c.iter.Next()
		if tc == nil {
			continue
		}

		conn := tc.getConn()
		if conn == nil {
			tc.kickReconnect()
			continue
		}

		openAttempts++
		strm, err := conn.OpenStrm()
		if err == nil {
			return strm, nil
		}

		flog.Debugf("failed to open stream, reconnecting in background: %v", err)
		tc.markBroken(conn)
	}

	if openAttempts == 0 {
		return nil, errNoTunnelConnections
	}

	return nil, errNoTunnelConnections
}
