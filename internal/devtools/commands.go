package devtools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/store"
)

const (
	// defaultLast is how many kept entries a feed's command shows unless asked for more.
	defaultLast = 100

	// defaultRows is how many rows a query returns unless asked for more, and maxRows the
	// most it returns at all.
	defaultRows = 200
	maxRows     = 10000

	// defaultScriptSeconds is how long a script may run in the page unless asked for
	// longer.
	defaultScriptSeconds = 15

	// shotTimeout is how long a picture of the window may take.
	shotTimeout = 15 * time.Second

	// bytesPerMB turns a byte count into megabytes.
	bytesPerMB = 1 << 20
)

// command is one thing the inspector does: what its arguments are, what it does, and how
// it answers.
type command struct {
	args  string
	about string
	run   func(ctx context.Context, out *writer, a args) error
}

// table is every command, by name.
func (s *Server) table() map[string]command {
	return map[string]command{
		"help":    {about: "lists the commands", run: s.help},
		"status":  {about: "the build, the process, the data's paths, the window, and the page", run: s.status},
		"logs":    {args: "n level grep follow", about: "the app's log, from Debug up", run: s.logs},
		"events":  {args: "n grep follow", about: "the events the services emitted", run: s.events},
		"console": {args: "n level grep follow", about: "the page's console and its uncaught errors", run: s.console},
		"trail":   {args: "n grep follow", about: "what the user clicked, pressed, typed in, and chose", run: s.trail},
		"watch": {
			args: "n level grep", about: "follows the log, the events, the console, and the trail together", run: s.watch,
		},
		"sql":    {args: "q max", about: "runs one statement that reads, against the live database", run: s.query},
		"tables": {about: "the database's tables, with their row counts", run: s.tables},
		"js":     {args: "code timeout", about: "runs JavaScript in the page, nb its helpers, and returns its value", run: s.js},
		"dom": {
			args: "sel styles html limit", about: "describes the elements a selector matches, as the web view lays them out",
			run: s.dom,
		},
		"cards":  {about: "the inbox's cards: where each is, and whether the web view draws it", run: s.cards},
		"click":  {args: "target", about: "clicks a control, by selector or by the name it shows", run: s.click},
		"shot":   {about: "a picture of the window as its web view draws it", run: s.shot},
		"stacks": {args: "debug", about: "every goroutine's stack", run: s.stacks},
		"reload": {about: "reloads the page", run: s.reload},
	}
}

// help lists the commands, by name.
func (s *Server) help(_ context.Context, out *writer, _ args) error {
	infos := make([]CommandInfo, 0, len(s.commands))

	for name, cmd := range s.commands {
		infos = append(infos, CommandInfo{Command: name, Args: cmd.args, About: cmd.about})
	}

	slices.SortFunc(infos, func(a, b CommandInfo) int { return strings.Compare(a.Command, b.Command) })

	return out.send(LineResult, infos)
}

// status answers with the app as About describes it, its Go runtime's numbers, the page,
// and how much each feed keeps.
func (s *Server) status(_ context.Context, out *writer, _ args) error {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	about := s.cfg.About()

	return out.send(LineResult, Status{
		About:      about,
		Uptime:     s.cfg.Now().Sub(about.Started).Round(time.Second).String(),
		Go:         runtime.Version(),
		Goroutines: runtime.NumGoroutine(),
		HeapMB:     megabytes(mem.HeapAlloc),
		SysMB:      megabytes(mem.Sys),
		GCs:        mem.NumGC,
		Page:       s.cfg.Page.Info(),
		Kept: map[string]int{
			LineLog: s.cfg.Logs.Len(), LineEvent: s.cfg.Events.Len(),
			LineConsole: s.cfg.Page.Console().Len(), LineTrail: s.cfg.Page.Trail().Len(),
		},
	})
}

// logs streams the log: its last n records at level and up whose text holds grep, and,
// with follow, each new one as it comes.
func (s *Server) logs(ctx context.Context, out *writer, a args) error {
	return stream(ctx, out, a.number("n", defaultLast), a.flag("follow"), s.logSource(a))
}

// events streams the events, as logs streams the log.
func (s *Server) events(ctx context.Context, out *writer, a args) error {
	return stream(ctx, out, a.number("n", defaultLast), a.flag("follow"), s.eventSource(a))
}

// console streams the page's console, as logs streams the log.
func (s *Server) console(ctx context.Context, out *writer, a args) error {
	return stream(ctx, out, a.number("n", defaultLast), a.flag("follow"), s.consoleSource(a))
}

// trail streams what the user did, as logs streams the log.
func (s *Server) trail(ctx context.Context, out *writer, a args) error {
	return stream(ctx, out, a.number("n", defaultLast), a.flag("follow"), s.trailSource(a))
}

// watch follows the log, the events, the console, and the trail together, in the order
// they come, after the last n of each (none unless asked).
func (s *Server) watch(ctx context.Context, out *writer, a args) error {
	return stream(ctx, out, a.number("n", 0), true,
		s.logSource(a), s.eventSource(a), s.consoleSource(a), s.trailSource(a))
}

// logSource is the log, filtered by level and grep.
func (s *Server) logSource(a args) source {
	level, grep := a.text("level"), strings.ToLower(a.text("grep"))

	return sourceOf(LineLog, s.cfg.Logs, func(e LogEntry) bool {
		return levelAtLeast(e.Level, level) && (grep == "" || contains(grep, e.Message, attrsText(e.Attrs)))
	})
}

// eventSource is the events, filtered by grep.
func (s *Server) eventSource(a args) source {
	grep := strings.ToLower(a.text("grep"))

	return sourceOf(LineEvent, s.cfg.Events, func(e EventEntry) bool {
		return grep == "" || contains(grep, e.Name, string(e.Data))
	})
}

// consoleSource is the page's console, filtered by level and grep.
func (s *Server) consoleSource(a args) source {
	level, grep := a.text("level"), strings.ToLower(a.text("grep"))

	return sourceOf(LineConsole, s.cfg.Page.Console(), func(e ConsoleEntry) bool {
		return levelAtLeast(e.Level, level) && (grep == "" || contains(grep, e.Text))
	})
}

// trailSource is the user's trail, filtered by grep.
func (s *Server) trailSource(a args) source {
	grep := strings.ToLower(a.text("grep"))

	return sourceOf(LineTrail, s.cfg.Page.Trail(), func(e TrailEntry) bool {
		return grep == "" || contains(grep, e.Kind, e.Target, e.Where)
	})
}

// query runs the statement q against the database, returning at most max rows.
func (s *Server) query(ctx context.Context, out *writer, a args) error {
	q, err := a.need("q")
	if err != nil {
		return err
	}

	result, err := s.cfg.Database.Query(ctx, q, min(a.number("max", defaultRows), maxRows))
	if err != nil {
		return err
	}

	return out.send(LineTable, result)
}

// tables lists the database's tables and their row counts, as a table.
func (s *Server) tables(ctx context.Context, out *writer, _ args) error {
	tables, err := s.cfg.Database.Tables(ctx)
	if err != nil {
		return err
	}

	result := store.QueryResult{Columns: []string{"table", "rows"}, Rows: make([][]string, 0, len(tables))}
	for _, table := range tables {
		result.Rows = append(result.Rows, []string{table.Name, strconv.FormatInt(table.Rows, 10)})
	}

	return out.send(LineTable, result)
}

// js runs code in the page.
func (s *Server) js(ctx context.Context, out *writer, a args) error {
	code, err := a.need("code")
	if err != nil {
		return err
	}

	return s.eval(ctx, out, a, code)
}

// dom describes the elements sel matches, with their computed styles and their HTML
// when asked, at most limit of them.
func (s *Server) dom(ctx context.Context, out *writer, a args) error {
	sel, err := a.need("sel")
	if err != nil {
		return err
	}

	opts := map[string]any{"styles": a.flag("styles"), "html": a.flag("html"), "limit": a.number("limit", 50)}

	return s.eval(ctx, out, a, "nb.dom("+quoteJS(sel)+", "+quoteJS(opts)+")")
}

// cards describes the inbox's cards.
func (s *Server) cards(ctx context.Context, out *writer, a args) error {
	return s.eval(ctx, out, a, "nb.cards()")
}

// click clicks target, a selector or the name a control shows.
func (s *Server) click(ctx context.Context, out *writer, a args) error {
	target, err := a.need("target")
	if err != nil {
		return err
	}

	return s.eval(ctx, out, a, "nb.click("+quoteJS(target)+")")
}

// eval runs code in the page and answers with its value, waiting timeout seconds at most.
func (s *Server) eval(ctx context.Context, out *writer, a args, code string) error {
	seconds := a.number("timeout", defaultScriptSeconds)

	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()

	value, err := s.cfg.Page.Eval(ctx, code)
	if err != nil {
		return err
	}

	return out.send(LineResult, value)
}

// shot answers with a picture of the window.
func (s *Server) shot(ctx context.Context, out *writer, _ args) error {
	ctx, cancel := context.WithTimeout(ctx, shotTimeout)
	defer cancel()

	png, err := s.cfg.Shot(ctx)
	if err != nil {
		return err
	}

	return out.send(LineShot, Shot{PNG: png})
}

// stacks answers with every goroutine's stack: grouped (debug 1, the default), or each
// in full as a panic prints it (debug 2).
func (s *Server) stacks(_ context.Context, out *writer, a args) error {
	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, a.number("debug", 1)); err != nil {
		return fmt.Errorf("read the goroutines: %w", err)
	}

	return out.send(LineText, buf.String())
}

// reload reloads the page; the agent installs again as it loads.
func (s *Server) reload(_ context.Context, out *writer, _ args) error {
	if err := s.cfg.Page.Run("location.reload()"); err != nil {
		return err
	}

	return out.send(LineText, "The page is reloading.\n")
}

// quoteJS is v as a JavaScript literal, which JSON's form of it is.
func quoteJS(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "null"
	}

	return string(data)
}

// contains reports whether any of texts holds needle, which is lower case, in any case.
func contains(needle string, texts ...string) bool {
	for _, text := range texts {
		if strings.Contains(strings.ToLower(text), needle) {
			return true
		}
	}

	return false
}

// attrsText is a log record's attributes as key=value pairs.
func attrsText(attrs []Attr) string {
	var b strings.Builder

	for _, a := range attrs {
		b.WriteString(" " + a.Key + "=" + a.Value)
	}

	return b.String()
}

// megabytes is n bytes in megabytes, to a tenth.
func megabytes(n uint64) float64 {
	return float64(n*10/bytesPerMB) / 10
}
