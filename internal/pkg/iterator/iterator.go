package iterator

import "sync/atomic"

type Iterator[T any] struct {
	Items []T
	index atomic.Uint64
}

func (it *Iterator[T]) Next() (T, bool) {
	n := uint64(len(it.Items))
	if n == 0 {
		var zero T
		return zero, false
	}
	i := it.index.Add(1)
	if n&(n-1) == 0 {
		return it.Items[i&(n-1)], true
	}
	return it.Items[i%n], true
}

func (it *Iterator[T]) Peek() (T, bool) {
	n := uint64(len(it.Items))
	if n == 0 {
		var zero T
		return zero, false
	}
	i := it.index.Load()
	return it.Items[i%n], true
}
