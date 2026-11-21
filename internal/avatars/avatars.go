// Package avatars keeps the profile pictures the sheets and the inbox's cards show: the
// people's, by login, and the bots'. A person's is downloaded from GitHub's avatar host by
// the login alone (github's FetchLoginAvatar), a bot's by the URL GitHub gave for it on the
// same host (FetchAvatar), since the host answers a bot's login with a generated pattern
// rather than its app's picture. Neither is an API request, so neither sends a token or
// spends a point of the budget. Each is kept in the avatars table, which this package owns,
// and downloaded again only once it is a week old and shown again. The window gets them as
// data: URLs, so it loads nothing remote itself.
package avatars

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"maps"
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
	// MaxLogins is the most logins one call looks at, people and bots together; the rest
	// get no picture.
	MaxLogins = 100

	// botSuffix ends the key a bot's picture is kept and answered by, after its login:
	// GitHub's GraphQL names a bot by its app's slug, which a person could have as a login,
	// and no person's login can hold "[".
	botSuffix = "[bot]"

	// refreshAfter is how old a kept picture is downloaded again at, the next time it is
	// shown: a login's address names no version, so a changed picture shows only then.
	refreshAfter = 7 * 24 * time.Hour

	// touchAfter is how long a kept picture's last showing stands before it is stamped
	// again, so a picture shown often is written at most once a day.
	touchAfter = 24 * time.Hour

	// unusedFor is how long a picture nothing shows is kept, and maxKept and maxKeptBytes
	// cap them all: the least recently shown past either are dropped.
	unusedFor    = 30 * 24 * time.Hour
	maxKept      = 2000
	maxKeptBytes = 16 << 20

	// retryAfter is how long a picture whose download failed goes without another; and
	// maxFailed how many such pictures are remembered before all are forgotten at once.
	retryAfter = 30 * time.Minute
	maxFailed  = 1000

	// downloads is how many run at once, and downloadTimeout how long each may take.
	downloads       = 4
	downloadTimeout = 15 * time.Second
)

// Fetcher downloads a picture: a person's by login, and a bot's by the URL GitHub gave
// for it, which it refuses off the avatar host. *github.Client implements it.
type Fetcher interface {
	FetchLoginAvatar(ctx context.Context, login string) (*github.Avatar, error)
	FetchAvatar(ctx context.Context, rawURL string) (*github.Avatar, error)
}

// Bot is a bot whose picture is asked for: its login as GitHub's GraphQL names it (its
// app's slug, without "[bot]"), and its picture's URL as GitHub gave it.
type Bot struct {
	Login string `json:"login"`
	URL   string `json:"url"`
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
	return s.PicturesFor(ctx, logins, nil)
}

// PicturesFor returns the pictures of people, by login, as Pictures does, and of bots, each
// downloaded from the URL given for it and keyed, kept and answered, by its login as given
// with "[bot]" after it, so a bot never takes the picture of a person whose login is its
// app's slug. A bot with no login or no URL is left out, and so is one whose URL the
// fetcher refuses (one off the avatar host), which isn't tried again for retryAfter, as any
// failed download. A bot's picture is kept, refreshed, and pruned as a person's is, and the
// first MaxLogins different people and bots together, the people first, are looked at.
func (s *Service) PicturesFor(ctx context.Context, people []string, bots []Bot) (map[string]string, error) {
	a := folded(people, bots)
	if len(a.keys) == 0 {
		return map[string]string{}, nil
	}

	now := s.clock.Now()

	have, err := s.read(ctx, a.keys, now)
	if err != nil {
		return nil, err
	}

	got := s.download(ctx, s.untried(have.due, now), a.urls)
	if err = ctx.Err(); err != nil {
		return nil, fmt.Errorf("read the profile pictures: %w", err)
	}

	if len(got) > 0 || have.stale {
		if err = s.keep(ctx, got, a.keys, now); err != nil {
			s.logger.WarnContext(ctx, "keep the profile pictures", slog.Any("error", err))
		}
	}

	maps.Copy(have.pictures, got)

	return named(a.asked, have.pictures), nil
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

// asking is what one call asks for: the names asked under each key (the lower-cased
// login, with botSuffix after a bot's, and a bot's name with botSuffix after it too), the
// keys in the order first seen, at most MaxLogins, and the URL of each bot's key.
type asking struct {
	asked map[string][]string
	keys  []string
	urls  map[string]string
}

// folded gathers what a call asks for, the people first, leaving out an empty login and a
// bot with no URL.
func folded(people []string, bots []Bot) asking {
	a := asking{asked: map[string][]string{}, urls: map[string]string{}}

	for _, login := range people {
		if login != "" {
			a.add(strings.ToLower(login), login)
		}
	}

	for _, bot := range bots {
		a.addBot(bot)
	}

	return a
}

// add asks for name under key, unless the call already looks at MaxLogins other keys, and
// reports whether it did.
func (a *asking) add(key, name string) bool {
	if _, ok := a.asked[key]; !ok {
		if len(a.keys) == MaxLogins {
			return false
		}

		a.keys = append(a.keys, key)
	}

	a.asked[key] = append(a.asked[key], name)

	return true
}

// addBot asks for bot's picture, keeping the first URL given for it, unless it has no login
// or no URL.
func (a *asking) addBot(bot Bot) {
	if bot.Login == "" || bot.URL == "" {
		return
	}

	key := strings.ToLower(bot.Login) + botSuffix
	if a.add(key, bot.Login+botSuffix) && a.urls[key] == "" {
		a.urls[key] = bot.URL
	}
}

// untried returns the keys whose download didn't fail within retryAfter of now,
// forgetting the failures that old.
func (s *Service) untried(keys []string, now time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := keys[:0:0]

	for _, key := range keys {
		if at, ok := s.failed[key]; ok {
			if now.Sub(at) < retryAfter {
				continue
			}

			delete(s.failed, key)
		}

		out = append(out, key)
	}

	return out
}

// failedAt remembers that key's download failed at.
func (s *Service) failedAt(key string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.failed) >= maxFailed {
		clear(s.failed)
	}

	s.failed[key] = at
}

// download downloads the keys' pictures, downloads at a time, a bot's from its URL in urls
// and a person's by login, and returns the ones it got. A download that fails is
// remembered (failedAt), unless ctx ended, which ends them all.
func (s *Service) download(ctx context.Context, keys []string, urls map[string]string) map[string]*github.Avatar {
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	out := map[string]*github.Avatar{}
	slots := make(chan struct{}, downloads)

	for _, key := range keys {
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}

			defer func() { <-slots }()

			if picture := s.downloadOne(ctx, key, urls[key]); picture != nil {
				mu.Lock()
				out[key] = picture
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	return out
}

// downloadOne downloads key's picture, from rawURL when it is a bot's and by the login
// otherwise, within downloadTimeout, once for every call that wants it at the time, and
// returns nil when that fails, remembering the failure (failedAt) unless ctx ended.
func (s *Service) downloadOne(ctx context.Context, key, rawURL string) *github.Avatar {
	picture, err := s.fetches.Do(ctx, key, func(ctx context.Context) (*github.Avatar, error) {
		ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
		defer cancel()

		if rawURL != "" {
			return s.fetcher.FetchAvatar(ctx, rawURL)
		}

		return s.fetcher.FetchLoginAvatar(ctx, key)
	})
	if err != nil {
		if ctx.Err() == nil {
			s.failedAt(key, s.clock.Now())
			s.logger.DebugContext(ctx, "download a profile picture", slog.Any("error", err))
		}

		return nil
	}

	return picture
}

// keep stores the pictures got, downloaded now, stamps the other shown ones (keys) as shown
// now, and prunes, in one transaction. A bot's is kept under its key, botSuffix and all.
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
