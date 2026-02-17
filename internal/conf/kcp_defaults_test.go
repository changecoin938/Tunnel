package conf

import "testing"

func TestPickKCPWindow(t *testing.T) {
	cases := []struct {
		memMB     int
		connCount int
		want      int
	}{
		{memMB: 0, connCount: 1, want: 0},
		{memMB: -1, connCount: 1, want: 0},
		{memMB: 1024, connCount: 1, want: 2048},
		{memMB: 4095, connCount: 1, want: 2048},
		{memMB: 4096, connCount: 1, want: 4096},
		{memMB: 8191, connCount: 1, want: 4096},
		{memMB: 8192, connCount: 1, want: 8192},
		{memMB: 16383, connCount: 1, want: 8192},
		{memMB: 16384, connCount: 1, want: 16384},
		{memMB: 16384, connCount: 16, want: 8192},
		{memMB: 16384, connCount: 32, want: 4096},
		{memMB: 9000, connCount: 32, want: 4096},
		{memMB: 9000, connCount: 0, want: 8192}, // connCount<1 treated as 1
	}

	for _, tc := range cases {
		got := pickKCPWindow(tc.memMB, tc.connCount)
		if got != tc.want {
			t.Fatalf("pickKCPWindow(memMB=%d, conn=%d)=%d, want %d", tc.memMB, tc.connCount, got, tc.want)
		}
	}
}

func TestPickSmuxBuf(t *testing.T) {
	cases := []struct {
		memMB        int
		sessionCount int
		want         int
	}{
		{memMB: 0, sessionCount: 1024, want: 0},
		{memMB: 1024, sessionCount: 256, want: 1 * 1024 * 1024},   // 25% of 1GB / 256 = 1MB
		{memMB: 4096, sessionCount: 1024, want: 1 * 1024 * 1024},  // 25% of 4GB / 1024 = 1MB
		{memMB: 4096, sessionCount: 512, want: 2 * 1024 * 1024},   // 25% of 4GB / 512 = 2MB
		{memMB: 16384, sessionCount: 1024, want: 4 * 1024 * 1024}, // 25% of 16GB / 1024 = 4MB
		{memMB: 32768, sessionCount: 1024, want: 4 * 1024 * 1024}, // clamped (would be 8MB)
		{memMB: 4096, sessionCount: 1, want: 4 * 1024 * 1024},     // clamped (would be 1GB)
		{memMB: 1024, sessionCount: 1024, want: 256 * 1024},       // clamped (would be 256KB)
		{memMB: 1024, sessionCount: 10_000_000, want: 256 * 1024}, // clamped min
		{memMB: 4096, sessionCount: 10_000_000, want: 256 * 1024}, // clamped min
		{memMB: 4096, sessionCount: 10_000_000, want: 256 * 1024}, // clamped min (duplicate intentional)
	}
	for _, tc := range cases {
		got := pickSmuxBuf(tc.memMB, tc.sessionCount)
		if got != tc.want {
			t.Fatalf("pickSmuxBuf(memMB=%d, sessions=%d)=%d, want %d", tc.memMB, tc.sessionCount, got, tc.want)
		}
	}
}

func TestPickStreamBuf(t *testing.T) {
	cases := []struct {
		memMB int
		want  int
	}{
		{memMB: 0, want: 0},
		{memMB: 1024, want: 128 * 1024},
		{memMB: 16383, want: 128 * 1024},
		{memMB: 16384, want: 256 * 1024},
	}
	for _, tc := range cases {
		got := pickStreamBuf(tc.memMB)
		if got != tc.want {
			t.Fatalf("pickStreamBuf(memMB=%d)=%d, want %d", tc.memMB, got, tc.want)
		}
	}
}

func TestPickMaxSessions(t *testing.T) {
	cases := []struct {
		memMB int
		want  int
	}{
		{memMB: 0, want: 0},
		{memMB: 1024, want: 256},
		{memMB: 2047, want: 256},
		{memMB: 2048, want: 512},
		{memMB: 4095, want: 512},
		{memMB: 4096, want: 1024},
	}
	for _, tc := range cases {
		got := pickMaxSessions(tc.memMB)
		if got != tc.want {
			t.Fatalf("pickMaxSessions(memMB=%d)=%d, want %d", tc.memMB, got, tc.want)
		}
	}
}

func TestPickMaxStreamsTotal(t *testing.T) {
	cases := []struct {
		memMB     int
		streamBuf int
		want      int
	}{
		{memMB: 0, streamBuf: 128 * 1024, want: 0},
		{memMB: 1024, streamBuf: 128 * 1024, want: 2048},   // 256MB / 128KB
		{memMB: 4096, streamBuf: 128 * 1024, want: 8192},   // 1GB / 128KB
		{memMB: 8192, streamBuf: 128 * 1024, want: 16384},  // 2GB / 128KB
		{memMB: 16384, streamBuf: 128 * 1024, want: 32768}, // 4GB / 128KB
		{memMB: 32768, streamBuf: 128 * 1024, want: 65536}, // 8GB / 128KB (cap)
	}
	for _, tc := range cases {
		got := pickMaxStreamsTotal(tc.memMB, tc.streamBuf)
		if got != tc.want {
			t.Fatalf("pickMaxStreamsTotal(memMB=%d, streamBuf=%d)=%d, want %d", tc.memMB, tc.streamBuf, got, tc.want)
		}
	}
}

func TestPickMaxStreamsPerSession(t *testing.T) {
	cases := []struct {
		maxStreamsTotal int
		want            int
	}{
		{maxStreamsTotal: 0, want: 0},
		{maxStreamsTotal: 1024, want: 256},   // min clamp
		{maxStreamsTotal: 4096, want: 256},   // 4096/16 = 256
		{maxStreamsTotal: 8192, want: 512},   // 8192/16 = 512
		{maxStreamsTotal: 16384, want: 1024}, // 16384/16 = 1024
		{maxStreamsTotal: 65536, want: 4096}, // 65536/16 = 4096 (cap)
		{maxStreamsTotal: 1_000_000, want: 4096},
	}
	for _, tc := range cases {
		got := pickMaxStreamsPerSession(tc.maxStreamsTotal)
		if got != tc.want {
			t.Fatalf("pickMaxStreamsPerSession(total=%d)=%d, want %d", tc.maxStreamsTotal, got, tc.want)
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
