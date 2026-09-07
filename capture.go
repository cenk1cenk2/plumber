package plumber

import (
	"context"
	"fmt"
)

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
