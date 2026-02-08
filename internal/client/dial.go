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

	for {
		tc := c.iter.Next()
		if tc == nil {
			return nil, errNoTunnelConnections
		}

		conn := tc.getConn()
		if conn == nil {
			if err := tc.reconnect(); err != nil {
				return nil, err
			}
			conn = tc.getConn()
			if conn == nil {
				continue
			}
		}

		strm, err := conn.OpenStrm()
		if err == nil {
			return strm, nil
		}

		flog.Debugf("failed to open stream, reconnecting: %v", err)
		if err := tc.reconnect(); err != nil {
			return nil, err
		}
	}
}
