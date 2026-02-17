package conf

import "testing"

func TestKCPSetDefaults_BlockDefaultsToAES128GCM(t *testing.T) {
	var k KCP
	k.setDefaults("client", 1)
	if k.Mode != "fast2" {
		t.Fatalf("expected default mode fast2, got %q", k.Mode)
	}
	if k.Block_ != "aes-128-gcm" {
		t.Fatalf("expected default block aes-128-gcm, got %q", k.Block_)
	}
}

func TestKCPSetDefaults_AlignWithUIClient(t *testing.T) {
	var k KCP
	k.setDefaults("client", 10)

	if k.Mode != "fast2" {
		t.Fatalf("expected mode fast2, got %q", k.Mode)
	}
	if k.Rcvwnd != 512 || k.Sndwnd != 512 {
		t.Fatalf("expected window defaults 512/512, got %d/%d", k.Rcvwnd, k.Sndwnd)
	}
	if k.Smuxbuf != 1*1024*1024 {
		t.Fatalf("expected smuxbuf 1MiB, got %d", k.Smuxbuf)
	}
	if k.Streambuf != 64*1024 {
		t.Fatalf("expected streambuf 64KiB, got %d", k.Streambuf)
	}
}

func TestKCPSetDefaults_AlignWithUIServerLimits(t *testing.T) {
	var k KCP
	k.setDefaults("server", 10)

	if k.MaxSessions != 512 {
		t.Fatalf("expected max_sessions 512, got %d", k.MaxSessions)
	}
	if k.MaxStreamsTotal != 16384 {
		t.Fatalf("expected max_streams_total 16384, got %d", k.MaxStreamsTotal)
	}
	if k.MaxStreamsPerSession != 256 {
		t.Fatalf("expected max_streams_per_session 256, got %d", k.MaxStreamsPerSession)
	}
}
