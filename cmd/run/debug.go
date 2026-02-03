package run

import (
	"net/http"
	_ "net/http/pprof"
	"paqet/internal/flog"
)

func startPprof(addr string) {
	if addr == "" {
		return
	}
	go func() {
		flog.Infof("pprof enabled on http://%s/debug/pprof/ (bind carefully)", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			flog.Errorf("pprof server failed: %v", err)
		}
	}()
}


