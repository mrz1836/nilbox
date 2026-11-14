package core

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/ghthreads"
	"github.com/mrz1836/nilbox/internal/inbox"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// ghThreadRules reads the user's rules for their GitHub notifications, which settings
// writes, in one read of the prefs (ghthreads.RuleSource).
type ghThreadRules struct {
	prefs *prefsstore.Store
}

// ThreadRules returns the rules as stored.
func (r ghThreadRules) ThreadRules(ctx context.Context) (threadrules.Rules, error) {
	stored, err := r.prefs.GetMany(ctx, threadrules.Keys()...)
	if err != nil {
		return threadrules.Rules{}, fmt.Errorf("read the rules for GitHub notifications: %w", err)
	}

	return threadrules.Decode(stored), nil
}

// ghThreadHolds reads how long the window's toasts stay, which the Undo hold of a clear
// the user's own action chose follows (ghthreads.HoldSource).
type ghThreadHolds struct {
	settings *settings.Service
}

// ToastSeconds returns the toast setting as stored.
func (h ghThreadHolds) ToastSeconds(ctx context.Context) (int, error) {
	return settingsPart(ctx, h.settings, func(s settings.Settings) int { return s.ToastSeconds })
}

// ghThreadOutcomes passes what the user finished from the inbox on to the mirror of their
// GitHub notifications (inbox.OutcomeSink), which records the moments that clear them.
type ghThreadOutcomes struct {
	settler *ghthreads.Settler
}

// Settle tells the mirror of outcomes, each as its own kind.
func (o ghThreadOutcomes) Settle(ctx context.Context, outcomes []inbox.Outcome) {
	told := make([]ghthreads.Outcome, 0, len(outcomes))

	for _, out := range outcomes {
		kind, ok := outcomeKinds()[out.Kind]
		if !ok {
			continue
		}

		told = append(told, ghthreads.Outcome{Kind: kind, Login: out.Login, Ref: out.Ref, At: out.At})
	}

	if len(told) > 0 {
		o.settler.Settle(ctx, told)
	}
}

// outcomeKinds maps each kind of the inbox's outcomes to the mirror's.
func outcomeKinds() map[inbox.OutcomeKind]ghthreads.OutcomeKind {
	return map[inbox.OutcomeKind]ghthreads.OutcomeKind{
		inbox.OutcomeMerged:      ghthreads.OutcomeMerged,
		inbox.OutcomeClosed:      ghthreads.OutcomeClosed,
		inbox.OutcomeReviewed:    ghthreads.OutcomeReviewed,
		inbox.OutcomeDismissed:   ghthreads.OutcomeDismissed,
		inbox.OutcomeRestored:    ghthreads.OutcomeRestored,
		inbox.OutcomeUnassigned:  ghthreads.OutcomeUnassigned,
		inbox.OutcomeUnrequested: ghthreads.OutcomeUnrequested,
		inbox.OutcomeSeenClosed:  ghthreads.OutcomeSeenClosed,
		inbox.OutcomeUnlisted:    ghthreads.OutcomeUnlisted,
	}
}
