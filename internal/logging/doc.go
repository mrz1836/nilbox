// Package logging builds the app's one logger.
//
// The desktop bootstrap calls New once and hands the logger to everything that logs; no
// package logs through slog's default logger. Each record goes to two places:
//
//   - stderr, as text, for whoever runs the binary from a terminal;
//   - FileName in the data directory's logs folder, as JSON, for a bug report. The file is
//     capped at MaxFileBytes: when the next record would pass the cap it is renamed to
//     FileName.1 (and FileName.1 to FileName.2), so at most KeptFiles files and about
//     15 MB are kept. The files are readable only by the user.
//
// A log file that cannot be opened, such as one in a folder the user cannot write, never
// stops the app: the logger writes to stderr alone and says why in its first record. With
// no data directory at all, it writes to stderr alone without a word.
//
// Records at Info and above are kept. DebugFlag on the command line, or DebugEnv set to a
// true value, keeps Debug records too.
package logging
