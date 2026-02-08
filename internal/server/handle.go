package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"paqet/internal/diag"
	"paqet/internal/flog"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
	"time"
)

func streamErrIsBenign(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return false
}

func (s *Server) handleConn(ctx context.Context, conn tnet.Conn) {
	var perSem chan struct{}
	if s.maxStreamsPerSession > 0 {
		perSem = make(chan struct{}, s.maxStreamsPerSession)
	}
	for {
		select {
		case <-ctx.Done():
			flog.Debugf("stopping smux session for %v due to context cancellation", conn.RemoteAddr())
			return
		default:
		}
		strm, err := conn.AcceptStrm()
		if err != nil {
			if ctx.Err() != nil || streamErrIsBenign(err) {
				flog.Debugf("stream accept closed for %v: %v", conn.RemoteAddr(), err)
				return
			}
			flog.Errorf("failed to accept stream on %v: %v", conn.RemoteAddr(), err)
			return
		}
		if perSem != nil {
			select {
			case perSem <- struct{}{}:
			default:
				flog.Warnf("dropping stream from %v: max_streams_per_session reached", conn.RemoteAddr())
				strm.Close()
				continue
			}
		}
		if s.streamSem != nil {
			select {
			case s.streamSem <- struct{}{}:
			default:
				if perSem != nil {
					<-perSem
				}
				flog.Warnf("dropping stream %d from %v: max_streams_total reached", strm.SID(), conn.RemoteAddr())
				strm.Close()
				continue
			}
		}
		diag.IncStreams()
		s.wg.Go(func() {
			defer func() {
				diag.DecStreams()
				if s.streamSem != nil {
					<-s.streamSem
				}
				if perSem != nil {
					<-perSem
				}
			}()
			defer strm.Close()
			if err := s.handleStrm(ctx, strm); err != nil {
				if ctx.Err() != nil || streamErrIsBenign(err) {
					flog.Debugf("stream %d from %v closed: %v", strm.SID(), strm.RemoteAddr(), err)
				} else {
					flog.Errorf("stream %d from %v closed with error: %v", strm.SID(), strm.RemoteAddr(), err)
				}
			} else {
				flog.Debugf("stream %d from %v closed", strm.SID(), strm.RemoteAddr())
			}
		})
	}
}

func (s *Server) handleStrm(ctx context.Context, strm tnet.Strm) error {
	var p protocol.Proto
	if s.headerTimeout > 0 {
		_ = strm.SetReadDeadline(time.Now().Add(s.headerTimeout))
	}
	err := p.Read(strm)
	_ = strm.SetReadDeadline(time.Time{})
	if err != nil {
		return fmt.Errorf("read protocol header: %w", err)
	}

	switch p.Type {
	case protocol.PPING:
		return s.handlePing(strm)
	case protocol.PTCPF:
		if len(p.TCPF) != 0 {
			s.pConn.SetClientTCPF(strm.RemoteAddr(), p.TCPF)
		}
		return nil
	case protocol.PTCP:
		return s.handleTCPProtocol(ctx, strm, &p)
	case protocol.PUDP:
		return s.handleUDPProtocol(ctx, strm, &p)
	default:
		return fmt.Errorf("unknown protocol type: %d", p.Type)
	}
}
