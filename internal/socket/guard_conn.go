package socket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"net"
	"paqet/internal/conf"
	"paqet/internal/diag"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

const guardHeaderLen = 12 // 4 bytes magic + 8 bytes cookie

type guardCookies struct {
	win     uint64
	cookies [][8]byte // cookies[0] is current window
}

type guardState struct {
	magic         [4]byte
	windowSeconds int64
	skew          int

	key [32]byte

	state atomic.Value // stores *guardCookies
}

func newGuardState(k *conf.KCP) *guardState {
	if k == nil || k.Guard == nil || !*k.Guard {
		return nil
	}
	// Validation ensures these are sane, but keep this defensive.
	if len(k.GuardMagic) != 4 || k.GuardWindow <= 0 || k.GuardSkew < 0 {
		return nil
	}
	if len(k.Key) == 0 {
		return nil
	}

	st := &guardState{
		windowSeconds: int64(k.GuardWindow),
		skew:          k.GuardSkew,
	}
	copy(st.magic[:], k.GuardMagic)

	// Derive a dedicated key for guard cookies.
	dk := pbkdf2.Key([]byte(k.Key), []byte("paqet_guard"), 100_000, 32, sha256.New)
	copy(st.key[:], dk)

	// Warm the cache so the first packet doesn't pay extra overhead.
	st.getCookies()
	return st
}

// GuardConn wraps a net.PacketConn and prepends a small authenticated header to each packet.
// It can cheaply drop random junk traffic BEFORE KCP decrypt/authentication runs.
//
// Header format: magic(4) + cookie(8) + kcpPacket(...)
type GuardConn struct {
	net.PacketConn

	st *guardState

	bufPool sync.Pool // []byte, used for WriteTo
}

func NewGuardConn(pc net.PacketConn, k *conf.KCP) net.PacketConn {
	if pc == nil {
		return pc
	}
	st := newGuardState(k)
	if st == nil {
		return pc
	}

	// Fast-path: if we're using our own PacketConn implementation, install the guard
	// in-place (no wrapper, no extra per-packet allocation/copy).
	if c, ok := pc.(*PacketConn); ok {
		c.setGuard(st)
		return c
	}

	g := &GuardConn{
		PacketConn: pc,
		st:         st,
		bufPool: sync.Pool{
			New: func() any { return make([]byte, 0, 2048) },
		},
	}
	return g
}

func (g *GuardConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	for {
		n, addr, err = g.PacketConn.ReadFrom(p)
		if err != nil {
			return 0, nil, err
		}
		if n < guardHeaderLen {
			diag.AddGuardDrop()
			// Drop undersized packet.
			continue
		}
		if !hmac.Equal(p[0:4], g.st.magic[:]) {
			diag.AddGuardDrop()
			continue
		}

		cookies := g.st.getCookies()
		ok := false
		for i := range cookies.cookies {
			if hmac.Equal(p[4:12], cookies.cookies[i][:]) {
				ok = true
				break
			}
		}
		if !ok {
			diag.AddGuardDrop()
			continue
		}

		// Strip the guard header in-place.
		copy(p, p[guardHeaderLen:n])
		diag.AddGuardPass()
		return n - guardHeaderLen, addr, nil
	}
}

func (g *GuardConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	cookies := g.st.getCookies()

	buf := g.bufPool.Get().([]byte)
	need := guardHeaderLen + len(p)
	if cap(buf) < need {
		buf = make([]byte, 0, need)
	}
	buf = buf[:need]

	copy(buf[0:4], g.st.magic[:])
	copy(buf[4:12], cookies.cookies[0][:])
	copy(buf[guardHeaderLen:], p)

	_, err = g.PacketConn.WriteTo(buf, addr)
	buf = buf[:0]
	g.bufPool.Put(buf)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (g *guardState) getCookies() *guardCookies {
	nowWin := uint64(time.Now().Unix() / g.windowSeconds)
	if v := g.state.Load(); v != nil {
		c := v.(*guardCookies)
		if c.win == nowWin {
			return c
		}
	}

	out := &guardCookies{
		win:     nowWin,
		cookies: make([][8]byte, g.skew+1),
	}
	for i := 0; i <= g.skew; i++ {
		out.cookies[i] = g.cookie(nowWin - uint64(i))
	}
	g.state.Store(out)
	return out
}

func (g *guardState) cookie(win uint64) [8]byte {
	var winb [8]byte
	binary.BigEndian.PutUint64(winb[:], win)

	mac := hmac.New(sha256.New, g.key[:])
	mac.Write(g.magic[:])
	mac.Write(winb[:])
	sum := mac.Sum(nil)

	var out [8]byte
	copy(out[:], sum[:8])
	return out
}
