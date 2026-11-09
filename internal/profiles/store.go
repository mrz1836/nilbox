package profiles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/profiles/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
)

// Store reads and writes the stored profiles of every agent: the agent_profiles table,
// which no other package touches.
type Store struct {
	db    *sql.DB
	q     *queries.Queries
	clock clock.Clock
}

// NewStore returns a Store over the pool db. Sync stamps the rows it adds from c; a nil c
// is the system clock.
func NewStore(db *sql.DB, c clock.Clock) *Store {
	return &Store{db: db, q: queries.New(db), clock: clock.Or(c)}
}

// All returns every stored profile, grouped by agent in the agents' order, each agent's
// default first and the rest by name. Folder, Missing, and SignedIn are left unset; the
// Service's List fills them in.
func (s *Store) All(ctx context.Context) ([]Profile, error) {
	rows, err := s.q.ListProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}

	profiles := make([]Profile, 0, len(rows))

	for _, row := range rows {
		p, decodeErr := fromRow(row)
		if decodeErr != nil {
			return nil, fmt.Errorf("list profiles: %w", decodeErr)
		}

		profiles = append(profiles, p)
	}

	sortProfiles(profiles)

	return profiles, nil
}

// Lookup returns the agent's stored profile named name, or ErrProfileNotFound.
func (s *Store) Lookup(ctx context.Context, id agent.ID, name string) (Profile, error) {
	row, err := s.q.GetProfile(ctx, queries.GetProfileParams{Agent: string(id), Name: name})

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Profile{}, fmt.Errorf("%w: %s %q", ErrProfileNotFound, agentName(id), name)
	case err != nil:
		return Profile{}, fmt.Errorf("look up profile: %w", err)
	}

	p, err := fromRow(row)
	if err != nil {
		return Profile{}, fmt.Errorf("look up profile: %w", err)
	}

	return p, nil
}

// Sync stores every discovered profile its agent has no row for yet, all of them or none.
// It never updates or deletes a row.
func (s *Store) Sync(ctx context.Context, found []Profile) error {
	if len(found) == 0 {
		return nil
	}

	now := s.clock.Now().UTC()

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for _, p := range found {
			params, err := insertParams(p, now)
			if err != nil {
				return err
			}

			if err = q.InsertProfile(ctx, params); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("store discovered profiles: %w", err)
	}

	return nil
}

// Prune removes the stored profiles that hold nothing worth keeping: named profiles whose
// directory is still there but holds no agent config, and whose settings are still the
// ones discovery gave them, so nobody edited them. Removing such a row loses nothing,
// and discovery adds it back if the directory later becomes an agent config. The default
// profile, edited profiles, and profiles whose directory is gone or cannot be read are
// kept, as is every profile of an agent this build does not know; an edited one is logged
// to log (nil discards it) as a warning. It returns the names it removed, as
// "<agent>/<name>", sorted.
func (s *Store) Prune(ctx context.Context, log *slog.Logger) ([]string, error) {
	log = logging.Or(log)

	rows, err := s.q.ListProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("read profiles to prune: %w", err)
	}

	var (
		ids     []int64
		removed []string
	)

	for _, row := range rows {
		p, decodeErr := fromRow(row)
		if decodeErr != nil {
			return nil, fmt.Errorf("read profiles to prune: %w", decodeErr)
		}

		if prunable(ctx, p, log) {
			ids = append(ids, row.ID)
			removed = append(removed, row.Agent+"/"+row.Name)
		}
	}

	if len(ids) == 0 {
		return nil, nil
	}

	if err = s.q.DeleteProfiles(ctx, ids); err != nil {
		return nil, fmt.Errorf("remove profiles with no agent config: %w", err)
	}

	slices.Sort(removed)

	return removed, nil
}

// fromRow converts a stored row to a Profile. Its lists and map are never nil: the schema
// only accepts a JSON array in each list column and an object in env, and an empty one
// decodes to an empty list or map.
func fromRow(row queries.AgentProfile) (Profile, error) {
	addDirs, err := store.DecodeJSON[[]string](row.AddDirs)
	if err != nil {
		return Profile{}, fmt.Errorf("profile %q: extra directories: %w", row.Name, err)
	}

	extraArgs, err := store.DecodeJSON[[]string](row.ExtraArgs)
	if err != nil {
		return Profile{}, fmt.Errorf("profile %q: extra arguments: %w", row.Name, err)
	}

	env, err := store.DecodeJSON[map[string]string](row.Env)
	if err != nil {
		return Profile{}, fmt.Errorf("profile %q: environment: %w", row.Name, err)
	}

	return Profile{
		Agent:          agent.ID(row.Agent),
		Name:           row.Name,
		ConfigDir:      row.ConfigDir,
		Model:          row.Model,
		Effort:         row.Effort,
		PermissionMode: row.PermissionMode,
		PlanMode:       row.PlanMode,
		AddDirs:        addDirs,
		ExtraArgs:      extraArgs,
		Env:            env,
	}, nil
}

// insertParams converts a Profile to a new row stamped now. Nil lists and maps are stored
// empty.
func insertParams(p Profile, now time.Time) (queries.InsertProfileParams, error) {
	addDirs, err := store.EncodeJSON(p.AddDirs)
	if err != nil {
		return queries.InsertProfileParams{}, fmt.Errorf("profile %q: extra directories: %w", p.Name, err)
	}

	extraArgs, err := store.EncodeJSON(p.ExtraArgs)
	if err != nil {
		return queries.InsertProfileParams{}, fmt.Errorf("profile %q: extra arguments: %w", p.Name, err)
	}

	env, err := store.EncodeJSON(p.Env)
	if err != nil {
		return queries.InsertProfileParams{}, fmt.Errorf("profile %q: environment: %w", p.Name, err)
	}

	return queries.InsertProfileParams{
		Agent:          string(p.Agent),
		Name:           p.Name,
		ConfigDir:      p.ConfigDir,
		Model:          p.Model,
		Effort:         p.Effort,
		PermissionMode: p.PermissionMode,
		PlanMode:       p.PlanMode,
		AddDirs:        addDirs,
		ExtraArgs:      extraArgs,
		Env:            env,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// agentName names an agent for a message: its name, or its ID when this build does not
// know it.
func agentName(id agent.ID) string {
	if a, err := agent.Lookup(id); err == nil && id != agent.Inherit {
		return a.Name
	}

	return string(id)
}
