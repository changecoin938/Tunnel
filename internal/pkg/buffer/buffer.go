package buffer

import (
	"sync"
)

var TPool = sync.Pool{
	New: func() any {
		// 32KB for TCP balances throughput against memory on 4GB boxes.
		// 500 streams × 2 directions × 32KB = 32MB worst-case.
		b := make([]byte, 32*1024)
		return &b
	},
}

var UPool = sync.Pool{
	New: func() any {
		// 16KB for UDP — typical UDP datagrams are 1400 bytes (MTU).
		// 128KB wastes 90%+ of each buffer. 16KB holds ~10 datagrams
		// and keeps memory low: 500 streams × 2 × 16KB = 16MB.
		b := make([]byte, 16*1024)
		return &b
	},
}
