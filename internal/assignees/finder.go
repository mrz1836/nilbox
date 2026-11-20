package assignees

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Person is someone a read of GitHub named: their login, and their profile's display name,
// "" when it gave none.
type Person struct {
	Login string
	Name  string
}

// Found is what a read of a repository's assignable users found: the people it named, the
// first page, and how many GitHub said match in all.
type Found struct {
	People []Person
	Total  int
}

// Read reads the assignable users of the item's repository whose login or name matches
// query, "" for everyone, as the sheet that asks makes the read: one GraphQL request of one
// point, through its inbox's footing.
type Read func(ctx context.Context, query string) (Found, error)

// Assignable is someone the directory knows can be assigned in a repository: whether GitHub
// listed them there (Listed), and when the app last saw them assigned there, zero for never.
type Assignable struct {
	Login      string
	Name       string
	Listed     bool
	AssignedAt time.Time
}

// Worker is someone the directory knows works on a repository: what they did there and when
// they were last seen there, and, among the owner's other repositories, which one (Where).
type Worker struct {
	Login     string
	Name      string
	Reviewed  int
	Authored  int
	Committed int
	LastAt    time.Time
	Where     string
}

// Known is what the directory knows of who can be assigned in a repository: everyone it
// learned can be (Assignable, by login), who works on it (WorksHere, the highest ranked
// first), and who works on the owner's other repositories (Org, likewise); and of GitHub's
// whole list, how many it said can be (Total), whether its last read named everyone
// (Complete), when that read was (ReadAt, zero before one), and whether one is due (Stale).
type Known struct {
	Assignable []Assignable
	WorksHere  []Worker
	Org        []Worker
	Total      int
	Complete   bool
	ReadAt     time.Time
	Stale      bool
}

// Directory is the People directory as the pickers use it: what it knows of a repository,
// whether what it holds answers a search (Covers), and the reads and assignments it keeps.
// core adapts *people.Service to it.
type Directory interface {
	Known(ctx context.Context, name repo.Name) (Known, error)
	Covers(ctx context.Context, name repo.Name, query string) (bool, error)
	Keep(ctx context.Context, name repo.Name, query string, found Found) error
	MarkFailed(ctx context.Context, name repo.Name, query string) error
	Assigned(ctx context.Context, name repo.Name, logins []string) error
}

// Item is the issue or pull request a picker is for, as its sheet read it: its repository,
// the signed-in user (Viewer), who opened it and whether that is a person, and who took part
// in it, people only.
type Item struct {
	Repo           repo.Name
	Viewer         string
	Author         string
	AuthorIsPerson bool
	Participants   []string
}

// Finder offers people to assign, and learns and searches who can be, for both sheets. A nil
// Finder offers no one, and reads nothing. It is safe for concurrent use.
type Finder struct {
	dir    Directory
	logger *slog.Logger

	// reads makes one read of GitHub at a time per repository and search.
	reads flight.Group[Found]
}

// NewFinder returns a Finder over dir, logging to logger (nil discards).
func NewFinder(dir Directory, logger *slog.Logger) *Finder {
	return &Finder{dir: dir, logger: logging.Or(logger)}
}

// none is a list of no one.
func none() Candidates {
	return Candidates{People: []Candidate{}}
}

// Candidates returns whom the picker offers to assign to it, from the directory alone, with
// no request.
func (f *Finder) Candidates(ctx context.Context, it Item) (Candidates, error) {
	if f == nil {
		return none(), nil
	}

	k, err := f.dir.Known(ctx, it.Repo)
	if err != nil {
		return none(), err
	}

	return candidatesOf(it, k, nil, ""), nil
}

// Learn reads GitHub's whole list of who can be assigned in its repository with read, when
// a read is due (Candidates.Stale), keeps it, and returns Candidates with what it learned:
// one request of one point, one at a time per repository. A read that fails is recorded, so
// none is due for 30 minutes, and is the error; one not due sends nothing.
func (f *Finder) Learn(ctx context.Context, it Item, read Read) (Candidates, error) {
	if f == nil {
		return none(), nil
	}

	k, err := f.dir.Known(ctx, it.Repo)
	if err != nil {
		return none(), err
	}

	if !k.Stale {
		return candidatesOf(it, k, nil, ""), nil
	}

	if _, err = f.read(ctx, it.Repo, "", read); err != nil {
		return none(), err
	}

	return f.Candidates(ctx, it)
}

// Search returns whom the picker offers to assign to it whose login or name matches query,
// those that match best first: from the directory alone when what it holds answers the
// search (Directory.Covers), else after one read of GitHub's assignable users that match,
// one point, which it keeps, the people it found and the list holds nothing else of marked
// SourceFound. One search per repository and words runs at a time. A read that fails is
// recorded, and is the error.
func (f *Finder) Search(ctx context.Context, it Item, query string, read Read) (Candidates, error) {
	if f == nil {
		return none(), nil
	}

	query = strings.TrimSpace(query)

	covered, err := f.dir.Covers(ctx, it.Repo, query)
	if err != nil {
		return none(), err
	}

	var found Found

	if !covered {
		if found, err = f.read(ctx, it.Repo, query, read); err != nil {
			return none(), err
		}
	}

	k, err := f.dir.Known(ctx, it.Repo)
	if err != nil {
		return none(), err
	}

	return candidatesOf(it, k, found.People, query), nil
}

// Seen keeps logins as assigned in the repository name, as a read saw them or the user just
// assigned them. A failure is logged.
func (f *Finder) Seen(ctx context.Context, name repo.Name, logins []string) {
	if f == nil || len(logins) == 0 {
		return
	}

	if err := f.dir.Assigned(ctx, name, logins); err != nil {
		f.logger.DebugContext(ctx, "could not keep who is assigned", "error", err)
	}
}

// read reads who can be assigned in the repository name that query matches ("" for the
// whole list) with read, once at a time per repository and words, unless what the directory
// holds answers it by the time the read would start, and keeps what it found.
func (f *Finder) read(ctx context.Context, name repo.Name, query string, read Read) (Found, error) {
	key := name.Key() + "\x00" + strings.ToLower(query)

	return f.reads.Do(ctx, key, func(ctx context.Context) (Found, error) {
		due, err := f.due(ctx, name, query)
		if err != nil || !due {
			return Found{}, err
		}

		found, err := read(ctx, query)
		if err != nil {
			if markErr := f.dir.MarkFailed(ctx, name, query); markErr != nil {
				f.logger.DebugContext(ctx, "could not record the failed read of who can be assigned", "error", markErr)
			}

			return Found{}, readError(name, query, err)
		}

		return found, f.dir.Keep(ctx, name, query, found)
	})
}

// due reports that a read of the repository name for query is still due: for the whole list
// (""), the directory's is stale; for a search, nothing it holds answers it.
func (f *Finder) due(ctx context.Context, name repo.Name, query string) (bool, error) {
	if query == "" {
		k, err := f.dir.Known(ctx, name)

		return k.Stale, err
	}

	covered, err := f.dir.Covers(ctx, name, query)

	return !covered, err
}

// readError words a failed read of who can be assigned in the repository name, for query.
func readError(name repo.Name, query string, err error) error {
	if query == "" {
		return uierr.New(fmt.Sprintf("Couldn't learn who can be assigned in %s: %s", name, uierr.Message(err)), err)
	}

	return uierr.New(fmt.Sprintf("Couldn't search GitHub for %q in %s: %s", query, name, uierr.Message(err)), err)
}

// candidatesOf is the list rank makes of it, k, found, and query, with what k says of
// GitHub's whole list.
func candidatesOf(it Item, k Known, found []Person, query string) Candidates {
	return Candidates{
		People: rank(it, k, found, query), Total: k.Total, Complete: k.Complete, ReadAt: stamp(k.ReadAt), Stale: k.Stale,
	}
}
