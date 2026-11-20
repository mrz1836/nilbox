// Package devtools is the inspector a build from source carries: the bundle named
// brand.DevName, from magex app:build or app:run, and magex app:dev. A release, or a
// rehearsal of one, never does: only files built with
// the devtools tag import it, and only a build from source passes that tag
// (internal/repoguard and the magefiles' tests keep both so).
//
// While the app runs, the inspector listens on a Unix socket in its data directory,
// readable by the user alone, and answers one request per connection, one JSON line in
// and JSON lines out (Request, Line). It keeps what the app has been doing in feeds that
// a client reads back or follows live: the log, every record from Debug up whatever the
// log file keeps (LogHandler); the events the services emit (Tee); and, from an agent it
// runs in the window's page on every load (Page), the page's console, the trail of what
// the user did there, clicks and keys, so a bug can be followed without the steps being
// told, and the notes the user leaves on elements they point at (⌥⌘P), each with what
// the element is and a picture of the window (Note), which it keeps in its folder in the
// data directory (InspectorDir), with whether the user lets pointing start, a choice the
// window's Settings show in a build from source alone. It also reads the database
// (read-only, through internal/store), runs JavaScript in the page and returns what it
// gives back, describes the page's elements and the inbox's cards as the web view lays
// them out, takes a picture of the window as the web view draws it, and dumps the
// goroutines.
//
// The magex inspect:* targets are its client (Ask and Render), so the user, or an agent
// helping them, can look inside a running build from the terminal.
package devtools
