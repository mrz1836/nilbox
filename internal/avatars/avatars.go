// Package avatars keeps the profile pictures of the people the sheets show, by login. Each
// is downloaded from GitHub's avatar host by its login alone (github's FetchLoginAvatar: no
// API request, so no token and no point of the budget), kept in the avatars table, which
// this package owns, and downloaded again only once it is a week old and shown again. The
// window gets them as data: URLs, so it loads nothing remote itself.
package avatars

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/avatars/internal/queries"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/store"
)

const (
	// MaxLogins is the most logins one call looks at; the rest get no picture.
	MaxLogins = 100

	// refreshAfter is how old a kept picture is downloaded again at, the next time it is
	// shown: a login's address names no version, so a changed picture shows only then.
	refreshAfter = 7 * 24 * time.Hour

	// touchAfter is how long a kept picture's last showing stands before it is stamped
	// again, so a picture shown often is written at most once a day.
	touchAfter = 24 * time.Hour

	// unusedFor is how long a picture no sheet shows is kept, and maxKept and maxKeptBytes
	// cap them all: the least recently shown past either are dropped.
	unusedFor    = 30 * 24 * time.Hour
	maxKept      = 2000
	maxKeptBytes = 16 << 20

	// retryAfter is how long a login whose download failed goes without another; and
	// maxFailed how many such logins are remembered before all are forgotten at once.
	retryAfter = 30 * time.Minute
	maxFailed  = 1000

	// downloads is how many run at once, and downloadTimeout how long each may take.
	downloads       = 4
	downloadTimeout = 15 * time.Second
)

// Fetcher downloads a login's picture. *github.Client implements it.
type Fetcher interface {
	FetchLoginAvatar(ctx context.Context, login string) (*github.Avatar, error)
}

// Config holds a Service's dependencies. Pool and Fetcher are required.
type Config struct {
	// Pool is the database, which keeps the pictures.
	Pool    *sql.DB
	Fetcher Fetcher

	Clock  clock.Clock
	Logger *slog.Logger
}

// Service serves the pictures. It is safe for concurrent use.
type Service struct {
	db      *sql.DB
	q       *queries.Queries
	fetcher Fetcher
	clock   clock.Clock
	logger  *slog.Logger

	// fetches downloads each login once at a time, for every call that wants it.
	fetches flight.Group[*github.Avatar]

	// failed holds when each login's last download failed, under mu.
	mu     sync.Mutex
	failed map[string]time.Time
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{
		db: cfg.Pool, q: queries.New(cfg.Pool), fetcher: cfg.Fetcher,
		clock: clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger), failed: map[string]time.Time{},
	}
}

// Pictures returns the profile pictures of logins as data: URLs, keyed by each login as
// given, and leaves out a login it has none for: one no user has (a bot's), or whose
// download failed. Logins are compared as GitHub compares them, ignoring case, and only the
// first MaxLogins different ones are looked at. A kept picture is served as kept until it is
// refreshAfter old; one missing or that old is downloaded, downloads at a time, and kept. A
// download that fails leaves the picture kept, if any, and the login isn't tried again for
// retryAfter. Only a failure to read the kept pictures is an error; one to keep them is
// logged, and the pictures are served all the same.
func (s *Service) Pictures(ctx context.Context, logins []string) (map[string]string, error) {
	asked, keys := folded(logins)
	if len(keys) == 0 {
		return map[string]string{}, nil
	}

	now := s.clock.Now()

	have, err := s.read(ctx, keys, now)
	if err != nil {
		return nil, err
	}

	got := s.download(ctx, s.untried(have.due, now))
	if err = ctx.Err(); err != nil {
		return nil, fmt.Errorf("read the profile pictures: %w", err)
	}

	if len(got) > 0 || have.stale {
		if err = s.keep(ctx, got, keys, now); err != nil {
			s.logger.WarnContext(ctx, "keep the profile pictures", slog.Any("error", err))
		}
	}

	for key, picture := range got {
		have.pictures[key] = picture
	}

	return named(asked, have.pictures), nil
}

// named keys the pictures, as data: URLs, by every login asked for each, leaving out those
// with none.
func named(asked map[string][]string, pictures map[string]*github.Avatar) map[string]string {
	out := make(map[string]string, len(asked))

	for key, names := range asked {
		if picture := pictures[key]; picture != nil {
			url := picture.DataURL()
			for _, name := range names {
				out[name] = url
			}
		}
	}

	return out
}

// kept is what is kept of some logins' pictures, as of a moment: the pictures, by key; the
// keys to download, missing or refreshAfter old; and whether any was last shown touchAfter
// ago or more, so stamping them is due.
type kept struct {
	pictures map[string]*github.Avatar
	due      []string
	stale    bool
}

// read reads what is kept of the pictures of keys, as of now.
func (s *Service) read(ctx context.Context, keys []string, now time.Time) (kept, error) {
	rows, err := s.q.ListAvatars(ctx, keys)
	if err != nil {
		return kept{}, fmt.Errorf("read the kept profile pictures: %w", err)
	}

	out := kept{pictures: make(map[string]*github.Avatar, len(keys))}

	for _, r := range rows {
		out.pictures[r.Login] = &github.Avatar{ContentType: r.Mime, Data: r.Image}

		if now.Sub(r.FetchedAt) >= refreshAfter {
			out.due = append(out.due, r.Login)
		}

		out.stale = out.stale || now.Sub(r.UsedAt) >= touchAfter
	}

	for _, key := range keys {
		if out.pictures[key] == nil {
			out.due = append(out.due, key)
		}
	}

	return out, nil
}

// folded groups logins by their lower-cased key, the first MaxLogins different ones, leaving
// out "", and returns the keys in the order first seen.
func folded(logins []string) (map[string][]string, []string) {
	asked := map[string][]string{}

	var keys []string

	for _, login := range logins {
		if login == "" {
			continue
		}

		key := strings.ToLower(login)
		if _, ok := asked[key]; !ok {
			if len(keys) == MaxLogins {
				continue
			}

			keys = append(keys, key)
		}

		asked[key] = append(asked[key], login)
	}

	return asked, keys
}

// untried returns the logins whose download didn't fail within retryAfter of now,
// forgetting the failures that old.
func (s *Service) untried(logins []string, now time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := logins[:0:0]

	for _, login := range logins {
		if at, ok := s.failed[login]; ok {
			if now.Sub(at) < retryAfter {
				continue
			}

			delete(s.failed, login)
		}

		out = append(out, login)
	}

	return out
}

// failedAt remembers that login's download failed at.
func (s *Service) failedAt(login string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.failed) >= maxFailed {
		clear(s.failed)
	}

	s.failed[login] = at
}

// download downloads the logins' pictures, downloads at a time, and returns the ones it
// got. A download that fails is remembered (failedAt), unless ctx ended, which ends them
// all.
func (s *Service) download(ctx context.Context, logins []string) map[string]*github.Avatar {
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	out := map[string]*github.Avatar{}
	slots := make(chan struct{}, downloads)

	for _, login := range logins {
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}

			defer func() { <-slots }()

			if picture := s.downloadOne(ctx, login); picture != nil {
				mu.Lock()
				out[login] = picture
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	return out
}

// downloadOne downloads login's picture, within downloadTimeout, once for every call that
// wants it at the time, and returns nil when that fails, remembering the failure (failedAt)
// unless ctx ended.
func (s *Service) downloadOne(ctx context.Context, login string) *github.Avatar {
	picture, err := s.fetches.Do(ctx, login, func(ctx context.Context) (*github.Avatar, error) {
		ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
		defer cancel()

		return s.fetcher.FetchLoginAvatar(ctx, login)
	})
	if err != nil {
		if ctx.Err() == nil {
			s.failedAt(login, s.clock.Now())
			s.logger.DebugContext(ctx, "download a profile picture", slog.Any("error", err))
		}

		return nil
	}

	return picture
}

// keep stores the pictures got, downloaded now, stamps the other shown ones (keys) as shown
// now, and prunes, in one transaction.
func (s *Service) keep(ctx context.Context, got map[string]*github.Avatar, keys []string, now time.Time) error {
	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for login, picture := range got {
			err := q.UpsertAvatar(ctx, queries.UpsertAvatarParams{
				Login: login, Mime: picture.ContentType, Image: picture.Data, Bytes: int64(len(picture.Data)),
				FetchedAt: now, UsedAt: now,
			})
			if err != nil {
				return fmt.Errorf("keep a profile picture: %w", err)
			}
		}

		err := q.TouchAvatars(ctx, queries.TouchAvatarsParams{UsedAt: now, Before: now.Add(-touchAfter), Logins: keys})
		if err != nil {
			return fmt.Errorf("stamp the profile pictures: %w", err)
		}

		return prune(ctx, q, now)
	})
}

// prune drops the pictures unused for unusedFor, then, from the most recently shown down,
// each one that would take them past maxKept or maxKeptBytes.
func prune(ctx context.Context, q *queries.Queries, now time.Time) error {
	if _, err := q.PruneUnusedAvatars(ctx, now.Add(-unusedFor)); err != nil {
		return fmt.Errorf("prune the profile pictures: %w", err)
	}

	rows, err := q.ListAvatarUsage(ctx)
	if err != nil {
		return fmt.Errorf("prune the profile pictures: %w", err)
	}

	var (
		count int
		total int64
	)

	for _, r := range rows {
		count++
		total += r.Bytes

		if count <= maxKept && total <= maxKeptBytes {
			continue
		}

		if err = q.DeleteAvatar(ctx, r.Login); err != nil {
			return fmt.Errorf("prune the profile pictures: %w", err)
		}

		// What is dropped no longer counts toward the caps.
		count--
		total -= r.Bytes
	}

	return nil
}
