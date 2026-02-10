package forward

import (
	"context"
	"errors"
	"io"
	"net"
	"paqet/internal/diag"
	"paqet/internal/flog"
	"paqet/internal/pkg/buffer"
	"paqet/internal/tnet"
	"strings"
	"syscall"
	"time"
)

func (f *Forward) listenUDP(ctx context.Context) {
	laddr, err := net.ResolveUDPAddr("udp", f.listenAddr)
	if err != nil {
		flog.Errorf("failed to resolve UDP listen address '%s': %v", f.listenAddr, err)
		return
	}

	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		flog.Errorf("failed to bind UDP socket on %s: %v", laddr, err)
		return
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	flog.Infof("UDP forwarder listening on %s -> %s", laddr, f.targetAddr)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := f.handleUDPPacket(ctx, conn); err != nil {
			flog.Errorf("UDP packet handling failed on %s: %v", f.listenAddr, err)
		}
	}
}

func (f *Forward) handleUDPPacket(ctx context.Context, conn *net.UDPConn) error {
	bufp := buffer.UPool.Get().(*[]byte)
	defer buffer.UPool.Put(bufp)
	buf := *bufp

	n, caddr, err := conn.ReadFromUDP(buf)
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}

	strm, new, k, err := f.client.UDP(caddr.String(), f.targetAddr)
	if err != nil {
		flog.Errorf("failed to establish UDP stream for %s -> %s: %v", caddr, f.targetAddr, err)
		f.client.CloseUDP(k)
		return err
	}

	if _, err := strm.Write(buf[:n]); err != nil {
		flog.Errorf("failed to forward %d bytes from %s -> %s: %v", n, caddr, f.targetAddr, err)
		f.client.CloseUDP(k)
		return err
	}
	diag.AddUDPUp(int64(n))
	if new {
		flog.Infof("accepted UDP connection %d for %s -> %s", strm.SID(), caddr, f.targetAddr)
		go f.handleUDPStrm(ctx, k, strm, conn, caddr)
	}

	return nil
}

func (f *Forward) handleUDPStrm(ctx context.Context, k uint64, strm tnet.Strm, conn *net.UDPConn, caddr *net.UDPAddr) {
	bufp := buffer.UPool.Get().(*[]byte)
	defer func() {
		buffer.UPool.Put(bufp)
		flog.Debugf("UDP stream %d closed for %s -> %s", strm.SID(), caddr, f.targetAddr)
		f.client.CloseUDP(k)
	}()
	buf := *bufp

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		strm.SetDeadline(time.Now().Add(8 * time.Second))
		err := CopyU(strm, conn, caddr, buf)
		strm.SetDeadline(time.Time{})
		if err != nil {
			flog.Errorf("UDP stream %d failed for %s -> %s: %v", strm.SID(), caddr, f.targetAddr, err)
			return
		}
	}
}

func CopyU(dst io.ReadWriter, src *net.UDPConn, addr *net.UDPAddr, buf []byte) error {
	n, err := dst.Read(buf)
	if err != nil {
		return err
	}

	backoff := 200 * time.Microsecond
	for attempt := 0; attempt < 5; attempt++ {
		_, err = src.WriteToUDP(buf[:n], addr)
		if err == nil {
			diag.AddUDPDown(int64(n))
			return nil
		}
		if errors.Is(err, syscall.ENOBUFS) ||
			errors.Is(err, syscall.ENOMEM) ||
			strings.Contains(err.Error(), "No buffer space available") ||
			strings.Contains(err.Error(), "Cannot allocate memory") {
			time.Sleep(backoff)
			if backoff < 5*time.Millisecond {
				backoff *= 2
			}
			continue
		}
		return err
	}
	// Treat persistent ENOBUFS/ENOMEM as packet loss for UDP to avoid tearing down the stream.
	return nil
}
