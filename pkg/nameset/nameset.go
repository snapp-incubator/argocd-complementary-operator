package nameset

import "iter"

type Nameset[T comparable] struct {
	data map[T]bool
}

func (nm Nameset[T]) Add(i T) {
	nm.data[i] = true
}

func (nm Nameset[T]) Remove(i T) {
	delete(nm.data, i)
}

func (nm Nameset[T]) Len() int {
	return len(nm.data)
}

func (nm Nameset[T]) Contains(i T) bool {
	_, ok := nm.data[i]

	return ok
}

func (nm Nameset[T]) Filter(filter func(T) bool) iter.Seq[T] {
	return func(yield func(i T) bool) {
		for i := range nm.data {
			if filter(i) && !yield(i) {
				return
			}
		}
	}
}

func (nm Nameset[T]) All() iter.Seq[T] {
	return func(yield func(i T) bool) {
		for i := range nm.data {
			if !yield(i) {
				return
			}
		}
	}
}

func New[T comparable]() Nameset[T] {
	return Nameset[T]{
		data: make(map[T]bool),
	}
}
