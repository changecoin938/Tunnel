package conf

import "testing"

func TestTransportSetDefaults_ConnDefaultsTo4(t *testing.T) {
	tp := &Transport{Protocol: "kcp"}
	tp.setDefaults("client")
	if tp.Conn != 4 {
		t.Fatalf("expected conn default 4, got %d", tp.Conn)
	}
}
