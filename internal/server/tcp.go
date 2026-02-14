package server

import (
	"context"
	"net"
	"paqet/internal/diag"
	"paqet/internal/flog"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
	"time"
)

func (s *Server) handleTCPProtocol(ctx context.Context, strm tnet.Strm, p *protocol.Proto) error {
	flog.Debugf("accepted TCP stream %d: %v -> %s", strm.SID(), strm.RemoteAddr(), p.Addr.String())
	return s.handleTCP(ctx, strm, p.Addr.String())
}

func (s *Server) handleTCP(ctx context.Context, strm tnet.Strm, addr string) error {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		flog.Errorf("failed to establish TCP connection to %s for stream %d: %v", addr, strm.SID(), err)
		return err
	}
	defer func() {
		conn.Close()
		flog.Debugf("closed TCP connection %s for stream %d", addr, strm.SID())
	}()
	flog.Debugf("TCP connection established to %s for stream %d", addr, strm.SID())

	errUp, errDown := diag.BidiCopy(
		ctx,
		conn,
		strm,
		func() error { return diag.CopyTCPUp(conn, strm) },
		func() error { return diag.CopyTCPDown(strm, conn) },
	)

	if ctx.Err() != nil {
		return nil
	}

	// ENOBUFS/ENOMEM is transient kernel memory pressure — never tear down the
	// TCP stream for it. With sustained retry in the copy layer this should not
	// happen, but treat it as benign just in case.
	if diag.IsNoBufferOrNoMem(errUp) || diag.IsNoBufferOrNoMem(errDown) {
		flog.Debugf("TCP stream %d to %s hit ENOBUFS (benign): up=%v down=%v", strm.SID(), addr, errUp, errDown)
		return nil
	}

	if !diag.IsBenignStreamErr(errUp) {
		flog.Errorf("TCP stream %d to %s failed (up): %v", strm.SID(), addr, errUp)
		return errUp
	}
	if !diag.IsBenignStreamErr(errDown) {
		flog.Errorf("TCP stream %d to %s failed (down): %v", strm.SID(), addr, errDown)
		return errDown
	}

	return nil
}
