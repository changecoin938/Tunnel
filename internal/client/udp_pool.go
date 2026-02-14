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
	udpPoolSweepScanLimit       = 512
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
	toClose = append(toClose, p.evictIdleLocked(now, udpPoolSweepScanLimit)...)
	if p.maxEntries > 0 && len(p.strms) > p.maxEntries {
		toClose = append(toClose, p.evictOverflowLocked(len(p.strms)-p.maxEntries)...)
	}
	p.mu.Unlock()

	for _, strm := range toClose {
		_ = strm.Close()
	}
}

func (p *udpPool) evictForInsertLocked(now time.Time) []*udpTrackedStrm {
	if p == nil || p.maxEntries <= 0 {
		return nil
	}
	// Keep insertion path O(overflow) and avoid full-map scans under write lock.
	if len(p.strms) >= p.maxEntries {
		return p.evictOverflowLocked(len(p.strms) - p.maxEntries + 1)
	}
	// Opportunistic bounded idle cleanup when we are close to capacity.
	return p.evictIdleLocked(now, 32)
}

func (p *udpPool) evictIdleLocked(now time.Time, maxScan int) []*udpTrackedStrm {
	if p == nil {
		return nil
	}
	var toClose []*udpTrackedStrm

	if p.idleTimeout > 0 {
		cutoff := now.Add(-p.idleTimeout).UnixNano()
		scanned := 0
		for k, strm := range p.strms {
			if strm == nil || strm.lastUsed.Load() < cutoff {
				if strm != nil {
					flog.Debugf("evicting idle UDP stream %d", strm.SID())
					toClose = append(toClose, strm)
				}
				delete(p.strms, k)
			}
			scanned++
			if maxScan > 0 && scanned >= maxScan {
				break
			}
		}
	}
	return toClose
}

func (p *udpPool) evictOverflowLocked(overflow int) []*udpTrackedStrm {
	if p == nil || overflow <= 0 {
		return nil
	}
	var toClose []*udpTrackedStrm
	for k, strm := range p.strms {
		if overflow <= 0 {
			break
		}
		if strm != nil {
			flog.Debugf("evicting UDP stream %d (pool full)", strm.SID())
			toClose = append(toClose, strm)
		}
		delete(p.strms, k)
		overflow--
	}
	return toClose
}
