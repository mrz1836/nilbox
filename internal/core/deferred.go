package core

import "context"

// Deferred is the work kept off the app's cold start: steps run once, in order, after the
// window's first paint. Reading the login shell's environment comes first, since what
// follows runs tools on the PATH it finds.
type Deferred struct {
	steps []func(ctx context.Context)
}

// Run runs each step in order and returns when they are done, or when ctx ends between
// steps. Each step logs its own outcome.
func (d *Deferred) Run(ctx context.Context) {
	for _, step := range d.steps {
		if ctx.Err() != nil {
			return
		}

		step(ctx)
	}
}
