package diag

import (
	"errors"
	"io"
	"paqet/internal/pkg/buffer"
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
	var written int64
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := writeFullWithRetry(dst, buf[:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				return written, ew
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
		}
		if er != nil {
			if errors.Is(er, io.EOF) {
				return written, nil
			}
			return written, er
		}
	}
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
		if errors.Is(err, syscall.ENOBUFS) || errors.Is(err, syscall.ENOMEM) {
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
