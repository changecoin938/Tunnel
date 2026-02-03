package client

import (
	"paqet/internal/flog"
	"paqet/internal/pkg/hash"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
	"time"
)

func (c *Client) UDP(lAddr, tAddr string) (tnet.Strm, bool, uint64, error) {
	key := hash.AddrPair(lAddr, tAddr)
	c.udpPool.mu.RLock()
	if strm, exists := c.udpPool.strms[key]; exists {
		c.udpPool.mu.RUnlock()
		flog.Debugf("reusing UDP stream %d for %s -> %s", strm.SID(), lAddr, tAddr)
		return strm, false, key, nil
	}
	c.udpPool.mu.RUnlock()

	strm, err := c.newStrm()
	if err != nil {
		flog.Debugf("failed to create stream for UDP %s -> %s: %v", lAddr, tAddr, err)
		return nil, false, 0, err
	}

	taddr, err := tnet.NewAddr(tAddr)
	if err != nil {
		flog.Debugf("invalid UDP address %s: %v", tAddr, err)
		strm.Close()
		return nil, false, 0, err
	}
	p := protocol.Proto{Type: protocol.PUDP, Addr: taddr}
	if c.cfg.Transport.KCP != nil && c.cfg.Transport.KCP.HeaderTimeout > 0 {
		_ = strm.SetWriteDeadline(time.Now().Add(time.Duration(c.cfg.Transport.KCP.HeaderTimeout) * time.Second))
	}
	err = p.Write(strm)
	_ = strm.SetWriteDeadline(time.Time{})
	if err != nil {
		flog.Debugf("failed to write UDP protocol header for %s -> %s on stream %d: %v", lAddr, tAddr, strm.SID(), err)
		strm.Close()
		return nil, false, 0, err
	}

	c.udpPool.mu.Lock()
	c.udpPool.strms[key] = strm
	c.udpPool.mu.Unlock()

	flog.Debugf("established UDP stream %d for %s -> %s", strm.SID(), lAddr, tAddr)
	return strm, true, key, nil
}

func (c *Client) CloseUDP(key uint64) error {
	return c.udpPool.delete(key)
}
