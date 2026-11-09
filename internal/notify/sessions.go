package notify

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// The agent sessions' kinds of notification. They start off, since the coding agents and
// the terminal can already notify the user themselves.
const (
	// KindSessionNeedsYou is an agent session waiting for the user's approval or input.
	KindSessionNeedsYou = notifyprefs.KindSessionNeedsYou

	// KindSessionFinished is an agent session that finished its turn.
	KindSessionFinished = notifyprefs.KindSessionFinished
)

// SessionBanners shows a banner when an agent session needs the user or finished its
// turn, for each kind the user turned on. Unlike the inbox's notifications, these are not
// kept in the notification log: the session's card already says what it is doing. It is
// the sessions Watcher's AttentionSink, and is not bound to the window.
type SessionBanners struct {
	base
}

// NewSessionBanners returns a SessionBanners.
func NewSessionBanners(cfg Config) *SessionBanners {
	return &SessionBanners{base: newBase(cfg)}
}

// NeedsYou shows that a session waits for the user, when that kind is on.
func (d *SessionBanners) NeedsYou(ctx context.Context, s sessions.Session) {
	d.show(ctx, KindSessionNeedsYou, s)
}

// Finished shows that a session finished its turn, when that kind is on.
func (d *SessionBanners) Finished(ctx context.Context, s sessions.Session) {
	d.show(ctx, KindSessionFinished, s)
}

// show posts a session's banner of kind, when the user allows banners and turned kind
// on. A failure is logged: the session goes on whether or not the user is told.
func (d *SessionBanners) show(ctx context.Context, kind notifyprefs.Kind, s sessions.Session) {
	if d.poster == nil || d.poster.Status() != StatusOn {
		return
	}

	off, err := d.bannersOff(ctx)
	if err != nil {
		d.logger.WarnContext(ctx, "could not read which banners are on", "error", err)

		return
	}

	if slices.Contains(off, kind) {
		return
	}

	if err = d.poster.Post(SessionBanner(kind, s)); err != nil {
		d.logger.WarnContext(ctx, "could not show a notification", "error", err)
	}
}

// SessionBanner is the banner for a session of kind: who and what as its title, its item
// as the subtitle, and what the agent last said, or the item's title, as its body. A
// click on it shows the item. Posting it again for the same session replaces it.
func SessionBanner(kind notifyprefs.Kind, s sessions.Session) Banner {
	name := string(s.Agent)
	if a, err := agent.Lookup(s.Agent); err == nil {
		name = a.Name
	}

	title := name + " " + s.State.Phrase()
	if kind == KindSessionFinished {
		title = name + " finished its turn"
	}

	body := strings.TrimSpace(s.LastMessage)
	if body == "" {
		body = s.Title
	}

	repoName := s.Repo
	if i := strings.LastIndexByte(repoName, '/'); i >= 0 {
		repoName = repoName[i+1:]
	}

	return Banner{
		ID:       brand.Slug + "-session-" + s.ID,
		Title:    title,
		Subtitle: repoName + " #" + strconv.Itoa(s.Number),
		Body:     textutil.Truncate(strings.Join(strings.Fields(body), " "), maxTitleRunes),
		Thread:   string(kind),
		ItemID:   s.ItemID,
		URL:      s.URL,
	}
}
