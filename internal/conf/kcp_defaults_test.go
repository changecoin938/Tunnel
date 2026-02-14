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
		memMB int
		want  int
	}{
		{memMB: 0, want: 0},
		{memMB: 1024, want: 4 * 1024 * 1024},
		{memMB: 2047, want: 4 * 1024 * 1024},
		{memMB: 2048, want: 8 * 1024 * 1024},
		{memMB: 8191, want: 8 * 1024 * 1024},
		{memMB: 8192, want: 16 * 1024 * 1024},
	}
	for _, tc := range cases {
		got := pickSmuxBuf(tc.memMB)
		if got != tc.want {
			t.Fatalf("pickSmuxBuf(memMB=%d)=%d, want %d", tc.memMB, got, tc.want)
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
		{memMB: 8191, want: 128 * 1024},
		{memMB: 8192, want: 256 * 1024},
		{memMB: 16383, want: 256 * 1024},
		{memMB: 16384, want: 512 * 1024},
	}
	for _, tc := range cases {
		got := pickStreamBuf(tc.memMB)
		if got != tc.want {
			t.Fatalf("pickStreamBuf(memMB=%d)=%d, want %d", tc.memMB, got, tc.want)
		}
	}
}
