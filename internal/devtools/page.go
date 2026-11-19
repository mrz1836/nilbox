package devtools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Prefix starts every message the agent posts to the app, so the app tells them from any
// other message the page sends it. The agent's script spells it too (agentScript).
const Prefix = "devtools:"

// ErrNoWindow is returned for a request that needs the window's page before the window
// has opened.
var ErrNoWindow = errors.New("the window has not opened yet")

// ErrNoAnswer is returned when the page did not answer before the request's deadline,
// as when it was reloading.
var ErrNoAnswer = errors.New("the page did not answer in time")

// ErrScript is returned, wrapped with what the page said, for JavaScript that threw.
var ErrScript = errors.New("the script failed in the page")

// agentScript is the agent the page runs: it is run on every page load and before each
// evaluation, and does nothing once installed.
//
//go:embed agent.js
var agentScript string

// ConsoleEntry is one line the page wrote to its console, or an error nothing caught.
type ConsoleEntry struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
}

// TrailEntry is one thing the user did in the page: a click, a key, typing in a field,
// or a choice changed, with what it was done to, and where.
type TrailEntry struct {
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`
	Target string    `json:"target"`
	Where  string    `json:"where,omitempty"`
}

// PageInfo is the page as the agent last described it: when it loaded, its address and
// title, and its viewport.
type PageInfo struct {
	Loaded    time.Time `json:"loaded"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Scale     float64   `json:"scale"`
	UserAgent string    `json:"userAgent"`
}

// agentMessage is a message the agent posted, by its type (T): a "result" of an
// evaluation, a "console" line, a "trail" entry, or "hello", the page describing itself
// as the agent installs.
type agentMessage struct {
	T      string          `json:"t"`
	ID     int64           `json:"id"`
	OK     bool            `json:"ok"`
	Value  json.RawMessage `json:"value"`
	Error  string          `json:"error"`
	Level  string          `json:"level"`
	Text   string          `json:"text"`
	Kind   string          `json:"kind"`
	Target string          `json:"target"`
	Where  string          `json:"where"`
	URL    string          `json:"url"`
	Title  string          `json:"title"`
	Width  int             `json:"width"`
	Height int             `json:"height"`
	Scale  float64         `json:"scale"`
	UA     string          `json:"ua"`
}

// evalResult is what an evaluation came back with.
type evalResult struct {
	value json.RawMessage
	err   error
}

// Page is the inspector's way into the window's page, through the agent the page runs:
// the app runs JavaScript there (Eval) through the window's ExecJS, which Attach hands
// it, and hands back each message the page posts (Receive), which carries an
// evaluation's result, a console line, an entry of the user's trail, or the page
// describing itself. It is safe for concurrent use.
type Page struct {
	mu      sync.Mutex
	exec    func(js string)
	pending map[int64]chan evalResult
	next    int64
	info    PageInfo
	console *Feed[ConsoleEntry]
	trail   *Feed[TrailEntry]
	now     func() time.Time
}

// NewPage returns a page with no window yet, keeping the console's last consoleSize
// lines and the trail's last trailSize entries, stamped with now.
func NewPage(consoleSize, trailSize int, now func() time.Time) *Page {
	return &Page{
		pending: map[int64]chan evalResult{},
		console: NewFeed[ConsoleEntry](consoleSize),
		trail:   NewFeed[TrailEntry](trailSize),
		now:     now,
	}
}

// Attach gives the page the window's way to run JavaScript in it.
func (p *Page) Attach(exec func(js string)) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.exec = exec
}

// Agent returns the script that installs the agent in the page, for the window to run on
// every page load.
func (p *Page) Agent() string {
	return agentScript
}

// Console returns the feed of the page's console.
func (p *Page) Console() *Feed[ConsoleEntry] {
	return p.console
}

// Trail returns the feed of what the user did in the page.
func (p *Page) Trail() *Feed[TrailEntry] {
	return p.trail
}

// Info returns the page as the agent last described it.
func (p *Page) Info() PageInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.info
}

// Run runs js in the page without waiting for anything back, or returns ErrNoWindow.
func (p *Page) Run(js string) error {
	p.mu.Lock()
	exec := p.exec
	p.mu.Unlock()

	if exec == nil {
		return ErrNoWindow
	}

	exec(js)

	return nil
}

// Eval runs code in the page, with the agent's helpers as nb, and returns what it gave
// back as JSON, once a promise it returns settles. code is an expression, or statements
// that return a value; it fails with ErrScript when it throws, ErrNoWindow before the
// window opens, and ErrNoAnswer when ctx ends first.
func (p *Page) Eval(ctx context.Context, code string) (json.RawMessage, error) {
	quoted, err := json.Marshal(code)
	if err != nil {
		return nil, fmt.Errorf("quote the script: %w", err)
	}

	id, answer := p.await()
	defer p.forget(id)

	// The agent comes first, in case the page loaded without it: it installs only once.
	if err = p.Run(fmt.Sprintf("%s\n;window.__appDevtools.run(%d, %s)", agentScript, id, quoted)); err != nil {
		return nil, err
	}

	select {
	case result := <-answer:
		return result.value, result.err
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %w", ErrNoAnswer, ctx.Err())
	}
}

// Receive handles message, which the page posted to the app: it reports false, doing
// nothing, for a message that is not the agent's.
func (p *Page) Receive(message string) bool {
	body, ok := strings.CutPrefix(message, Prefix)
	if !ok {
		return false
	}

	var m agentMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return true
	}

	switch m.T {
	case "result":
		p.answer(m)
	case "console":
		p.console.Add(ConsoleEntry{Time: p.now(), Level: m.Level, Text: m.Text})
	case "trail":
		p.trail.Add(TrailEntry{Time: p.now(), Kind: m.Kind, Target: m.Target, Where: m.Where})
	case "hello":
		p.hello(m)
	}

	return true
}

// await registers a new evaluation and returns its ID and where its result arrives.
func (p *Page) await() (int64, chan evalResult) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.next++
	answer := make(chan evalResult, 1)
	p.pending[p.next] = answer

	return p.next, answer
}

// forget drops the evaluation id, answered or given up on.
func (p *Page) forget(id int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.pending, id)
}

// answer hands an evaluation's result to whoever waits on it; one nobody waits on any
// longer is dropped.
func (p *Page) answer(m agentMessage) {
	p.mu.Lock()
	answer, ok := p.pending[m.ID]
	p.mu.Unlock()

	if !ok {
		return
	}

	result := evalResult{value: m.Value}
	if !m.OK {
		result = evalResult{err: fmt.Errorf("%w: %s", ErrScript, m.Error)}
	}

	if len(result.value) == 0 && result.err == nil {
		result.value = json.RawMessage("null")
	}

	// The channel holds the one answer an evaluation gets; a second is dropped rather
	// than holding up the window's messages.
	select {
	case answer <- result:
	default:
	}
}

// hello records the page as the agent described it on installing.
func (p *Page) hello(m agentMessage) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.info = PageInfo{
		Loaded: p.now(), URL: m.URL, Title: m.Title, Width: m.Width, Height: m.Height, Scale: m.Scale, UserAgent: m.UA,
	}
}
