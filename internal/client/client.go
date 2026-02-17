package client

import (
	"context"
	"paqet/internal/conf"
	"paqet/internal/diag"
	"paqet/internal/flog"
	"paqet/internal/pkg/iterator"
	"sync"
	"time"
)

type Client struct {
	cfg     *conf.Conf
	iter    *iterator.Iterator[*timedConn]
	udpPool *udpPool

	// connReady is closed+recreated whenever any timedConn finishes reconnecting.
	// newStrm waits on this instead of busy-polling every 50ms.
	connReadyMu sync.Mutex
	connReady   chan struct{}

	shutdownDone chan struct{}
	shutdownOnce sync.Once
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
		connReady:    make(chan struct{}),
		shutdownDone: make(chan struct{}),
	}
	return c, nil
}

// notifyConnReady signals any goroutines waiting in newStrm that a connection
// may now be available. Called by timedConn after a successful reconnect.
func (c *Client) notifyConnReady() {
	c.connReadyMu.Lock()
	close(c.connReady)
	c.connReady = make(chan struct{})
	c.connReadyMu.Unlock()
}

// getConnReady returns the current connReady channel for waiting.
func (c *Client) getConnReady() <-chan struct{} {
	c.connReadyMu.Lock()
	ch := c.connReady
	c.connReadyMu.Unlock()
	return ch
}

func (c *Client) Start(ctx context.Context) error {
	items := make([]*timedConn, 0, c.cfg.Transport.Conn)
	for i := range c.cfg.Transport.Conn {
		tc, err := newTimedConn(ctx, c.cfg, i, c)
		if tc == nil {
			return err
		}
		items = append(items, tc)
	}
	c.iter.Items = items
	for _, tc := range c.iter.Items {
		go tc.maintain()
	}
	flog.Infof("client initializing %d tunnel connections in background", len(c.iter.Items))
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
		c.shutdownOnce.Do(func() { close(c.shutdownDone) })
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

func (c *Client) WaitShutdown(timeout time.Duration) bool {
	if c == nil || c.shutdownDone == nil {
		return true
	}
	if timeout <= 0 {
		<-c.shutdownDone
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-c.shutdownDone:
		return true
	case <-timer.C:
		return false
	}
}
