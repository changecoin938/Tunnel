package server

import (
	"context"
	"net"
	"paqet/internal/diag"
	"paqet/internal/flog"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
)

func (s *Server) handleUDPProtocol(ctx context.Context, strm tnet.Strm, p *protocol.Proto) error {
	flog.Debugf("accepted UDP stream %d: %v -> %s", strm.SID(), strm.RemoteAddr(), p.Addr.String())
	return s.handleUDP(ctx, strm, p.Addr.String())
}

func (s *Server) handleUDP(ctx context.Context, strm tnet.Strm, addr string) error {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		flog.Errorf("failed to establish UDP connection to %s for stream %d: %v", addr, strm.SID(), err)
		return err
	}
	defer func() {
		conn.Close()
		flog.Debugf("closed UDP connection %s for stream %d", addr, strm.SID())
	}()
	flog.Debugf("UDP connection established to %s for stream %d", addr, strm.SID())

	errUp, errDown := diag.BidiCopy(
		ctx,
		conn,
		strm,
		func() error { return diag.CopyUDPUp(conn, strm) },
		func() error { return diag.CopyUDPDown(strm, conn) },
	)

	if ctx.Err() != nil {
		return nil
	}

	if !diag.IsBenignStreamErr(errUp) {
		flog.Errorf("UDP stream %d to %s failed (up): %v", strm.SID(), addr, errUp)
		return errUp
	}
	if !diag.IsBenignStreamErr(errDown) {
		flog.Errorf("UDP stream %d to %s failed (down): %v", strm.SID(), addr, errDown)
		return errDown
	}
	return nil
}
