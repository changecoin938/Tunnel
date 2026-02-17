package client

import (
	"context"
	"paqet/internal/flog"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
	"time"
)

func (c *Client) TCP(ctx context.Context, addr string) (tnet.Strm, error) {
	strm, err := c.newStrm(ctx)
	if err != nil {
		flog.Debugf("failed to create stream for TCP %s: %v", addr, err)
		return nil, err
	}

	tAddr, err := tnet.NewAddr(addr)
	if err != nil {
		flog.Debugf("invalid TCP address %s: %v", addr, err)
		strm.Close()
		return nil, err
	}

	p := protocol.Proto{Type: protocol.PTCP, Addr: tAddr}
	if c.cfg.Transport.KCP != nil && c.cfg.Transport.KCP.HeaderTimeout > 0 {
		_ = strm.SetWriteDeadline(time.Now().Add(time.Duration(c.cfg.Transport.KCP.HeaderTimeout) * time.Second))
	}
	err = p.Write(strm)
	_ = strm.SetWriteDeadline(time.Time{})
	if err != nil {
		flog.Debugf("failed to write TCP protocol header for %s on stream %d: %v", addr, strm.SID(), err)
		strm.Close()
		return nil, err
	}

	flog.Debugf("TCP stream %d established for %s", strm.SID(), addr)
	return strm, nil
}
