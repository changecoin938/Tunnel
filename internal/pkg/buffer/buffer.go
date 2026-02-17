package buffer

import (
	"sync"
)

var TPool = sync.Pool{
	New: func() any {
		// 128KB balances throughput (fewer read/write cycles) against memory on
		// 4GB boxes. At 500 concurrent TCP streams × 2 directions = 1000 buffers
		// = 128MB worst-case, which fits comfortably.
		b := make([]byte, 128*1024)
		return &b
	},
}

var UPool = sync.Pool{
	New: func() any {
		b := make([]byte, 128*1024)
		return &b
	},
}
