package conf

import "testing"

func TestTransportSetDefaults_ConnDefaultsTo10(t *testing.T) {
	tp := &Transport{Protocol: "kcp"}
	tp.setDefaults("client")
	if tp.Conn != 10 {
		t.Fatalf("expected conn default 10, got %d", tp.Conn)
	}
}
