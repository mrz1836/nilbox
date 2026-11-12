// Package periodic runs a job now and then, as often as a setting says, until its context
// ends, the way the inbox refresh, the checks follower, and the worktree sweep run.
//
// A run that fails is logged and the next one still happens; a run that panics is
// recovered and logged the same way, so one bad run never stops the loop or the app.
package periodic
