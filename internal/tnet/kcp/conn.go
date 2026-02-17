package kcp

import (
	"errors"
	"fmt"
	"net"
	"paqet/internal/protocol"
	"paqet/internal/socket"
	"paqet/internal/tnet"
	"time"

	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
)

type Conn struct {
	PacketConn    *socket.PacketConn
	OwnPacketConn bool
	UDPSession    *kcp.UDPSession
	Session       *smux.Session
}

func (c *Conn) OpenStrm() (tnet.Strm, error) {
	strm, err := c.Session.OpenStream()
	if err != nil {
		return nil, err
	}
	return &Strm{strm}, nil
}

func (c *Conn) AcceptStrm() (tnet.Strm, error) {
	strm, err := c.Session.AcceptStream()
	if err != nil {
		return nil, err
	}
	return &Strm{strm}, nil
}

func (c *Conn) Ping(wait bool) error {
	strm, err := c.Session.OpenStream()
	if err != nil {
		return fmt.Errorf("ping failed: %v", err)
	}
	defer strm.Close()
	if wait {
		_ = strm.SetDeadline(time.Now().Add(5 * time.Second))
		defer strm.SetDeadline(time.Time{})
		p := protocol.Proto{Type: protocol.PPING}
		err = p.Write(strm)
		if err != nil {
			return fmt.Errorf("connection test failed: %v", err)
		}
		err = p.Read(strm)
		if err != nil {
			return fmt.Errorf("connection test failed: %v", err)
		}
		if p.Type != protocol.PPONG {
			return fmt.Errorf("connection test failed: unexpected response type %d", p.Type)
		}
	}
	return nil
}

func (c *Conn) Close() error {
	var errs []error
	if c.UDPSession != nil {
		if err := c.UDPSession.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.Session != nil {
		if err := c.Session.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.PacketConn != nil && c.OwnPacketConn {
		if err := c.PacketConn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *Conn) LocalAddr() net.Addr                { return c.Session.LocalAddr() }
func (c *Conn) RemoteAddr() net.Addr               { return c.Session.RemoteAddr() }
func (c *Conn) SetDeadline(t time.Time) error      { return c.Session.SetDeadline(t) }
func (c *Conn) SetReadDeadline(t time.Time) error  { return c.UDPSession.SetReadDeadline(t) }
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.UDPSession.SetWriteDeadline(t) }
