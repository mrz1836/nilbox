package scripts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scripts/internal/queries"
)

// trustKey names a command the user trusted within a repository: its source and its name
// there.
type trustKey struct {
	source Source
	name   string
}

// trustRecord is what the user trusted under a key: the hash of the command's effective
// form then, and its display text.
type trustRecord struct {
	sha256  string
	command string
}

// trustStore keeps the commands the user trusted, in script_trust. A store without a
// database keeps nothing, so every repository command asks.
type trustStore struct {
	q     *queries.Queries
	clock clock.Clock
}

// newTrustStore returns the store over db, which may be nil.
func newTrustStore(db *sql.DB, clk clock.Clock) trustStore {
	t := trustStore{clock: clock.Or(clk)}
	if db != nil {
		t.q = queries.New(db)
	}

	return t
}

// forRepo returns every command the user trusted in the repository name.
func (t trustStore) forRepo(ctx context.Context, name repo.Name) (map[trustKey]trustRecord, error) {
	out := map[trustKey]trustRecord{}
	if t.q == nil || !name.Valid() {
		return out, nil
	}

	rows, err := t.q.RepoTrust(ctx, queries.RepoTrustParams{RepoOwner: name.Owner, RepoName: name.Repo})
	if err != nil {
		return nil, fmt.Errorf("read the trusted commands of %s: %w", name, err)
	}

	for _, r := range rows {
		out[trustKey{source: Source(r.Source), name: r.Name}] = trustRecord{sha256: r.Sha256, command: r.Command}
	}

	return out, nil
}

// get returns what the user trusted under key in the repository name, and whether they
// trusted anything there.
func (t trustStore) get(ctx context.Context, name repo.Name, key trustKey) (trustRecord, bool, error) {
	if t.q == nil || !name.Valid() {
		return trustRecord{}, false, nil
	}

	row, err := t.q.GetTrust(ctx, queries.GetTrustParams{
		RepoOwner: name.Owner, RepoName: name.Repo, Source: string(key.source), Name: key.name,
	})

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return trustRecord{}, false, nil
	case err != nil:
		return trustRecord{}, false, fmt.Errorf("read a trusted command of %s: %w", name, err)
	}

	return trustRecord{sha256: row.Sha256, command: row.Command}, true, nil
}

// set records that the user trusted the command under key in the repository name, as it
// is now: rec's hash and display text.
func (t trustStore) set(ctx context.Context, name repo.Name, key trustKey, rec trustRecord) error {
	if t.q == nil || !name.Valid() {
		return nil
	}

	err := t.q.SetTrust(ctx, queries.SetTrustParams{
		RepoOwner: name.Owner, RepoName: name.Repo, Source: string(key.source), Name: key.name, Sha256: rec.sha256,
		Command: rec.command, GrantedAt: t.clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("remember a trusted command of %s: %w", name, err)
	}

	return nil
}

// withTrust returns sc's entry with what running it asks first, from the records of what
// the user trusted; who names the repository in a custom magex target's question.
func withTrust(sc script, records map[trustKey]trustRecord, who string) Entry {
	e := sc.entry
	e.Trust, e.SHA256 = TrustNone, sc.hash

	if !sc.needsTrust() {
		return e
	}

	rec, ok := records[trustKey{source: e.Source, name: sc.name}]

	switch {
	case !ok:
		e.Trust = TrustAsk
	case rec.sha256 == sc.hash:
		e.Trust = TrustTrusted
	default:
		e.Trust, e.Trusted = TrustChanged, rec.command
	}

	if e.Custom {
		e.TrustNote = "This runs " + who + "'s own magefiles code."
		if e.Trust == TrustChanged {
			e.TrustNote = who + "'s magefiles changed since you last ran it."
		}
	}

	return e
}
