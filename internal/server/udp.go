package server

import (
	"context"
	"net"
	"paqet/internal/flog"
	"paqet/internal/pkg/buffer"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
	"time"
)

func (s *Server) handleUDPProtocol(ctx context.Context, strm tnet.Strm, p *protocol.Proto) error {
	flog.Infof("accepted UDP stream %d: %s -> %s", strm.SID(), strm.RemoteAddr(), p.Addr.String())
	return s.handleUDP(ctx, strm, p.Addr.String())
}

func (s *Server) handleUDP(ctx context.Context, strm tnet.Strm, addr string) error {
	conn, err := (&net.Dialer{
		Timeout: 10 * time.Second,
	}).DialContext(ctx, "udp", addr)
	if err != nil {
		flog.Errorf("failed to establish UDP connection to %s for stream %d: %v", addr, strm.SID(), err)
		return err
	}
	defer func() {
		conn.Close()
		flog.Debugf("closed UDP connection %s for stream %d", addr, strm.SID())
	}()
	flog.Debugf("UDP connection established to %s for stream %d", addr, strm.SID())

	errChan := make(chan error, 2)
	go func() {
		err := buffer.CopyU(conn, strm)
		errChan <- err
	}()
	go func() {
		err := buffer.CopyU(strm, conn)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		now := time.Now()
		conn.SetDeadline(now)
		strm.SetDeadline(now)
		err2 := <-errChan
		conn.SetDeadline(time.Time{})
		strm.SetDeadline(time.Time{})
		if err != nil {
			flog.Errorf("UDP stream %d to %s failed: %v", strm.SID(), addr, err)
			return err
		}
		if err2 != nil {
			flog.Errorf("UDP stream %d to %s failed: %v", strm.SID(), addr, err2)
			return err2
		}
	case <-ctx.Done():
		return nil
	}

	return nil
}
