package diag

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
)

// IsBenignStreamErr reports whether an error is expected during normal shutdown,
// connection teardown, or transient kernel buffer pressure.
func IsBenignStreamErr(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	if IsNoBufferOrNoMem(err) {
		return true
	}
	return false
}
