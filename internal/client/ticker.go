package client

import (
	"context"
	"paqet/internal/diag"
	"time"
)

func (c *Client) ticker(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	i := 0
	for {
		select {
		case <-ticker.C:
			if len(c.iter.Items) == 0 {
				continue
			}
			tc := c.iter.Items[i%len(c.iter.Items)]
			i++
			if tc == nil {
				continue
			}
			conn := tc.getConn()
			if conn == nil {
				continue
			}
			start := time.Now()
			err := conn.Ping(true)
			diag.SetPing(time.Since(start), err)
		case <-ctx.Done():
			return
		}
	}
}
