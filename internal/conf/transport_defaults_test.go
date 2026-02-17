package conf

import (
	"runtime"
	"testing"
)

func TestTransportSetDefaults_ConnDefaultsToNumCPU(t *testing.T) {
	tp := &Transport{Protocol: "kcp"}
	tp.setDefaults("client")
	want := runtime.NumCPU()
	if want < 2 {
		want = 2
	}
	if want > 16 {
		want = 16
	}
	if tp.Conn != want {
		t.Fatalf("expected conn default %d (NumCPU), got %d", want, tp.Conn)
	}
}
