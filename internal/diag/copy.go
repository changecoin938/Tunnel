package diag

import (
	"io"
	"paqet/internal/pkg/buffer"
)

func CopyTCPUp(dst io.Writer, src io.Reader) error {
	bufp := buffer.TPool.Get().(*[]byte)
	defer buffer.TPool.Put(bufp)
	buf := *bufp

	n, err := io.CopyBuffer(dst, src, buf)
	AddTCPUp(n)
	return err
}

func CopyTCPDown(dst io.Writer, src io.Reader) error {
	bufp := buffer.TPool.Get().(*[]byte)
	defer buffer.TPool.Put(bufp)
	buf := *bufp

	n, err := io.CopyBuffer(dst, src, buf)
	AddTCPDown(n)
	return err
}

func CopyUDPUp(dst io.Writer, src io.Reader) error {
	bufp := buffer.UPool.Get().(*[]byte)
	defer buffer.UPool.Put(bufp)
	buf := *bufp

	n, err := io.CopyBuffer(dst, src, buf)
	AddUDPUp(n)
	return err
}

func CopyUDPDown(dst io.Writer, src io.Reader) error {
	bufp := buffer.UPool.Get().(*[]byte)
	defer buffer.UPool.Put(bufp)
	buf := *bufp

	n, err := io.CopyBuffer(dst, src, buf)
	AddUDPDown(n)
	return err
}
