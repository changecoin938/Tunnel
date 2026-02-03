package server

import (
	"context"
	"fmt"
	"paqet/internal/flog"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
	"time"
)

func (s *Server) handleConn(ctx context.Context, conn tnet.Conn) {
	var perSem chan struct{}
	if s.maxStreamsPerSession > 0 {
		perSem = make(chan struct{}, s.maxStreamsPerSession)
	}
	for {
		select {
		case <-ctx.Done():
			flog.Debugf("stopping smux session for %s due to context cancellation", conn.RemoteAddr())
			return
		default:
		}
		strm, err := conn.AcceptStrm()
		if err != nil {
			flog.Errorf("failed to accept stream on %s: %v", conn.RemoteAddr(), err)
			return
		}
		if perSem != nil {
			select {
			case perSem <- struct{}{}:
			default:
				flog.Warnf("dropping stream from %s: max_streams_per_session reached", conn.RemoteAddr())
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
				flog.Warnf("dropping stream %d from %s: max_streams_total reached", strm.SID(), conn.RemoteAddr())
				strm.Close()
				continue
			}
		}
		s.wg.Go(func() {
			defer func() {
				if s.streamSem != nil {
					<-s.streamSem
				}
				if perSem != nil {
					<-perSem
				}
			}()
			defer strm.Close()
			if err := s.handleStrm(ctx, strm); err != nil {
				flog.Errorf("stream %d from %s closed with error: %v", strm.SID(), strm.RemoteAddr(), err)
			} else {
				flog.Debugf("stream %d from %s closed", strm.SID(), strm.RemoteAddr())
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
		flog.Errorf("failed to read protocol message from stream %d: %v", strm.SID(), err)
		return err
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
		flog.Errorf("unknown protocol type %d on stream %d", p.Type, strm.SID())
		return fmt.Errorf("unknown protocol type: %d", p.Type)
	}
}
