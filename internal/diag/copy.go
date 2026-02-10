package diag

import (
	"errors"
	"io"
	"paqet/internal/pkg/buffer"
	"strings"
	"syscall"
	"time"
)

func CopyTCPUp(dst io.Writer, src io.Reader) error {
	bufp := buffer.TPool.Get().(*[]byte)
	defer buffer.TPool.Put(bufp)
	buf := *bufp

	n, err := copyWithWriteRetry(dst, src, buf)
	AddTCPUp(n)
	return err
}

func CopyTCPDown(dst io.Writer, src io.Reader) error {
	bufp := buffer.TPool.Get().(*[]byte)
	defer buffer.TPool.Put(bufp)
	buf := *bufp

	n, err := copyWithWriteRetry(dst, src, buf)
	AddTCPDown(n)
	return err
}

func CopyUDPUp(dst io.Writer, src io.Reader) error {
	bufp := buffer.UPool.Get().(*[]byte)
	defer buffer.UPool.Put(bufp)
	buf := *bufp

	n, err := copyWithWriteRetry(dst, src, buf)
	AddUDPUp(n)
	return err
}

func CopyUDPDown(dst io.Writer, src io.Reader) error {
	bufp := buffer.UPool.Get().(*[]byte)
	defer buffer.UPool.Put(bufp)
	buf := *bufp

	n, err := copyWithWriteRetry(dst, src, buf)
	AddUDPDown(n)
	return err
}

func copyWithWriteRetry(dst io.Writer, src io.Reader, buf []byte) (int64, error) {
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
		return wt.WriteTo(&retryWriter{w: dst})
	}

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
		maxTotalSleep = 500 * time.Millisecond
		maxBackoff    = 20 * time.Millisecond
	)
	backoff := 200 * time.Microsecond
	var totalSlept time.Duration

	var written int64
	for {
		n, err := dst.ReadFrom(src)
		if n > 0 {
			written += n
			backoff = 200 * time.Microsecond
			totalSlept = 0
		}
		if err == nil || errors.Is(err, io.EOF) {
			return written, nil
		}
		if !isNoBufferOrNoMem(err) {
			return written, err
		}
		if totalSlept >= maxTotalSleep {
			return written, err
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

func isNoBufferOrNoMem(err error) bool {
	return errors.Is(err, syscall.ENOBUFS) ||
		errors.Is(err, syscall.ENOMEM) ||
		strings.Contains(err.Error(), "No buffer space available") ||
		strings.Contains(err.Error(), "Cannot allocate memory")
}

func writeFullWithRetry(dst io.Writer, p []byte) (int, error) {
	const (
		maxTotalSleep = 500 * time.Millisecond
		maxBackoff    = 20 * time.Millisecond
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

		// ENOBUFS/ENOMEM can be transient under kernel network memory pressure.
		// Treat it as retryable with bounded backoff to avoid tearing down streams.
		if isNoBufferOrNoMem(err) {
			if totalSlept >= maxTotalSleep {
				return written, err
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
