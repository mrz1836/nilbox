// Package clock tells the services the time and writes times the one way the app does.
//
// Services take a Clock so tests can set the time; System is the real one. Times the UI
// or the database reads are written with Format, in UTC as RFC 3339, and times in file
// names with Stamp, which sorts and has no characters a file name cannot hold.
package clock
