package diag

import (
	"errors"
	"io"
	"paqet/internal/pkg/buffer"
	"strings"
	"sync"
	"syscall"
	"time"
)

func CopyTCPUp(dst io.Writer, src io.Reader) error {
	n, err := copyWithWriteRetry(dst, src, &buffer.TPool)
	AddTCPUp(n)
	return err
}

func CopyTCPDown(dst io.Writer, src io.Reader) error {
	n, err := copyWithWriteRetry(dst, src, &buffer.TPool)
	AddTCPDown(n)
	return err
}

func CopyUDPUp(dst io.Writer, src io.Reader) error {
	n, err := copyWithWriteRetry(dst, src, &buffer.UPool)
	AddUDPUp(n)
	return err
}

func CopyUDPDown(dst io.Writer, src io.Reader) error {
	n, err := copyWithWriteRetry(dst, src, &buffer.UPool)
	AddUDPDown(n)
	return err
}

func copyWithWriteRetry(dst io.Writer, src io.Reader, pool *sync.Pool) (int64, error) {
	// Preserve fast-path optimizations (splice/sendfile) where possible.
	type readerFrom interface {
		ReadFrom(io.Reader) (int64, error)
	}
	if rf, ok := dst.(readerFrom); ok {
		return readFromWithRetry(rf, src)
	}

	type writerTo interface {
		WriteTo(io.Writer) (int64, error)
	}
	if wt, ok := src.(writerTo); ok {
		return writeToWithRetry(wt, &retryWriter{w: dst})
	}

	if pool == nil {
		return io.Copy(&retryWriter{w: dst}, src)
	}

	bufp := pool.Get().(*[]byte)
	defer pool.Put(bufp)
	buf := *bufp
	return io.CopyBuffer(&retryWriter{w: dst}, src, buf)
}

type retryWriter struct{ w io.Writer }

func (w *retryWriter) Write(p []byte) (int, error) {
	return writeFullWithRetry(w.w, p)
}

func readFromWithRetry(dst interface {
	ReadFrom(io.Reader) (int64, error)
}, src io.Reader) (int64, error) {
	const (
		burstBudget   = 500 * time.Millisecond // phase 1: fast exponential backoff
		maxBackoff    = 20 * time.Millisecond
		sustainedWait = 100 * time.Millisecond // phase 2: steady retry — never give up
		maxTotalWait  = 30 * time.Second       // upper bound per ENOBUFS/ENOMEM episode
	)
	backoff := 200 * time.Microsecond
	var totalSlept time.Duration

	var written int64
	for {
		n, err := dst.ReadFrom(src)
		if n > 0 {
			written += n
			// Data flowed — reset to burst mode.
			backoff = 200 * time.Microsecond
			totalSlept = 0
		}
		if err == nil || errors.Is(err, io.EOF) {
			return written, nil
		}
		if !isTransientBackpressure(err) {
			return written, err
		}

		// ENOBUFS/ENOMEM: transient kernel buffer pressure. Retry, but don't keep
		// goroutines stuck forever under sustained memory pressure.
		// Phase 1 (burst): fast exponential backoff up to burstBudget.
		// Phase 2 (sustained): steady 100ms waits.
		if IsNoBufferOrNoMem(err) {
			AddEnobufsRetry()
		}
		if totalSlept >= maxTotalWait {
			return written, err
		}
		if totalSlept >= burstBudget {
			if IsNoBufferOrNoMem(err) {
				AddEnobufsSustained()
			}
			time.Sleep(sustainedWait)
			totalSlept += sustainedWait
			continue
		}
		time.Sleep(backoff)
		totalSlept += backoff
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func writeToWithRetry(src interface {
	WriteTo(io.Writer) (int64, error)
}, dst io.Writer) (int64, error) {
	const (
		burstBudget   = 500 * time.Millisecond
		maxBackoff    = 20 * time.Millisecond
		sustainedWait = 100 * time.Millisecond
		maxTotalWait  = 30 * time.Second
	)
	backoff := 200 * time.Microsecond
	var totalSlept time.Duration

	var written int64
	for {
		n, err := src.WriteTo(dst)
		if n > 0 {
			written += n
			backoff = 200 * time.Microsecond
			totalSlept = 0
		}
		if err == nil || errors.Is(err, io.EOF) {
			return written, nil
		}
		if !isTransientBackpressure(err) {
			return written, err
		}

		if IsNoBufferOrNoMem(err) {
			AddEnobufsRetry()
		}
		if totalSlept >= maxTotalWait {
			return written, err
		}
		if totalSlept >= burstBudget {
			if IsNoBufferOrNoMem(err) {
				AddEnobufsSustained()
			}
			time.Sleep(sustainedWait)
			totalSlept += sustainedWait
			continue
		}
		time.Sleep(backoff)
		totalSlept += backoff
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// IsNoBufferOrNoMem reports whether err is ENOBUFS, ENOMEM, or the equivalent
// libpcap string error. Exported so TCP handlers can use it as a safety net.
func IsNoBufferOrNoMem(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.ENOBUFS) ||
		errors.Is(err, syscall.ENOMEM) ||
		strings.Contains(err.Error(), "No buffer space available") ||
		strings.Contains(err.Error(), "Cannot allocate memory")
}

func isTransientBackpressure(err error) bool {
	if err == nil {
		return false
	}
	// Under heavy load, TCPConn's splice/sendfile paths can surface EAGAIN/EWOULDBLOCK.
	// Treat it like temporary kernel backpressure: retry with a short backoff instead
	// of tearing down the stream (speedtest bursts commonly hit this).
	return IsNoBufferOrNoMem(err) ||
		errors.Is(err, syscall.EAGAIN) ||
		errors.Is(err, syscall.EWOULDBLOCK) ||
		strings.Contains(err.Error(), "Resource temporarily unavailable")
}

func writeFullWithRetry(dst io.Writer, p []byte) (int, error) {
	const (
		burstBudget   = 500 * time.Millisecond
		maxBackoff    = 20 * time.Millisecond
		sustainedWait = 100 * time.Millisecond
		maxTotalWait  = 30 * time.Second
	)
	backoff := 200 * time.Microsecond
	var totalSlept time.Duration

	written := 0
	for len(p) > 0 {
		n, err := dst.Write(p)
		if n > 0 {
			written += n
			p = p[n:]
			backoff = 200 * time.Microsecond
			totalSlept = 0
		}
		if err == nil {
			if n == 0 {
				return written, io.ErrShortWrite
			}
			continue
		}

		// ENOBUFS/ENOMEM: never give up — keep the stream alive.
		if isTransientBackpressure(err) {
			if IsNoBufferOrNoMem(err) {
				AddEnobufsRetry()
			}
			if totalSlept >= maxTotalWait {
				return written, err
			}
			if totalSlept >= burstBudget {
				if IsNoBufferOrNoMem(err) {
					AddEnobufsSustained()
				}
				time.Sleep(sustainedWait)
				totalSlept += sustainedWait
				continue
			}
			time.Sleep(backoff)
			totalSlept += backoff
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}
		return written, err
	}
	return written, nil
}
