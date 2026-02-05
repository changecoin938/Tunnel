package diag

import (
	"encoding/json"
	"net/http"
	"sync"
)

var httpOnce sync.Once

func RegisterHTTP() {
	if !Enabled() {
		return
	}
	httpOnce.Do(func() {
		http.HandleFunc("/debug/paqet/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok\n"))
		})

		http.HandleFunc("/debug/paqet/status", func(w http.ResponseWriter, r *http.Request) {
			st := Snapshot()
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			_ = enc.Encode(st)
		})

		http.HandleFunc("/debug/paqet/text", func(w http.ResponseWriter, r *http.Request) {
			st := Snapshot()
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(FormatText(st)))
		})
	})
}
