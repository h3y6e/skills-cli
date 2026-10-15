package skill

import (
	"sync"
)

// parallelMap applies f to each item on a bounded number of goroutines,
// keeping the order of items.
func parallelMap[T, R any](items []T, f func(T) R) []R {
	results := make([]R, len(items))
	sem := make(chan struct{}, 16)

	var wg sync.WaitGroup
	for i, item := range items {
		sem <- struct{}{}

		wg.Go(func() {
			defer func() { <-sem }()

			results[i] = f(item)
		})
	}

	wg.Wait()

	return results
}

// onceMap runs a function once per key, sharing its result with every caller.
type onceMap[K comparable, V any] struct {
	mu sync.Mutex
	m  map[K]func() (V, error)
}

func (o *onceMap[K, V]) do(key K, f func() (V, error)) (V, error) {
	o.mu.Lock()

	if o.m == nil {
		o.m = map[K]func() (V, error){}
	}

	g, ok := o.m[key]
	if !ok {
		g = sync.OnceValues(f)
		o.m[key] = g
	}

	o.mu.Unlock()

	return g()
}

func (o *onceMap[K, V]) doErr(key K, f func() error) error {
	_, err := o.do(key, func() (V, error) {
		var zero V
		return zero, f()
	})

	return err
}
