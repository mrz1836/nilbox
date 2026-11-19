package devtools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/store"
)

const (
	// maxLineBytes is the longest line of an answer a client reads: room for a picture of
	// a large window.
	maxLineBytes = 256 << 20

	// maxCellRunes is how much of a value a table shows; json=true shows it whole.
	maxCellRunes = 160
)

// ErrNotRunning is returned when nothing answers on the inspector's socket.
var ErrNotRunning = errors.New("no build from source is running with the inspector on these data (start " +
	brand.DevName + " with magex app:run or magex app:dev, or pass data= for a sandbox)")

// ErrInspector is returned, with the inspector's words, for an answer that ended in an
// error.
var ErrInspector = errors.New("the inspector refused")

// ErrCutShort is returned when an answer stops before its end.
var ErrCutShort = errors.New("the app stopped answering")

// Ask sends req to the inspector listening at socket and hands each line of its answer
// to each, until the answer ends, ctx ends (which ends a follow without an error), or
// each fails.
func Ask(ctx context.Context, socket string, req Request, each func(Line) error) error {
	var dialer net.Dialer

	conn, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNotRunning, err)
	}

	defer func() { _ = conn.Close() }()

	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if err = json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("send the request: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLineBytes)

	for scanner.Scan() {
		done, lineErr := handleLine(scanner.Bytes(), each)
		if done || lineErr != nil {
			return lineErr
		}
	}

	if ctx.Err() != nil {
		return nil
	}

	return cutShort(scanner.Err())
}

// cutShort is why an answer ended before its end line, given the error reading it
// stopped with (nil at the connection's end): ErrCutShort when the app went away, which
// reads as the end on one system and as the connection reset on another (Linux resets
// it when the app closes it before reading all the client sent), or else the error.
func cutShort(err error) error {
	switch {
	case err == nil:
		return ErrCutShort
	case errors.Is(err, syscall.ECONNRESET):
		return fmt.Errorf("%w: %w", ErrCutShort, err)
	default:
		return fmt.Errorf("read the answer: %w", err)
	}
}

// handleLine decodes one line of an answer and hands it to each, reporting whether the
// answer ended with it.
func handleLine(data []byte, each func(Line) error) (bool, error) {
	var line Line
	if err := json.Unmarshal(data, &line); err != nil {
		return true, fmt.Errorf("read the answer: %w", err)
	}

	switch line.Type {
	case LineEnd:
		return true, nil
	case LineError:
		var why string

		_ = json.Unmarshal(line.Value, &why)

		return true, fmt.Errorf("%w: %s", ErrInspector, why)
	default:
		return false, each(line)
	}
}

// Render writes line to w as a terminal shows it: a feed's entry on one line, stamped
// with its local time; a query's rows as a table; text as it is; any other value as
// indented JSON. A picture is the client's to save; Render only says its size.
func Render(w io.Writer, line Line) error {
	text, err := renderText(line)
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, text)

	return err
}

// renderText is line as Render writes it.
func renderText(line Line) (string, error) {
	switch line.Type {
	case LineLog:
		return renderEntry(line.Value, func(e LogEntry) string {
			return fmt.Sprintf("%s %-5s %s%s", clockText(e.Time), e.Level, e.Message, attrsText(e.Attrs))
		})
	case LineEvent:
		return renderEntry(line.Value, func(e EventEntry) string {
			return fmt.Sprintf("%s EVENT %s %s", clockText(e.Time), e.Name, e.Data)
		})
	case LineConsole:
		return renderEntry(line.Value, func(e ConsoleEntry) string {
			return fmt.Sprintf("%s PAGE  %-5s %s", clockText(e.Time), strings.ToUpper(e.Level), e.Text)
		})
	case LineTrail:
		return renderEntry(line.Value, func(e TrailEntry) string {
			return strings.TrimSuffix(fmt.Sprintf("%s USER  %-6s %s · %s", clockText(e.Time), e.Kind, e.Target, e.Where), " · ")
		})
	default:
		return renderValue(line)
	}
}

// renderValue is a line that holds a value, not a feed's entry, as Render writes it.
func renderValue(line Line) (string, error) {
	switch line.Type {
	case LineTable:
		var result store.QueryResult
		if err := json.Unmarshal(line.Value, &result); err != nil {
			return "", fmt.Errorf("read the rows: %w", err)
		}

		return tableText(result), nil
	case LineText:
		var text string
		if err := json.Unmarshal(line.Value, &text); err != nil {
			return "", fmt.Errorf("read the text: %w", err)
		}

		return text, nil
	case LineShot:
		var shot Shot
		if err := json.Unmarshal(line.Value, &shot); err != nil {
			return "", fmt.Errorf("read the picture: %w", err)
		}

		return "A picture of " + strconv.Itoa(len(shot.PNG)) + " bytes.\n", nil
	default:
		var indented bytes.Buffer
		if err := json.Indent(&indented, line.Value, "", "  "); err != nil {
			return string(line.Value) + "\n", nil //nolint:nilerr // a value that is not JSON prints as it came
		}

		return indented.String() + "\n", nil
	}
}

// renderEntry decodes a feed's entry and formats it on one line.
func renderEntry[T any](value json.RawMessage, format func(T) string) (string, error) {
	var e T
	if err := json.Unmarshal(value, &e); err != nil {
		return "", fmt.Errorf("read the entry: %w", err)
	}

	return format(e) + "\n", nil
}

// tableText is a query's result as aligned columns, with a note when rows were left out.
func tableText(result store.QueryResult) string {
	var b strings.Builder

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join(result.Columns, "\t"))

	for _, row := range result.Rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = cellText(cell)
		}

		_, _ = fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}

	_ = tw.Flush()

	count := strconv.Itoa(len(result.Rows)) + " rows"
	if len(result.Rows) == 1 {
		count = "1 row"
	}

	if result.Truncated {
		count += "; more left out (max= shows more)"
	}

	return b.String() + "(" + count + ")\n"
}

// cellText is a value as a table shows it: on one line, cut short after maxCellRunes
// with its whole length said.
func cellText(cell string) string {
	cell = strings.ReplaceAll(cell, "\n", "⏎")

	runes := []rune(cell)
	if len(runes) <= maxCellRunes {
		return cell
	}

	return string(runes[:maxCellRunes]) + "… (" + strconv.Itoa(len(runes)) + " chars)"
}

// clockText is t as a clock shows it, in local time, to the millisecond.
func clockText(t time.Time) string {
	return t.Local().Format("15:04:05.000") //nolint:gosmopolitan // the terminal shows the user's own clock
}
