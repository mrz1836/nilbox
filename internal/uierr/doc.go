// Package uierr keeps what the user is told about a failure apart from the error the logs
// record.
//
// An error carries its full chain of context, which is what a log line or a bug report
// needs. The window needs something else: one plain sentence the user can act on. New
// attaches that sentence to an error without losing the chain, and Message reads it back
// wherever an error turns into text the window shows: a bound method's error, a stored
// refresh failure, a field's problem. An error with no attached sentence reads as its own
// message, made into a sentence.
package uierr
