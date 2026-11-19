package devtools

import (
	"context"
	"slices"
	"time"
)

// timed is an entry of a feed, which knows when it happened.
type timed interface {
	when() time.Time
}

// entryLine is one entry of a feed on its way out, with its line's type.
type entryLine struct {
	typ   string
	value any
	at    time.Time
}

// source is one feed as a command streams it, its entries filtered by the command's
// arguments: the last n it keeps, and a way to follow the new ones.
type source struct {
	last   func(n int) []entryLine
	follow func(ctx context.Context, into chan<- entryLine)
}

// sourceOf is feed as a source of lines of type typ, keeping the entries keep lets
// through.
func sourceOf[T timed](typ string, feed *Feed[T], keep func(T) bool) source {
	return source{
		last: func(n int) []entryLine { return lastLines(typ, feed, keep, n) },
		follow: func(ctx context.Context, into chan<- entryLine) {
			followLines(ctx, typ, feed, keep, into)
		},
	}
}

// lineOf is e on its way out as a line of type typ.
func lineOf[T timed](typ string, e T) entryLine {
	return entryLine{typ: typ, value: e, at: e.when()}
}

// lastLines are the last n entries of feed that keep lets through, as lines of type typ.
func lastLines[T timed](typ string, feed *Feed[T], keep func(T) bool, n int) []entryLine {
	kept := slices.DeleteFunc(feed.Last(0), func(e T) bool { return !keep(e) })
	if n >= 0 && len(kept) > n {
		kept = kept[len(kept)-n:]
	}

	lines := make([]entryLine, 0, len(kept))
	for _, e := range kept {
		lines = append(lines, lineOf(typ, e))
	}

	return lines
}

// followLines hands into each new entry of feed that keep lets through, as a line of type
// typ, until ctx ends. It starts following before it returns, so an entry added after it
// is never missed.
func followLines[T timed](ctx context.Context, typ string, feed *Feed[T], keep func(T) bool, into chan<- entryLine) {
	entries, stop := feed.Follow()

	go func() {
		defer stop()

		for {
			select {
			case <-ctx.Done():
				return
			case e := <-entries:
				if keep(e) {
					forward(ctx, into, lineOf(typ, e))
				}
			}
		}
	}()
}

// forward hands line on, unless ctx ends first.
func forward(ctx context.Context, into chan<- entryLine, line entryLine) {
	select {
	case into <- line:
	case <-ctx.Done():
	}
}

// stream sends the last n entries of the sources, in time order, then, with follow,
// each new one as it comes, until ctx ends or the client stops reading.
func stream(ctx context.Context, out *writer, n int, follow bool, sources ...source) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Following starts before the kept entries are read, so none added between the two
	// is missed.
	live := make(chan entryLine, followerBuffer)

	if follow {
		for _, src := range sources {
			src.follow(ctx, live)
		}
	}

	if err := sendKept(out, n, sources); err != nil || !follow {
		return err
	}

	return sendLive(ctx, out, live)
}

// sendKept sends the last n entries of the sources, in the order they happened.
func sendKept(out *writer, n int, sources []source) error {
	parts := make([][]entryLine, 0, len(sources))
	for _, src := range sources {
		parts = append(parts, src.last(n))
	}

	kept := slices.Concat(parts...)
	slices.SortStableFunc(kept, func(a, b entryLine) int { return a.at.Compare(b.at) })

	for _, line := range kept {
		if err := out.send(line.typ, line.value); err != nil {
			return err
		}
	}

	return nil
}

// sendLive sends each line live hands it until ctx ends or the client stops reading.
func sendLive(ctx context.Context, out *writer, live <-chan entryLine) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case line := <-live:
			if err := out.send(line.typ, line.value); err != nil {
				return err
			}
		}
	}
}
