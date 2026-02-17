package conf

import "testing"

func TestPickKCPWindow(t *testing.T) {
	cases := []struct {
		memMB     int
		connCount int
	}{
		{memMB: 0, connCount: 1},
		{memMB: -1, connCount: 1},
		{memMB: 1024, connCount: 1},
		{memMB: 16384, connCount: 32},
		{memMB: 9000, connCount: 0},
	}

	for _, tc := range cases {
		got := pickKCPWindow(tc.memMB, tc.connCount)
		if got != 8192 {
			t.Fatalf("pickKCPWindow(memMB=%d, conn=%d)=%d, want 8192", tc.memMB, tc.connCount, got)
		}
	}
}

func TestPickSmuxBuf(t *testing.T) {
	cases := []struct {
		memMB        int
		sessionCount int
	}{
		{memMB: 0, sessionCount: 1024},
		{memMB: 1024, sessionCount: 256},
		{memMB: 4096, sessionCount: 1},
		{memMB: 1024, sessionCount: 10_000_000},
	}
	for _, tc := range cases {
		got := pickSmuxBuf(tc.memMB, tc.sessionCount)
		if got != 16*1024*1024 {
			t.Fatalf("pickSmuxBuf(memMB=%d, sessions=%d)=%d, want %d", tc.memMB, tc.sessionCount, got, 16*1024*1024)
		}
	}
}

func TestPickStreamBuf(t *testing.T) {
	for _, memMB := range []int{0, 1024, 16384} {
		got := pickStreamBuf(memMB)
		if got != 4*1024*1024 {
			t.Fatalf("pickStreamBuf(memMB=%d)=%d, want %d", memMB, got, 4*1024*1024)
		}
	}
}

func TestPickMaxSessions(t *testing.T) {
	for _, memMB := range []int{0, 1024, 4096} {
		got := pickMaxSessions(memMB)
		if got != 128 {
			t.Fatalf("pickMaxSessions(memMB=%d)=%d, want 128", memMB, got)
		}
	}
}

func TestPickMaxStreamsTotal(t *testing.T) {
	cases := []struct {
		memMB     int
		streamBuf int
	}{
		{memMB: 0, streamBuf: 128 * 1024},
		{memMB: 1024, streamBuf: 128 * 1024},
		{memMB: 16384, streamBuf: 4 * 1024 * 1024},
	}
	for _, tc := range cases {
		got := pickMaxStreamsTotal(tc.memMB, tc.streamBuf)
		if got != 16384 {
			t.Fatalf("pickMaxStreamsTotal(memMB=%d, streamBuf=%d)=%d, want 16384", tc.memMB, tc.streamBuf, got)
		}
	}
}

func TestPickMaxStreamsPerSession(t *testing.T) {
	for _, total := range []int{0, 1024, 16384, 1_000_000} {
		got := pickMaxStreamsPerSession(total)
		if got != 4096 {
			t.Fatalf("pickMaxStreamsPerSession(total=%d)=%d, want 4096", total, got)
		}
	}
}

func TestKCPSetDefaults_BlockDefaultsToAES128GCM(t *testing.T) {
	var k KCP
	k.setDefaults("client", 1)
	if k.Block_ != "aes-128-gcm" {
		t.Fatalf("expected default block aes-128-gcm, got %q", k.Block_)
	}
}

func TestKCPSetDefaults_AlignWithUIClient(t *testing.T) {
	var k KCP
	k.setDefaults("client", 10)

	if k.Mode != "fast3" {
		t.Fatalf("expected mode fast3, got %q", k.Mode)
	}
	if k.Rcvwnd != 8192 || k.Sndwnd != 8192 {
		t.Fatalf("expected window defaults 8192/8192, got %d/%d", k.Rcvwnd, k.Sndwnd)
	}
	if k.Smuxbuf != 16*1024*1024 {
		t.Fatalf("expected smuxbuf 16MiB, got %d", k.Smuxbuf)
	}
	if k.Streambuf != 4*1024*1024 {
		t.Fatalf("expected streambuf 4MiB, got %d", k.Streambuf)
	}
}

func TestKCPSetDefaults_AlignWithUIServerLimits(t *testing.T) {
	var k KCP
	k.setDefaults("server", 10)

	if k.MaxSessions != 128 {
		t.Fatalf("expected max_sessions 128, got %d", k.MaxSessions)
	}
	if k.MaxStreamsTotal != 16384 {
		t.Fatalf("expected max_streams_total 16384, got %d", k.MaxStreamsTotal)
	}
	if k.MaxStreamsPerSession != 4096 {
		t.Fatalf("expected max_streams_per_session 4096, got %d", k.MaxStreamsPerSession)
	}
}
