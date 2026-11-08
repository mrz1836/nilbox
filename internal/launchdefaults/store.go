package launchdefaults

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults/internal/queries"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Store reads and writes the stored launch defaults: the launch_defaults table, which no
// other package touches. The launcher reads them through it, and the prompt library
// learns from it where each prompt is a default.
//
// A Store from NewStore runs each call on the pool. One joined to a transaction with
// WithTx runs every call inside it.
type Store struct {
	q     *queries.Queries
	clock clock.Clock
}

// NewStore returns a Store over the pool db. Saves stamp the rows from c; a nil c is the
// system clock.
func NewStore(db *sql.DB, c clock.Clock) *Store {
	return &Store{q: queries.New(db), clock: clock.Or(c)}
}

// WithTx returns a Store that runs every call inside tx, a transaction on the same
// database, so its reads see what tx wrote and its writes commit or roll back with tx.
func (s *Store) WithTx(tx *sql.Tx) *Store {
	return &Store{q: s.q.WithTx(tx), clock: s.clock}
}

// All returns every stored launch default, by owner and then repository ignoring case,
// each owner's own defaults first.
func (s *Store) All(ctx context.Context) ([]Defaults, error) {
	rows, err := s.q.ListLaunchDefaults(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the launch defaults: %w", err)
	}

	out := make([]Defaults, 0, len(rows))

	for _, row := range rows {
		d, decodeErr := fromRow(row)
		if decodeErr != nil {
			return nil, fmt.Errorf("read the launch defaults: %w", decodeErr)
		}

		out = append(out, d)
	}

	return out, nil
}

// PromptUses lists every place a launch default names a prompt: each org's and each
// repository's default prompt for pull requests and for issues, in the order All
// returns the defaults. It implements prompts.UsageSource.
func (s *Store) PromptUses(ctx context.Context) ([]prompts.Use, error) {
	rows, err := s.q.ListPromptUses(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the launch defaults: %w", err)
	}

	var uses []prompts.Use

	for _, row := range rows {
		target := repo.Name{Owner: row.Owner, Repo: row.Name}

		if row.PRPromptID != nil {
			uses = append(uses, prompts.Use{PromptID: *row.PRPromptID, Kind: item.KindPR, Target: target})
		}

		if row.IssuePromptID != nil {
			uses = append(uses, prompts.Use{PromptID: *row.IssuePromptID, Kind: item.KindIssue, Target: target})
		}
	}

	return uses, nil
}

// save stores a validated Input with its extra arguments, replacing every setting of
// the defaults stored for the same target, whose owner and name match ignoring case and
// keep the casing they were first saved with. It returns the defaults as stored.
func (s *Store) save(ctx context.Context, in Input, args []string) (Defaults, error) {
	encoded, err := store.EncodeJSON(args)
	if err != nil {
		return Defaults{}, fmt.Errorf("save the launch defaults for %s: %w", target(in.Owner, in.Name), err)
	}

	now := s.now()

	row, err := s.q.SaveLaunchDefaults(ctx, queries.SaveLaunchDefaultsParams{
		Owner: in.Owner, Name: in.Name, Agent: in.Agent, Profile: in.Profile,
		PRPromptID: in.PRPromptID, IssuePromptID: in.IssuePromptID, Model: in.Model, Effort: in.Effort,
		PlanMode: in.PlanMode, Worktree: in.Worktree, ExtraArgs: encoded, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Defaults{}, fmt.Errorf("save the launch defaults for %s: %w", target(in.Owner, in.Name), err)
	}

	return toDefaults(row, textutil.NonNil(args)), nil
}

// remove deletes an org's (name "") or a repository's launch defaults, matching the
// owner and name ignoring case. It returns ErrNotFound when none are stored.
func (s *Store) remove(ctx context.Context, owner, name string) error {
	n, err := s.q.DeleteLaunchDefaults(ctx, queries.DeleteLaunchDefaultsParams{Owner: owner, Name: name})

	switch {
	case err != nil:
		return fmt.Errorf("remove the launch defaults for %s: %w", target(owner, name), err)
	case n == 0:
		return fmt.Errorf("%w: %s", ErrNotFound, target(owner, name))
	default:
		return nil
	}
}

// now is the time a save stamps, in UTC so stored timestamps sort as text.
func (s *Store) now() time.Time {
	return s.clock.Now().UTC()
}

// fromRow converts a stored row to Defaults. ExtraArgs is never nil: the schema only
// accepts a JSON array in extra_args, and an empty one decodes to an empty list.
func fromRow(row queries.LaunchDefault) (Defaults, error) {
	args, err := store.DecodeJSON[[]string](row.ExtraArgs)
	if err != nil {
		return Defaults{}, fmt.Errorf("%s: extra arguments: %w", target(row.Owner, row.Name), err)
	}

	return toDefaults(row, args), nil
}

// toDefaults converts a stored row whose extra arguments are args to Defaults.
func toDefaults(row queries.LaunchDefault, args []string) Defaults {
	return Defaults{
		Owner: row.Owner, Name: row.Name, Agent: row.Agent, Profile: row.Profile,
		PRPromptID: row.PRPromptID, IssuePromptID: row.IssuePromptID, Model: row.Model, Effort: row.Effort,
		PlanMode: row.PlanMode, Worktree: row.Worktree, ExtraArgs: args, ExtraFlags: agent.Join(args),
		UpdatedAt: clock.Format(row.UpdatedAt),
	}
}
