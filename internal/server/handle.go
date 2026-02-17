package server

import (
	"context"
	"fmt"
	"paqet/internal/diag"
	"paqet/internal/flog"
	"paqet/internal/protocol"
	"paqet/internal/socket"
	"paqet/internal/tnet"
	tkcp "paqet/internal/tnet/kcp"
	"time"
)

func (s *Server) handleConn(ctx context.Context, conn tnet.Conn) {
	var pConn *socket.PacketConn
	if kc, ok := conn.(*tkcp.Conn); ok {
		pConn = kc.PacketConn
	}
	if pConn != nil {
		remote := conn.RemoteAddr()
		defer pConn.ClearClientTCPF(remote)
	}

	var perSem chan struct{}
	if s.maxStreamsPerSession > 0 {
		perSem = make(chan struct{}, s.maxStreamsPerSession)
	}
	for {
		strm, err := conn.AcceptStrm()
		if err != nil {
			if ctx.Err() != nil || diag.IsBenignStreamErr(err) {
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
			if err := s.handleStrm(ctx, strm, pConn); err != nil {
				if ctx.Err() != nil || diag.IsBenignStreamErr(err) {
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

func (s *Server) handleStrm(ctx context.Context, strm tnet.Strm, pConn *socket.PacketConn) error {
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
			if pConn != nil {
				pConn.SetClientTCPF(strm.RemoteAddr(), p.TCPF)
			}
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
