package server

import (
	"context"
	"fmt"
	"sync/atomic"

	"paqet/internal/flog"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
)

func (s *Server) handleConn(ctx context.Context, conn tnet.Conn) {
	var activeStreams atomic.Int64
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
		if max := int64(s.cfg.Transport.KCP.MaxStreamsPerSession); max > 0 && activeStreams.Load() >= max {
			flog.Warnf("rejecting stream %d on %s: max_streams_per_session limit reached (%d)", strm.SID(), conn.RemoteAddr(), max)
			strm.Close()
			continue
		}
		activeStreams.Add(1)
		s.wg.Go(func() {
			defer activeStreams.Add(-1)
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
	err := p.Read(strm)
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
