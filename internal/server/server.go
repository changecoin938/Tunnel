package server

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"paqet/internal/conf"
	"paqet/internal/diag"
	"paqet/internal/flog"
	"paqet/internal/socket"
	"paqet/internal/tnet"
	"paqet/internal/tnet/kcp"
	"sync"
	"syscall"
	"time"
)

type Server struct {
	cfg   *conf.Conf
	wg    sync.WaitGroup

	sessSem              chan struct{}
	streamSem            chan struct{}
	maxStreamsPerSession int
	headerTimeout        time.Duration
}

func New(cfg *conf.Conf) (*Server, error) {
	s := &Server{
		cfg: cfg,
	}

	if cfg.Transport.KCP != nil {
		k := cfg.Transport.KCP
		s.headerTimeout = time.Duration(k.HeaderTimeout) * time.Second
		if s.headerTimeout <= 0 {
			s.headerTimeout = 10 * time.Second
		}
		if k.MaxSessions > 0 {
			s.sessSem = make(chan struct{}, k.MaxSessions)
		}
		if k.MaxStreamsTotal > 0 {
			s.streamSem = make(chan struct{}, k.MaxStreamsTotal)
		}
		if k.MaxStreamsPerSession > 0 {
			s.maxStreamsPerSession = k.MaxStreamsPerSession
		}
	}

	return s, nil
}

func (s *Server) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		flog.Infof("Shutdown signal received, initiating graceful shutdown...")
		cancel()
	}()

	basePort := s.cfg.Network.Port
	if basePort == 0 {
		return fmt.Errorf("server network port cannot be 0 (set network.ipv4.addr/network.ipv6.addr port)")
	}
	connCount := s.cfg.Transport.Conn
	if connCount < 1 {
		connCount = 1
	}
	lastPort := basePort + connCount - 1
	if lastPort > 65535 {
		return fmt.Errorf("server port range too large: base=%d conn=%d => last=%d (max 65535)", basePort, connCount, lastPort)
	}

	var listeners []tnet.Listener
	for i := 0; i < connCount; i++ {
		netCfg := s.cfg.Network
		netCfg.Port = basePort + i

		pConn, err := socket.New(ctx, &netCfg)
		if err != nil {
			return fmt.Errorf("could not create raw packet conn (port=%d): %w", netCfg.Port, err)
		}

		l, err := kcp.Listen(s.cfg.Transport.KCP, pConn)
		if err != nil {
			pConn.Close()
			return fmt.Errorf("could not start KCP listener (port=%d): %w", netCfg.Port, err)
			}
			listeners = append(listeners, l)

			listener := l
			s.wg.Go(func() {
				s.listen(ctx, listener)
			})
		}
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()

	if connCount > 1 {
		flog.Infof("Server started - listening for packets on :%d-%d (%d conns)", basePort, lastPort, connCount)
	} else {
		flog.Infof("Server started - listening for packets on :%d", basePort)
	}

	s.wg.Wait()
	flog.Infof("Server shutdown completed")
	return nil
}

func (s *Server) listen(ctx context.Context, listener tnet.Listener) {
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	acceptBackoff := 100 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			flog.Warnf("failed to accept connection: %v", err)
			time.Sleep(acceptBackoff)
			if acceptBackoff < 5*time.Second {
				acceptBackoff *= 2
				if acceptBackoff > 5*time.Second {
					acceptBackoff = 5 * time.Second
				}
			}
			continue
		}
		acceptBackoff = 100 * time.Millisecond
		if s.sessSem != nil {
			select {
			case s.sessSem <- struct{}{}:
			default:
				flog.Warnf("dropping new connection from %v: max_sessions reached", conn.RemoteAddr())
				conn.Close()
				continue
			}
		}
		diag.IncSessions()
		flog.Infof("accepted new connection from %v (local: %v)", conn.RemoteAddr(), conn.LocalAddr())

		s.wg.Go(func() {
			defer func() {
				diag.DecSessions()
				if s.sessSem != nil {
					<-s.sessSem
				}
			}()
			defer conn.Close()
			s.handleConn(ctx, conn)
		})
	}
}
