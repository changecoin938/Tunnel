package conf

import "testing"

func TestTransportSetDefaults_ConnDefaultsTo2(t *testing.T) {
	tp := &Transport{Protocol: "kcp"}
	tp.setDefaults("client")
	if tp.Conn != 2 {
		t.Fatalf("expected conn default 2, got %d", tp.Conn)
	}
}
