package buffer

import (
	"sync"
)

var TPool = sync.Pool{
	New: func() any {
		// Keep per-goroutine memory modest for high concurrency (thousands of streams).
		// Larger buffers slightly help throughput, but can OOM 4GB boxes under load.
		b := make([]byte, 64*1024)
		return &b
	},
}

var UPool = sync.Pool{
	New: func() any {
		b := make([]byte, 64*1024)
		return &b
	},
}
