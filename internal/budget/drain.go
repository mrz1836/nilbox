package budget

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
)

// A budget drains fast when something else signed in as the user (gh, a coding agent,
// another app) spends it quickly enough to run it out before it refills. The Tracker works
// that out from the readings the app's own responses carry, so it costs no request: each
// window keeps samples of what it had used, and what the app had spent of it, about a
// minute apart (sampleGap), and its pace is what changed from the oldest sample of the
// last paceSpan to the newest. A window drains when, over at least minPaceSpan of that,
// everything else used at least drainShare percent of the budget, and the pace of all of
// it, the app's spend included, runs it out before it refills. Only the hourly budgets
// drain: the search budget's window lasts a minute. The gauge shows the drain of the window
// it shows, and the Warner tells the user once an hour at most for each budget.

const (
	// sampleGap is about how far apart a window's samples are kept.
	sampleGap = time.Minute

	// paceSpan is how far back a pace looks, and minPaceSpan the least it needs to judge.
	paceSpan    = 15 * time.Minute
	minPaceSpan = 5 * time.Minute

	// drainShare is the least share of a budget, in percent, that everything else must have
	// used within the pace's span for it to drain.
	drainShare = 10

	// warnEvery is how often the user is told of one budget draining, at most.
	warnEvery = time.Hour
)

// drainBudgets are the budgets that can drain, in the order their alerts are told.
func drainBudgets() []string {
	return []string{github.ResourceGraphQL, github.ResourceCore}
}

// budgetLabel names a budget as the window does.
func budgetLabel(resource string) string {
	switch resource {
	case github.ResourceGraphQL:
		return "GraphQL"
	case github.ResourceCore:
		return "REST"
	default:
		return resource
	}
}

// sample is what a window had used, and what the app had spent of it, at a time.
type sample struct {
	at    time.Time
	used  int
	spent int
}

// keepSample samples the window as its newest reading left it, at now. The newest sample moves
// along until it is sampleGap past the one before, so a burst of responses keeps one, and
// samples older than paceSpan are let go, the newest always kept.
func (h *held) keepSample(now time.Time) {
	s := sample{at: now, used: h.reading.Used, spent: h.spent}

	if n := len(h.samples); n >= 2 && now.Sub(h.samples[n-2].at) < sampleGap {
		h.samples[n-1] = s
	} else {
		h.samples = append(h.samples, s)
	}

	cut := 0
	for cut < len(h.samples)-1 && now.Sub(h.samples[cut].at) > paceSpan {
		cut++
	}

	h.samples = h.samples[cut:]
}

// drainage is a window draining fast: what everything else and the app used over span, and
// when the window runs out at the pace of the two, before it refills at resetAt.
type drainage struct {
	others   int
	mine     int
	span     time.Duration
	runsOut  time.Time
	resetAt  time.Time
	resource string
}

// paceSamples returns the oldest of the window's samples within paceSpan of now, and the
// newest, or false when they are not two.
func (h *held) paceSamples(now time.Time) (from, to sample, ok bool) {
	since := now.Add(-paceSpan)

	for i, s := range h.samples[:max(len(h.samples)-1, 0)] {
		if !s.at.Before(since) {
			return h.samples[i], h.samples[len(h.samples)-1], true
		}
	}

	return sample{}, sample{}, false
}

// drain works out whether the window drains fast as of now (see the top of this file).
func (h *held) drain(now time.Time) (drainage, bool) {
	r := h.reading
	if windowLength(r.Resource) < time.Hour || r.Limit <= 0 {
		return drainage{}, false
	}

	from, to, ok := h.paceSamples(now)
	if !ok {
		return drainage{}, false
	}

	span := to.at.Sub(from.at)
	others := max((to.used-to.spent)-(from.used-from.spent), 0)
	total := to.used - from.used

	if span < minPaceSpan || total <= 0 || others*100 < drainShare*r.Limit {
		return drainage{}, false
	}

	// At the pace of the span, what is left lasts left/total of the span again.
	lasts := time.Duration(float64(span) * float64(max(r.Remaining, 0)) / float64(total))

	runsOut := to.at.Add(lasts)
	if !runsOut.Before(r.ResetAt) {
		return drainage{}, false
	}

	return drainage{
		others: others, mine: max(to.spent-from.spent, 0), span: span, runsOut: runsOut, resetAt: r.ResetAt,
		resource: r.Resource,
	}, true
}

// Drain is a budget draining fast, as the gauge shows it: the zero value while it doesn't.
// Every field is a plain value.
type Drain struct {
	// Draining is true while something else spends the budget fast enough to run it out
	// before it refills.
	Draining bool `json:"draining"`

	// Others is what everything else signed in as the user used of it over the last Minutes,
	// and Mine what the app spent of it meanwhile.
	Others  int `json:"others"`
	Mine    int `json:"mine"`
	Minutes int `json:"minutes"`

	// RunsOutAt is when it runs out at this pace, as RFC 3339: before it refills.
	RunsOutAt string `json:"runsOutAt"`
}

// minutes is a span in whole minutes, rounded.
func minutes(span time.Duration) int {
	return int(span.Round(time.Minute) / time.Minute)
}

// gaugeDrain converts a drainage for the gauge.
func gaugeDrain(d drainage) Drain {
	return Drain{
		Draining: true, Others: d.others, Mine: d.mine, Minutes: minutes(d.span),
		RunsOutAt: clock.Format(d.runsOut),
	}
}

// Alert is a budget draining fast, worded for a notification.
type Alert struct {
	// Resource is the budget (github.ResourceGraphQL or github.ResourceCore).
	Resource string

	// Title is its sentence, and Body what follows it.
	Title string
	Body  string

	// Key tells it from every other alert: the budget and the window it drains, so a
	// restart never tells the same one again.
	Key string
}

// alertFor words a drainage for the user, with times in loc.
func alertFor(d drainage, loc *time.Location) Alert {
	return Alert{
		Resource: d.resource,
		Title: fmt.Sprintf("Something signed in as you, not %s, used %s %s points in %d minutes.",
			brand.Name, countText(d.others), budgetLabel(d.resource), minutes(d.span)),
		Body: fmt.Sprintf("At this pace it runs out around %s, before it refills at %s.",
			d.runsOut.In(loc).Format("3:04 PM"), d.resetAt.In(loc).Format("3:04 PM")),
		Key: "budget-draining-" + d.resource + "-" + strconv.FormatInt(d.resetAt.Unix(), 10),
	}
}

// countText writes n with a comma between each three digits: 1,800.
func countText(n int) string {
	if n < 0 {
		return "-" + countText(-n)
	}

	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}

	return s
}

// alerts returns an alert for each budget draining now in the window the gauge shows, but
// none for a budget told of within warnEvery, and notes each as told.
func (t *Tracker) alerts() []Alert {
	t.mu.Lock()
	defer t.mu.Unlock()

	raw := t.clock.Now()
	now := raw.UTC()

	var out []Alert

	for _, resource := range drainBudgets() {
		h := t.shownLocked(resource, now)
		if h == nil {
			continue
		}

		d, ok := h.drain(now)
		if !ok {
			continue
		}

		if told, seen := t.warned[resource]; seen && now.Sub(told) < warnEvery {
			continue
		}

		t.warned[resource] = now

		out = append(out, alertFor(d, raw.Location()))
	}

	return out
}

// Notifier hears that a budget drains fast, once an hour at most for each.
type Notifier interface {
	Drained(ctx context.Context, alert Alert)
}

// Warner tells its Notifier when a budget drains fast. It looks after each change the
// Tracker makes, so it runs as the app's tracked work.
type Warner struct {
	tracker  *Tracker
	notifier Notifier
}

// NewWarner returns a Warner that tells notifier of tracker's budgets draining.
func NewWarner(tracker *Tracker, notifier Notifier) *Warner {
	return &Warner{tracker: tracker, notifier: notifier}
}

// Run looks for a budget draining after each change the Tracker makes until ctx ends.
func (w *Warner) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.tracker.looks:
		}

		for _, alert := range w.tracker.alerts() {
			w.notifier.Drained(ctx, alert)
		}
	}
}
