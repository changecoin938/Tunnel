package client

import (
	"context"
	"errors"
	"paqet/internal/flog"
	"paqet/internal/tnet"
	"time"
)

var errNoTunnelConnections = errors.New("no tunnel connections available")

func (c *Client) newStrm(ctx context.Context) (tnet.Strm, error) {
	if c == nil || c.iter == nil || len(c.iter.Items) == 0 {
		return nil, errNoTunnelConnections
	}

	// Try each tunnel connection once per attempt. On cold start or during reconnects,
	// briefly wait for any tunnel to come up to avoid immediate user-visible failures.
	//
	// This is especially important during reinstall/restart where tunnel dials happen
	// in background and may take a few seconds.
	deadline := time.Now().Add(3 * time.Second)
	kicked := false
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		n := len(c.iter.Items)
		for i := 0; i < n; i++ {
			tc, ok := c.iter.Next()
			if !ok || tc == nil {
				continue
			}

			conn := tc.getConn()
			if conn == nil {
				if !kicked {
					tc.kickReconnect()
				}
				continue
			}

			strm, err := conn.OpenStrm()
			if err == nil {
				return strm, nil
			}

			flog.Debugf("failed to open stream, reconnecting in background: %v", err)
			tc.markBroken(conn)
		}
		kicked = true // Only kick reconnects once to avoid thundering-herd.

		if time.Now().After(deadline) {
			return nil, errNoTunnelConnections
		}

		// Wait for a connection to become ready instead of busy-polling.
		// This reduces CPU usage and avoids thundering-herd reconnect storms.
		remaining := time.Until(deadline)
		waitTimer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			waitTimer.Stop()
			return nil, ctx.Err()
		case <-c.getConnReady():
			waitTimer.Stop()
			// A connection may be ready, retry immediately.
		case <-waitTimer.C:
			// Timeout reached, will check deadline at loop top.
		}
	}
}
