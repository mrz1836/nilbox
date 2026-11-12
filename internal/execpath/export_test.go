package execpath

import "time"

// WithWaitDelay returns r waiting delay, in runWaitDelay's place, for a descendant still
// holding its program's output once the program has exited, so a test of one need not
// wait as long.
func WithWaitDelay(r Exec, delay time.Duration) Exec {
	r.waitDelay = delay

	return r
}
