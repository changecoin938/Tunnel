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
	udpPoolTouchIntervalDefault = 5 * time.Second
)

type udpTrackedStrm struct {
	tnet.Strm
	lastUsed atomic.Int64 // unix nano
}

func (s *udpTrackedStrm) touch() {
	now := time.Now().UnixNano()
	last := s.lastUsed.Load()
	// Throttle time.Now() -> atomic.Store churn in high-QPS UDP (e.g., DNS).
	if last != 0 && now-last < int64(udpPoolTouchIntervalDefault) {
		return
	}
	s.lastUsed.Store(now)
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
	if p == nil {
		return nil
	}

	var strm *udpTrackedStrm
	p.mu.Lock()
	if p.strms != nil {
		strm = p.strms[key]
		delete(p.strms, key)
	}
	p.mu.Unlock()

	if strm != nil {
		flog.Debugf("closing UDP session stream %d", strm.SID())
		_ = strm.Close()
	}

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
	if p == nil {
		return
	}

	var toClose []*udpTrackedStrm
	p.mu.Lock()
	for k, strm := range p.strms {
		if strm != nil {
			toClose = append(toClose, strm)
		}
		delete(p.strms, k)
	}
	p.mu.Unlock()

	for _, strm := range toClose {
		_ = strm.Close()
	}
}

func (p *udpPool) evict(now time.Time) {
	if p == nil {
		return
	}

	var toClose []*udpTrackedStrm
	p.mu.Lock()
	toClose = p.evictLocked(now)
	p.mu.Unlock()

	for _, strm := range toClose {
		_ = strm.Close()
	}
}

func (p *udpPool) evictLocked(now time.Time) []*udpTrackedStrm {
	if p == nil {
		return nil
	}

	var toClose []*udpTrackedStrm

	if p.idleTimeout > 0 {
		cutoff := now.Add(-p.idleTimeout).UnixNano()
		for k, strm := range p.strms {
			if strm == nil || strm.lastUsed.Load() < cutoff {
				if strm != nil {
					flog.Debugf("evicting idle UDP stream %d", strm.SID())
					toClose = append(toClose, strm)
				}
				delete(p.strms, k)
			}
		}
	}

	if p.maxEntries > 0 && len(p.strms) > p.maxEntries {
		// Still over capacity: evict arbitrary entries (Go map iteration is randomized).
		over := len(p.strms) - p.maxEntries
		for k, strm := range p.strms {
			if over <= 0 {
				break
			}
			if strm != nil {
				flog.Debugf("evicting UDP stream %d (pool full)", strm.SID())
				toClose = append(toClose, strm)
			}
			delete(p.strms, k)
			over--
		}
	}

	return toClose
}
