package conf

import "testing"

func TestKCPSetDefaults_BlockDefaultsToAES128GCM(t *testing.T) {
	var k KCP
	k.setDefaults("client", 1)
	if k.Mode != "fast3" {
		t.Fatalf("expected default mode fast3, got %q", k.Mode)
	}
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
	if k.Smuxbuf != 4*1024*1024 {
		t.Fatalf("expected smuxbuf 4MiB, got %d", k.Smuxbuf)
	}
	if k.Streambuf != 256*1024 {
		t.Fatalf("expected streambuf 256KiB, got %d", k.Streambuf)
	}
}

func TestKCPSetDefaults_AlignWithUIServerLimits(t *testing.T) {
	var k KCP
	k.setDefaults("server", 10)

	if k.MaxSessions != 2048 {
		t.Fatalf("expected max_sessions 2048, got %d", k.MaxSessions)
	}
	if k.MaxStreamsTotal != 65536 {
		t.Fatalf("expected max_streams_total 65536, got %d", k.MaxStreamsTotal)
	}
	if k.MaxStreamsPerSession != 4096 {
		t.Fatalf("expected max_streams_per_session 4096, got %d", k.MaxStreamsPerSession)
	}
}
