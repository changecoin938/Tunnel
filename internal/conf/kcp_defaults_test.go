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
	if k.Rcvwnd != 1024 || k.Sndwnd != 1024 {
		t.Fatalf("expected window defaults 1024/1024, got %d/%d", k.Rcvwnd, k.Sndwnd)
	}
	if k.Smuxbuf != 2*1024*1024 {
		t.Fatalf("expected smuxbuf 2MiB, got %d", k.Smuxbuf)
	}
	if k.Streambuf != 128*1024 {
		t.Fatalf("expected streambuf 128KiB, got %d", k.Streambuf)
	}
}

func TestKCPSetDefaults_AlignWithUIServerLimits(t *testing.T) {
	var k KCP
	k.setDefaults("server", 10)

	if k.MaxSessions != 512 {
		t.Fatalf("expected max_sessions 512, got %d", k.MaxSessions)
	}
	if k.MaxStreamsTotal != 4096 {
		t.Fatalf("expected max_streams_total 4096, got %d", k.MaxStreamsTotal)
	}
	if k.MaxStreamsPerSession != 256 {
		t.Fatalf("expected max_streams_per_session 256, got %d", k.MaxStreamsPerSession)
	}
}
