package buffer

import (
	"errors"
	"sync"
)

type RingBuffer[T any] struct {
	data   []T
	head   int
	tail   int
	size   int
	isFull bool
	mu     sync.Mutex
}

func NewRingBuffer[T any](size int) *RingBuffer[T] {
	return &RingBuffer[T]{
		data: make([]T, size),
		size: size,
	}
}

func (r *RingBuffer[T]) Write(v T) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.data[r.head] = v

	if r.isFull {
		r.tail = (r.tail + 1) % r.size
	}

	r.head = (r.head + 1) % r.size

	r.isFull = r.head == r.tail
}

func (r *RingBuffer[T]) Read() (T, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.isFull && r.head == r.tail {
		var zero T
		return zero, errors.New("buffer is empty")
	}

	v := r.data[r.tail]
	r.isFull = false
	r.tail = (r.tail + 1) % r.size

	return v, nil
}

func (r *RingBuffer[T]) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isFull {
		return r.size
	}
	if r.head >= r.tail {
		return r.head - r.tail
	}
	return r.size + (r.head - r.tail)
}

func (r *RingBuffer[T]) len() int {
	if r.isFull {
		return r.size
	}
	if r.head >= r.tail {
		return r.head - r.tail
	}
	return r.size + (r.head - r.tail)
}

func (r *RingBuffer[T]) GetAll() []T {
	r.mu.Lock()
	defer r.mu.Unlock()

	size := r.len()
	result := make([]T, size)

	if size == 0 {
		return result
	}

	if !r.isFull && r.head > r.tail {
		copy(result, r.data[r.tail:r.head])
	} else {
		n := copy(result, r.data[r.tail:])
		copy(result[n:], r.data[:r.head])
	}
	return result
}
