package diag

import (
	"context"
	"fmt"
	"net"
	"time"
)

type bidiCopyResult struct {
	idx int
	err error
}

// BidiCopy runs two copy loops (usually up/down) and ensures both goroutines exit
// before returning by cancelling both sides (via SetDeadline(time.Now())) once either
// direction finishes or ctx is done.
//
// It returns the two copy errors (one per direction). Callers should typically
// treat benign close/shutdown errors as nil via IsBenignStreamErr.
func BidiCopy(ctx context.Context, a net.Conn, b net.Conn, f1 func() error, f2 func() error) (error, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	errCh := make(chan bidiCopyResult, 2)
	go func() { errCh <- bidiCopyResult{idx: 0, err: f1()} }()
	go func() { errCh <- bidiCopyResult{idx: 1, err: f2()} }()

	var errs [2]error
	got := 0

	// Wait for first completion OR shutdown request, then cancel both sides to
	// unblock the other copy goroutine. Cancellation uses deadlines so the caller
	// can retain ownership of Close().
	select {
	case <-ctx.Done():
	case res := <-errCh:
		errs[res.idx] = res.err
		got = 1
	}

	cancelAt := time.Now()
	if a != nil {
		_ = a.SetDeadline(cancelAt)
	}
	if b != nil {
		_ = b.SetDeadline(cancelAt)
	}

	// Use 5s force-close timeout (must be shorter than the server's 10s shutdown timer
	// to avoid orphaned goroutines when the server exits).
	shutdownTimer := time.NewTimer(5 * time.Second)
	defer shutdownTimer.Stop()

	for got < 2 {
		if !shutdownTimer.Stop() {
			select {
			case <-shutdownTimer.C:
			default:
			}
		}
		shutdownTimer.Reset(5 * time.Second)

		select {
		case res := <-errCh:
			errs[res.idx] = res.err
			got++
		case <-shutdownTimer.C:
			// Force-close both connections to guarantee stuck goroutines unblock.
			// SetDeadline may not be sufficient if io.Copy is stuck in a kernel splice path.
			if a != nil {
				_ = a.Close()
			}
			if b != nil {
				_ = b.Close()
			}
			if errs[0] == nil {
				errs[0] = fmt.Errorf("bidi copy timeout waiting for goroutine shutdown")
			}
			if errs[1] == nil {
				errs[1] = fmt.Errorf("bidi copy timeout waiting for goroutine shutdown")
			}
			return errs[0], errs[1]
		}
	}

	return errs[0], errs[1]
}
