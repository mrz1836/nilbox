// Package flight runs one call per key at a time and hands its result to every caller
// that asked while it ran, the way golang.org/x/sync/singleflight does, with each
// caller's context kept:
//
//   - A call runs with the context of the caller that started it.
//   - A caller stops waiting as soon as its own context ends; the call goes on for the
//     others.
//   - When a call ends because the context of the caller that started it ended, a caller
//     still waiting does not take that as its answer: it starts the call again, under
//     its own context.
//   - A call that panics makes every caller waiting for it panic, each on its own
//     goroutine, with a *PanicError, so whatever recovers a caller's panic recovers this
//     one. The process is never brought down from a goroutine no caller owns.
package flight

import (
	"context"
	"fmt"
	"runtime/debug"

	"golang.org/x/sync/singleflight"
)

// Group runs calls that return a T, one per key at a time. The zero Group is ready to
// use, and a Group must not be copied after first use.
type Group[T any] struct {
	calls singleflight.Group
}

// outcome is what one call returned, and whether it failed because the context it ran
// with ended, so its result answers only the caller that started it.
type outcome[T any] struct {
	value     T
	err       error
	abandoned bool
	panicked  *PanicError
}

// PanicError is what a caller panics with when the call it waited for panicked: the
// call's panic value and the stack where it happened.
type PanicError struct {
	Value any
	Stack []byte
}

// Error describes the panic with its stack.
func (p *PanicError) Error() string {
	return fmt.Sprintf("%v\n\n%s", p.Value, p.Stack)
}

// Unwrap returns the panic value when it is an error.
func (p *PanicError) Unwrap() error {
	err, _ := p.Value.(error)

	return err
}

// Do runs fn under key with ctx, or waits for the call under key already running, and
// returns its result. It returns ctx's error as soon as ctx ends, whether or not the call
// has.
func (g *Group[T]) Do(ctx context.Context, key string, fn func(context.Context) (T, error)) (T, error) {
	for {
		if err := ctx.Err(); err != nil {
			var zero T

			return zero, err
		}

		results := g.calls.DoChan(key, func() (any, error) {
			return run(ctx, fn), nil
		})

		select {
		case <-ctx.Done():
			var zero T

			return zero, ctx.Err()
		case r := <-results:
			got, _ := r.Val.(outcome[T]) // run always returns an outcome

			switch {
			case got.panicked != nil:
				panic(got.panicked)
			case got.abandoned && ctx.Err() == nil:
				continue // the caller that started it left; start it again
			default:
				return got.value, got.err
			}
		}
	}
}

// run calls fn, catching a panic so the call's goroutine never brings the process down.
func run[T any](ctx context.Context, fn func(context.Context) (T, error)) (got outcome[T]) {
	defer func() {
		if v := recover(); v != nil {
			got = outcome[T]{panicked: &PanicError{Value: v, Stack: debug.Stack()}}
		}
	}()

	value, err := fn(ctx)

	return outcome[T]{value: value, err: err, abandoned: err != nil && ctx.Err() != nil}
}
