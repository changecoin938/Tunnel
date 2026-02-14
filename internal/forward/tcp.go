package forward

import (
	"context"
	"net"

	"paqet/internal/diag"
	"paqet/internal/flog"
)

func (f *Forward) listenTCP(ctx context.Context) error {
	listener, err := net.Listen("tcp", f.listenAddr)
	if err != nil {
		flog.Errorf("failed to bind TCP socket on %s: %v", f.listenAddr, err)
		return err
	}
	defer listener.Close()
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	flog.Infof("TCP forwarder listening on %s -> %s", f.listenAddr, f.targetAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				flog.Errorf("failed to accept TCP connection on %s: %v", f.listenAddr, err)
				continue
			}
		}

		f.wg.Go(func() {
			defer conn.Close()
			if err := f.handleTCPConn(ctx, conn); err != nil {
				flog.Errorf("TCP connection %s -> %s closed with error: %v", conn.RemoteAddr(), f.targetAddr, err)
			} else {
				flog.Debugf("TCP connection %s -> %s closed", conn.RemoteAddr(), f.targetAddr)
			}
		})
	}
}

func (f *Forward) handleTCPConn(ctx context.Context, conn net.Conn) error {
	strm, err := f.client.TCP(f.targetAddr)
	if err != nil {
		flog.Errorf("failed to establish stream for %s -> %s: %v", conn.RemoteAddr(), f.targetAddr, err)
		return err
	}
	defer func() {
		flog.Debugf("TCP stream closed for %s -> %s", conn.RemoteAddr(), f.targetAddr)
		_ = strm.Close()
	}()
	flog.Debugf("accepted TCP connection %s -> %s", conn.RemoteAddr(), f.targetAddr)

	errDown, errUp := diag.BidiCopy(
		ctx,
		conn,
		strm,
		func() error { return diag.CopyTCPDown(conn, strm) },
		func() error { return diag.CopyTCPUp(strm, conn) },
	)

	if ctx.Err() != nil {
		return nil
	}

	// ENOBUFS/ENOMEM is transient — never tear down the connection for it.
	if diag.IsNoBufferOrNoMem(errDown) || diag.IsNoBufferOrNoMem(errUp) {
		flog.Debugf("TCP stream %d for %s -> %s hit ENOBUFS (benign): down=%v up=%v", strm.SID(), conn.RemoteAddr(), f.targetAddr, errDown, errUp)
		return nil
	}

	if !diag.IsBenignStreamErr(errDown) {
		flog.Errorf("TCP stream %d failed for %s -> %s (down): %v", strm.SID(), conn.RemoteAddr(), f.targetAddr, errDown)
		return errDown
	}
	if !diag.IsBenignStreamErr(errUp) {
		flog.Errorf("TCP stream %d failed for %s -> %s (up): %v", strm.SID(), conn.RemoteAddr(), f.targetAddr, errUp)
		return errUp
	}
	return nil
}
