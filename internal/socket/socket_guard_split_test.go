package socket

import (
	"bytes"
	"net"
	"sync"
	"testing"
)

func newTestGuardState() *guardState {
	g := &guardState{
		windowSeconds: 30,
		skew:          1,
	}
	copy(g.magic[:], []byte("PQT1"))
	copy(g.key[:], []byte("0123456789abcdef0123456789abcdef"))
	return g
}

func makeGuardHeader(g *guardState, cookie [8]byte) []byte {
	h := make([]byte, guardHeaderLen)
	copy(h[0:4], g.magic[:])
	copy(h[4:12], cookie[:])
	return h
}

func TestFindNextGuard_FindsSecondHeader(t *testing.T) {
	g := newTestGuardState()
	cookies := g.getCookies()

	hdr := makeGuardHeader(g, cookies.cookies[0])

	pkt1 := append(append([]byte(nil), hdr...), []byte("AAAA")...)
	pkt2 := append(append([]byte(nil), hdr...), []byte("BBBBBB")...)
	coalesced := append(pkt1, pkt2...)

	got := findNextGuard(coalesced, guardHeaderLen, g, cookies)
	want := len(pkt1)
	if got != want {
		t.Fatalf("findNextGuard()=%d, want %d", got, want)
	}
}

func TestFindNextGuard_SkipsBadCookie(t *testing.T) {
	g := newTestGuardState()
	cookies := g.getCookies()

	good := makeGuardHeader(g, cookies.cookies[0])
	badCookie := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	if bytes.Equal(badCookie[:], cookies.cookies[0][:]) {
		t.Fatalf("badCookie unexpectedly matched current cookie")
	}
	bad := makeGuardHeader(g, badCookie)

	pkt1 := append(append([]byte(nil), good...), []byte("AAAA")...)
	pktBad := append(append([]byte(nil), bad...), []byte("junk")...)
	pkt2 := append(append([]byte(nil), good...), []byte("BBBB")...)
	coalesced := append(append(pkt1, pktBad...), pkt2...)

	got := findNextGuard(coalesced, guardHeaderLen, g, cookies)
	want := len(pkt1) + len(pktBad)
	if got != want {
		t.Fatalf("findNextGuard()=%d, want %d", got, want)
	}
}

func TestEnqueueCoalesced_SplitsIntoPending(t *testing.T) {
	g := newTestGuardState()
	cookies := g.getCookies()
	hdr := makeGuardHeader(g, cookies.cookies[0])

	pkt1 := append(append([]byte(nil), hdr...), []byte("AAAA")...)
	pkt2 := append(append([]byte(nil), hdr...), []byte("BBBBBB")...)
	coalesced := append(pkt1, pkt2...)

	c := &PacketConn{
		bufPool: sync.Pool{
			New: func() any { return make([]byte, 0, 64*1024) },
		},
	}
	addr := &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 5678}

	if ok := c.enqueueCoalesced(coalesced, addr, g, cookies); !ok {
		t.Fatalf("enqueueCoalesced() returned false")
	}
	if c.pendingAddr != addr {
		t.Fatalf("pendingAddr=%v, want %v", c.pendingAddr, addr)
	}
	if len(c.pending) != 2 {
		t.Fatalf("pending len=%d, want 2", len(c.pending))
	}
	if !bytes.Equal(c.pending[0], []byte("AAAA")) {
		t.Fatalf("pending[0]=%q, want %q", string(c.pending[0]), "AAAA")
	}
	if !bytes.Equal(c.pending[1], []byte("BBBBBB")) {
		t.Fatalf("pending[1]=%q, want %q", string(c.pending[1]), "BBBBBB")
	}
}
