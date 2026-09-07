package flow

import (
	"context"
	"fmt"
	"sync"
)

// Result carries the value a job has produced to the jobs that come after it, so producers that run
// in their own goroutines can hand over their value without sharing a variable.
type Result[T any] struct {
	lock  sync.RWMutex
	value T
	set   bool
}

// Capture runs the function and writes the value it produces to the given destination, which makes
// it possible to fill in the fields of a context while the flow is running.
//
//	Capture(&C.Version, probe)
func Capture[T any](dst *T, fn func(ctx context.Context) (T, error)) Job {
	if dst == nil {
		panic(fmt.Errorf("Captured value requires a destination."))
	}

	return func(ctx context.Context) error {
		value, err := fn(ctx)

		if err != nil {
			return err
		}

		*dst = value

		return nil
	}
}

// CaptureResult runs the function and stores the value it produces inside the given result.
func CaptureResult[T any](r *Result[T], fn func(ctx context.Context) (T, error)) Job {
	if r == nil {
		panic(fmt.Errorf("Captured result requires a destination."))
	}

	return func(ctx context.Context) error {
		value, err := fn(ctx)

		if err != nil {
			return err
		}

		r.write(value)

		return nil
	}
}

// Get returns the captured value, which is the zero value of the type while nothing is captured
// yet.
func (r *Result[T]) Get() T {
	value, _ := r.Ok()

	return value
}

// Ok returns the captured value together with whether it has been captured at all.
func (r *Result[T]) Ok() (T, bool) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return r.value, r.set
}

// Writes the captured value.
func (r *Result[T]) write(value T) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.value = value
	r.set = true
}
