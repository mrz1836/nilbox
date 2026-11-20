package mcpserver

import (
	"errors"
	"io"
	"time"
)

// MaxLine is the most one message may be, for the tests of the line it can't read.
const MaxLine = maxLine

// errNoCwd is a working folder the system can't say.
var errNoCwd = errors.New("the working folder can't be read")

// Host is what a test hands the tool mode in place of its process's: the moment it reads
// as now, the randomness of a proposal's ID, and the folder it runs in, "" for one the
// system can't say.
type Host struct {
	Now    time.Time
	Random io.Reader
	Cwd    string
}

// ServeOn is Serve on the host h.
func ServeOn(args []string, stdin io.Reader, stdout, stderr io.Writer, h Host) (int, bool) {
	return serve(args, stdin, stdout, stderr, host{
		now:    func() time.Time { return h.Now },
		random: h.Random,
		cwd: func() (string, error) {
			if h.Cwd == "" {
				return "", errNoCwd
			}

			return h.Cwd, nil
		},
	})
}
