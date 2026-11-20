package mcpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// The statuses the tool mode exits with, beside 0 once stdin ends.
const (
	// codeFailed ends a session on an error: stdin couldn't be read, or stdout written.
	codeFailed = 1

	// codeRefused refuses a call the tool mode can't serve (errCall) before it reads
	// anything.
	codeRefused = 2
)

// host is what the tool mode reads of its process beyond its arguments and its streams:
// the time, randomness for a proposal's ID, and the folder it runs in. Serve hands it the
// process's own; a test hands it its own.
type host struct {
	now    func() time.Time
	random io.Reader
	cwd    func() (string, error)
}

// processHost is the process's own host: the system's clock, its cryptographic
// randomness, and its working folder as the system reports it, never as the environment
// says.
func processHost() host {
	return host{now: time.Now, random: rand.Reader, cwd: syscall.Getwd}
}

// Serve runs the tool mode when args ask for it (IsCall), and returns the status the
// process exits with and true; for any other arguments it returns 0 and false and reads
// nothing. It answers the MCP messages on stdin, one JSON-RPC message a line, on stdout,
// each as it comes, and returns 0 once stdin ends. A call it can't serve (a protocol this
// build doesn't know, no describe folder, a conversation that isn't one) is refused with a
// line on stderr and a non-zero status before it reads anything, and an error that ends a
// session (stdin unreadable, stdout gone) is said on stderr too.
func Serve(args []string, stdin io.Reader, stdout, stderr io.Writer) (int, bool) {
	return serve(args, stdin, stdout, stderr, processHost())
}

// serve is Serve on the host h.
func serve(args []string, stdin io.Reader, stdout, stderr io.Writer, h host) (int, bool) {
	if !IsCall(args) {
		return 0, false
	}

	c, err := readCall(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, textutil.Sentence(err.Error()))

		return codeRefused, true
	}

	if err = newServer(c, h).run(context.Background(), stdin, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, textutil.Sentence(err.Error()))

		return codeFailed, true
	}

	return 0, true
}

// server is one session of the tool mode.
type server struct {
	call  call
	host  host
	where kit.Where

	// folder is the folder an outside session runs in, by its base name, which its
	// proposals name (kit.ProposalFile.Where): "" in the app, or when the system can't say.
	folder string
}

// newServer returns the session that serves c on h.
func newServer(c call, h host) *server {
	s := &server{call: c, host: h, where: c.where()}

	if s.where == kit.Outside {
		if dir, err := h.cwd(); err == nil && dir != "" {
			s.folder = kit.Name(filepath.Base(dir))
		}
	}

	return s
}

// run answers each message on stdin as it comes, until stdin ends: a line is read,
// answered, and its answer written before the next is read, so nothing waits for stdin to
// end, which the agent's client never closes: it ends the tool mode with a signal.
func (s *server) run(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	in, out := newLines(stdin), newReplies(stdout)

	for {
		line, err := in.next()

		switch {
		case errors.Is(err, io.EOF):
			return nil
		case errors.Is(err, errTooLong):
			err = out.write(errorOf(nil, codeInvalidRequest, "Invalid request: "+errTooLong.Error()+", the most a "+
				"message may be."))
		case err != nil:
			return fmt.Errorf("read a message: %w", err)
		default:
			err = out.write(s.handle(ctx, line))
		}

		if err != nil {
			return fmt.Errorf("write an answer: %w", err)
		}
	}
}

// handle answers one line: nil for an empty line, a response, and a notification, which
// is never answered and asks nothing of the tool mode (that the session started, or a
// call's progress or its cancellation).
func (s *server) handle(ctx context.Context, line []byte) *reply {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}

	req, refusal := readRequest(line)
	if refusal != nil || req.id == nil {
		return refusal
	}

	return s.dispatch(ctx, req)
}
