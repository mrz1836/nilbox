// Package periodic runs a job now and then on a fixed interval until its context ends,
// the way the inbox refresh and the worktree sweep run.
//
// A run that fails is logged and the next one still happens; a run that panics is
// recovered and logged the same way, so one bad run never stops the loop or the app.
package periodic
