package ghthreads

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/store"
)

// KeySync is the preference that holds the state of the passes, as JSON: whose
// notifications the last pass read, where the next one starts, and how the last one went.
// Only this package writes it.
const KeySync = "ghthreads.sync"

// Access is whether GitHub lets the token read the user's notifications.
type Access string

// The answers about access.
const (
	// AccessUnchecked is no listing answered yet, for this account.
	AccessUnchecked Access = "unchecked"

	// AccessOK is GitHub answering the last listing.
	AccessOK Access = "ok"

	// AccessRefused is GitHub refusing the last listing (403 or 404), as it does a
	// fine-grained token: none is sent again for RefusedFor, unless CheckNow asks.
	AccessRefused Access = "refused"
)

// syncState is the state of the passes, which KeySync holds, for the account named.
type syncState struct {
	// Account is the login whose notifications the last pass read, "" before the first.
	Account string `json:"account,omitempty"`

	// ETag is page 1's ETag from its last answer, which the next listing sends; and
	// HighWater the newest update the last listing saw, where the next stops reading.
	// Both are written only by a listing that read every page it meant to.
	ETag      string    `json:"etag,omitempty"`
	HighWater time.Time `json:"highWater,omitzero"`

	// PollSeconds is how long GitHub last asked the app to wait between listings, in
	// seconds (X-Poll-Interval), 0 when it never said.
	PollSeconds int `json:"pollSeconds,omitempty"`

	// LastPass is when a listing last got GitHub's answer, and LastFullWalk when one last
	// read every page.
	LastPass     time.Time `json:"lastPass,omitzero"`
	LastFullWalk time.Time `json:"lastFullWalk,omitzero"`

	// Access is GitHub's last answer about the token, and AskAgainAt when a refused listing
	// is sent again.
	Access     Access    `json:"access,omitempty"`
	AskAgainAt time.Time `json:"askAgainAt,omitzero"`

	// LastError is what the window says about the last pass's failure, "" when it went
	// through.
	LastError string `json:"lastError,omitempty"`

	// Truncated is whether the last walk of every page stopped at the last page the app
	// reads while GitHub listed another (github.ThreadPage.Capped): the mirror holds only
	// the newest MaxThreads.
	Truncated bool `json:"truncated,omitempty"`

	// WalkNext is whether the next listing walks every page: the mirror went stale while
	// reading was off, or after GitHub refused it.
	WalkNext bool `json:"walkNext,omitempty"`
}

// pollInterval is PollSeconds as a duration.
func (st syncState) pollInterval() time.Duration {
	return time.Duration(st.PollSeconds) * time.Second
}

// access is Access, AccessUnchecked when none was recorded.
func (st syncState) access() Access {
	if st.Access == "" {
		return AccessUnchecked
	}

	return st.Access
}

// loadState reads the state of the passes. One never stored is the zero state; one that
// can't be decoded is logged and read as the zero state too, which only makes the next
// pass walk every page.
func (s *Service) loadState(ctx context.Context) (syncState, error) {
	stored, ok, err := s.prefs.Get(ctx, KeySync)
	if err != nil {
		return syncState{}, fmt.Errorf("read the state of the GitHub notifications: %w", err)
	}

	if !ok {
		return syncState{}, nil
	}

	st, err := store.DecodeJSON[syncState](stored)
	if err != nil {
		s.logger.WarnContext(ctx, "the state of the GitHub notifications can't be read; starting over",
			slog.Any("error", err))

		return syncState{}, nil
	}

	return st, nil
}

// saveState writes st through p, the pool's preferences or a transaction's.
func saveState(ctx context.Context, p *prefs.Store, st syncState) error {
	encoded, err := store.EncodeJSON(st)
	if err != nil {
		return err
	}

	if err = p.Set(ctx, KeySync, encoded); err != nil {
		return fmt.Errorf("store the state of the GitHub notifications: %w", err)
	}

	return nil
}
