package core

import (
	"context"
	"fmt"

	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
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
