package socks

import (
	"context"
	"paqet/internal/client"
	"sync"
)

const socksReplyBufCap = 4 + 1 + 255 + 2 // header + addr + port (max domain length 255)

var rPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, socksReplyBufCap)
		return &b
	},
}

type Handler struct {
	client *client.Client
	ctx    context.Context
}
