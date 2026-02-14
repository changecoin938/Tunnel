package client

import (
	"context"
	"paqet/internal/flog"
	"paqet/internal/tnet"
	"sync"
	"sync/atomic"
	"time"
)

const (
	udpPoolMaxEntriesDefault    = 4096
	udpPoolIdleTimeoutDefault   = 60 * time.Second
	udpPoolSweepIntervalDefault = 30 * time.Second
)

type udpTrackedStrm struct {
	tnet.Strm
	lastUsed atomic.Int64 // unix nano
}

func (s *udpTrackedStrm) touch() {
	s.lastUsed.Store(time.Now().UnixNano())
}

func (s *udpTrackedStrm) Read(p []byte) (int, error) {
	n, err := s.Strm.Read(p)
	if n > 0 {
		s.touch()
	}
	return n, err
}

func (s *udpTrackedStrm) Write(p []byte) (int, error) {
	n, err := s.Strm.Write(p)
	if n > 0 {
		s.touch()
	}
	return n, err
}

type udpPool struct {
	strms       map[uint64]*udpTrackedStrm
	maxEntries  int
	idleTimeout time.Duration
	mu          sync.RWMutex
}

func (p *udpPool) delete(key uint64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strm, exists := p.strms[key]; exists && strm != nil {
		flog.Debugf("closing UDP session stream %d", strm.SID())
		strm.Close()
	} else {
		flog.Debugf("UDP session key %d not found for close", key)
	}
	delete(p.strms, key)

	return nil
}

func (p *udpPool) sweep(ctx context.Context) {
	if p == nil {
		return
	}
	ticker := time.NewTicker(udpPoolSweepIntervalDefault)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			p.closeAll()
			return
		case <-ticker.C:
			p.evict(time.Now())
		}
	}
}

func (p *udpPool) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, strm := range p.strms {
		if strm != nil {
			_ = strm.Close()
		}
		delete(p.strms, k)
	}
}

func (p *udpPool) evict(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.evictLocked(now)
}

func (p *udpPool) evictLocked(now time.Time) {
	if p.idleTimeout > 0 {
		cutoff := now.Add(-p.idleTimeout).UnixNano()
		for k, strm := range p.strms {
			if strm == nil || strm.lastUsed.Load() < cutoff {
				if strm != nil {
					flog.Debugf("evicting idle UDP stream %d", strm.SID())
					_ = strm.Close()
				}
				delete(p.strms, k)
			}
		}
	}

	if p.maxEntries > 0 && len(p.strms) > p.maxEntries {
		// Evict oldest entries until under the limit.
		for len(p.strms) > p.maxEntries {
			var oldestKey uint64
			var oldestAt int64
			first := true
			for k, strm := range p.strms {
				if strm == nil {
					oldestKey = k
					first = false
					break
				}
				at := strm.lastUsed.Load()
				if first || at < oldestAt {
					oldestKey = k
					oldestAt = at
					first = false
				}
			}
			if first {
				break
			}
			if strm := p.strms[oldestKey]; strm != nil {
				flog.Debugf("evicting UDP stream %d (pool full)", strm.SID())
				_ = strm.Close()
			}
			delete(p.strms, oldestKey)
		}
	}
}
