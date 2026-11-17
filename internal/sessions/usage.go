package sessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/agent"
)

// ErrNoUsage is returned by a UsageReader that finds no token counts for the session: no
// file of its own, or one that reports none.
var ErrNoUsage = errors.New("the session's token counts can't be read")

// Usage is how many tokens an agent session used, read on demand for the session panel:
// as the agent's own files on this Mac report them, or, for a background run's session,
// as its run reported them. The four parts add up to Total: Input is the input not read
// from the cache, CacheRead what was, CacheWrite what was written to it, and Output what
// the agent wrote, its reasoning included. A run reports what it wrote to the cache within
// Input, so its CacheWrite is 0.
type Usage struct {
	// Known reports counts read; false, every count is 0.
	Known bool `json:"known"`

	// FromRun reports counts a background run reported, which its record keeps, rather
	// than the agent's files.
	FromRun bool `json:"fromRun"`

	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
	Total      int64 `json:"total"`

	// Partial reports counts read from only part of the files, which were too long to
	// read whole: the session used at least that many.
	Partial bool `json:"partial"`
}

// Sum returns the usage with Total worked out from its parts, and Known.
func (u Usage) Sum() Usage {
	u.Known = true
	u.Total = u.Input + u.Output + u.CacheRead + u.CacheWrite

	return u
}

// UsageReader reads the token counts of one agent's session from the agent's own files on
// this Mac. It never reaches the network. Each agent's observer package provides one.
type UsageReader interface {
	Usage(ctx context.Context, r Record) (Usage, error)
}

// RunTokens reads the tokens a background run reported so far, by the run engine's run
// (Record.RunID), as Autopilot's ledger records them; core adapts it. False when the run
// is recorded nowhere.
type RunTokens interface {
	RunTokens(ctx context.Context, runID int64) (Usage, bool, error)
}

// Usage returns how many tokens the session with id used: a background run's as its run
// reported them, any other's read from the agent's own files. Known false, with no error,
// for a session the agent hasn't named yet, an agent with no reader, a run that reported
// none, or files that report none.
func (s *Service) Usage(ctx context.Context, id string) (Usage, error) {
	r, err := s.store.Get(ctx, id)
	if err != nil {
		return Usage{}, fmt.Errorf("read the session: %w", err)
	}

	if r.RunID > 0 {
		return s.runUsage(ctx, r), nil
	}

	reader := s.usage[r.Agent]
	if reader == nil || r.AgentSessionID == "" {
		return Usage{}, nil
	}

	got, err := reader.Usage(ctx, r)
	if err != nil {
		if !errors.Is(err, ErrNoUsage) {
			s.logger.DebugContext(ctx, "could not read a session's token counts", "agent", r.Agent, "error", err)
		}

		return Usage{}, nil
	}

	if !got.Known {
		return Usage{}, nil
	}

	return got, nil
}

// runUsage is how many tokens the background run's session r used, as its run reported
// them: none without a reader, or for a run that reported none or can't be read, logged.
func (s *Service) runUsage(ctx context.Context, r Record) Usage {
	if s.tokens == nil {
		return Usage{}
	}

	got, ok, err := s.tokens.RunTokens(ctx, r.RunID)
	if err != nil {
		s.logger.DebugContext(ctx, "could not read a background run's tokens", "run", r.RunID, "error", err)

		return Usage{}
	}

	if !ok || got.Input+got.Output+got.CacheRead+got.CacheWrite <= 0 {
		return Usage{}
	}

	got = got.Sum()
	got.FromRun = true

	return got
}

// usageReaders returns the readers in cfg, by agent, leaving out nil ones.
func usageReaders(readers map[agent.ID]UsageReader) map[agent.ID]UsageReader {
	out := make(map[agent.ID]UsageReader, len(readers))

	for id, r := range readers {
		if r != nil {
			out[id] = r
		}
	}

	return out
}
