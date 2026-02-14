package diag

import (
	"context"
	"io"
)

// BidiCopy runs two copy loops (usually up/down) and ensures both goroutines exit
// before returning by closing both sides once either direction finishes or ctx is done.
//
// It returns the two copy errors (one per direction). Callers should typically
// treat benign close/shutdown errors as nil via IsBenignStreamErr.
func BidiCopy(ctx context.Context, a io.Closer, b io.Closer, f1 func() error, f2 func() error) (error, error) {
	errCh := make(chan error, 2)
	go func() { errCh <- f1() }()
	go func() { errCh <- f2() }()

	// Wait for first completion OR shutdown request, then close both sides to
	// unblock the other copy goroutine.
	gotFirst := false
	var firstErr error
	select {
	case <-ctx.Done():
	case firstErr = <-errCh:
		gotFirst = true
	}

	if a != nil {
		_ = a.Close()
	}
	if b != nil {
		_ = b.Close()
	}

	// Drain both copy results. Channel buffer size ensures neither sender blocks.
	if gotFirst {
		secondErr := <-errCh
		return firstErr, secondErr
	}
	err1 := <-errCh
	err2 := <-errCh
	return err1, err2
}
