package client

import (
	"context"
	"paqet/internal/conf"
	"paqet/internal/diag"
	"paqet/internal/flog"
	"paqet/internal/pkg/iterator"
)

type Client struct {
	cfg     *conf.Conf
	iter    *iterator.Iterator[*timedConn]
	udpPool *udpPool
}

func New(cfg *conf.Conf) (*Client, error) {
	c := &Client{
		cfg:  cfg,
		iter: &iterator.Iterator[*timedConn]{},
		udpPool: &udpPool{
			strms:       make(map[uint64]*udpTrackedStrm),
			maxEntries:  udpPoolMaxEntriesDefault,
			idleTimeout: udpPoolIdleTimeoutDefault,
		},
	}
	return c, nil
}

func (c *Client) Start(ctx context.Context) error {
	connected := 0
	for i := range c.cfg.Transport.Conn {
		tc, err := newTimedConn(ctx, c.cfg, i)
		if tc == nil {
			// Fatal config error (e.g., port range too large).
			return err
		}
		if err != nil {
			flog.Errorf("failed to establish connection %d (will retry in background): %v", i+1, err)
		} else {
			connected++
			flog.Debugf("client connection %d established successfully", i+1)
		}
		c.iter.Items = append(c.iter.Items, tc)
		go tc.maintain()
	}
	if connected == 0 {
		flog.Warnf("client started with 0/%d tunnel connections established (will keep retrying)", len(c.iter.Items))
	}
	// The ticker is only for diagnostics (ping RTT). Avoid extra streams/CPU in production.
	if diag.Enabled() {
		go c.ticker(ctx)
	}
	if c.udpPool != nil {
		go c.udpPool.sweep(ctx)
	}

	go func() {
		<-ctx.Done()
		for _, tc := range c.iter.Items {
			tc.close()
		}
		flog.Infof("client shutdown complete")
	}()

	ipv4Addr := "<nil>"
	ipv6Addr := "<nil>"
	if c.cfg.Network.IPv4.Addr != nil {
		ipv4Addr = c.cfg.Network.IPv4.Addr.IP.String()
	}
	if c.cfg.Network.IPv6.Addr != nil {
		ipv6Addr = c.cfg.Network.IPv6.Addr.IP.String()
	}
	flog.Infof("Client started: IPv4:%s IPv6:%s -> %s (%d connections)", ipv4Addr, ipv6Addr, c.cfg.Server.Addr, len(c.iter.Items))
	return nil
}
