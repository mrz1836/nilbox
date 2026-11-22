package manifests

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// headRef names HEAD as a row's Tip, and originPrefix starts the Tip of origin's branch.
const (
	headRef      = "HEAD"
	originPrefix = "origin/"
)

// candidate is a clone to read: its folder, and the same resolved, which what is kept
// goes by; its origin's repository; and whether the user pinned it.
type candidate struct {
	dir    string
	key    string
	name   repo.Name
	pinned bool
}

// tip is the commit a clone is read at: the default branch, "" when none is known; what
// was read, as in "origin/main" or "HEAD"; the commit, "" for a HEAD with none; and, for
// HEAD, why.
type tip struct {
	branch string
	ref    string
	commit string
	atHead bool
	why    string
}

// contents is what a read of a clone at one commit found: its modules, stalest first, the
// tools it is set up for, its workspace files, and whether it held more modules than
// were read.
type contents struct {
	modules    []Module
	tools      []Tool
	workspaces []string
	truncated  bool
}

// keptClone is what a clone's last read found, kept in memory for the next: the commit it
// read and what was there, whether the clone was shallow, and where its FETCH_HEAD is,
// with when that was last written then.
type keptClone struct {
	commit    string
	contents  contents
	shallow   bool
	fetchHead string
	fetched   time.Time
}

// readClone reads the clone c: the commit to read, then what is there, from what its last
// read kept when that commit is unchanged. What it can't read, its row says.
func (s *Service) readClone(ctx context.Context, c candidate) Clone {
	row := Clone{
		Repo: c.name.String(), Dir: paths.Tilde(c.dir, s.home), Pinned: c.pinned, Modules: []Module{}, Tools: []Tool{},
		Workspaces: []string{}, key: c.name.Key(),
	}

	t, err := s.resolveTip(ctx, c)
	if err != nil {
		row.Error = s.cloneError(ctx, c, "Couldn't work out which commit of this clone to read", err)

		return row
	}

	row.Tip, row.Commit, row.AtHead, row.AtHeadWhy, row.DefaultBranch = t.ref, t.commit, t.atHead, t.why, t.branch

	if t.commit == "" {
		return row
	}

	k, err := s.contentsAt(ctx, c, t.commit)
	if err != nil {
		row.Error = s.cloneError(ctx, c, "Couldn't read this clone's files at "+t.ref, err)

		return row
	}

	row.Modules, row.Tools, row.Workspaces = k.contents.modules, k.contents.tools, k.contents.workspaces
	row.Truncated, row.Shallow, row.FetchedAt = k.contents.truncated, k.shallow, stamp(k.fetched)
	row.Counted, row.oldest = countedOf(row.Modules), oldestOf(row.Modules)
	row.Oldest = stamp(row.oldest)

	return row
}

// cloneError logs a clone's read that failed, and words it for the row: lead, and what
// git said.
func (s *Service) cloneError(ctx context.Context, c candidate, lead string, err error) string {
	s.logger.WarnContext(ctx, "couldn't read a clone's manifests", "repo", c.name.String(), "error", err)

	return uierr.Message(uierr.New(textutil.WithReason(lead, gitread.Message(err)), err))
}

// resolveTip works out the commit to read in the clone c: origin's default branch as the
// clone last fetched it, the default branch worked out as gitread.DefaultBranch does
// (the one the app stored, else the one origin's HEAD names, else the first of origin's
// main and master the clone has); or else HEAD, saying why.
func (s *Service) resolveTip(ctx context.Context, c candidate) (tip, error) {
	guessed := ""

	guess := func(branch string) bool {
		oid, err := s.git.TrackingCommit(ctx, c.dir, branch)
		guessed = oid

		return err == nil && oid != ""
	}

	branch := gitread.DefaultBranch(ctx, s.git, c.dir, s.storedDefault(ctx, c.name), guess)

	if branch != "" {
		commit, err := s.trackingCommit(ctx, c.dir, branch, guessed)
		if err != nil {
			return tip{}, err
		}

		if commit != "" {
			return tip{branch: branch, ref: originPrefix + branch, commit: commit}, nil
		}
	}

	head, err := s.git.Head(ctx, c.dir)
	if err != nil {
		return tip{}, err
	}

	return tip{branch: branch, ref: headRef, commit: head, atHead: true, why: whyAtHead(branch)}, nil
}

// trackingCommit returns the commit origin's branch named when the clone last fetched
// it: guessed, when the default branch's guess read it already, or else read now; "" when
// the clone has no such ref, or the branch is one gitread won't pass to git.
func (s *Service) trackingCommit(ctx context.Context, dir, branch, guessed string) (string, error) {
	if guessed != "" {
		return guessed, nil
	}

	commit, err := s.git.TrackingCommit(ctx, dir, branch)
	if errors.Is(err, gitread.ErrGitCommandNotAllowed) {
		return "", nil
	}

	return commit, err
}

// storedDefault is the default branch the app stored for the repository name, or "".
func (s *Service) storedDefault(ctx context.Context, name repo.Name) string {
	if s.branches == nil || !name.Valid() {
		return ""
	}

	def, err := s.branches.DefaultBranch(ctx, name)
	if err != nil {
		return ""
	}

	return def
}

// whyAtHead says why a clone was read at HEAD, its repository's default branch being
// branch, "" when none is known.
func whyAtHead(branch string) string {
	if branch == "" {
		return "No default branch is known for this repository, and this clone has no origin/main or origin/master, " +
			"so its HEAD was read."
	}

	return "This clone has no " + originPrefix + branch + ", so its HEAD was read: its GitHub remote may not be named " +
		"origin, or it hasn't fetched " + branch + " yet."
}

// contentsAt returns what is in the clone c at commit: what its last read kept, when that
// read the same commit, with whether the clone is shallow read again once it fetched
// since; or else a read of the commit, kept for the next scan unless a read in it failed
// in a way the next may not. Where FETCH_HEAD is, once read, is kept too, so an unchanged
// clone costs no git beyond what found its commit.
func (s *Service) contentsAt(ctx context.Context, c candidate, commit string) (keptClone, error) {
	prev, had := s.keptFor(c.key)

	fetchHead := prev.fetchHead
	if fetchHead == "" {
		fetchHead = s.fetchHead(ctx, c)
	}

	fetched := modTime(fetchHead)

	if had && prev.commit == commit {
		if !fetched.Equal(prev.fetched) {
			prev.fetchHead, prev.fetched, prev.shallow = fetchHead, fetched, s.shallow(ctx, c)
			s.keep(c.key, prev)
		}

		return prev, nil
	}

	read, settled, err := s.readContents(ctx, c.dir, commit)
	if err != nil {
		return keptClone{}, err
	}

	k := keptClone{commit: commit, contents: read, shallow: s.shallow(ctx, c), fetchHead: fetchHead, fetched: fetched}

	if settled {
		s.keep(c.key, k)
	}

	return k, nil
}

// readContents reads what Deps reads in the clone at dir at commit: its tree, each module
// the tree holds, stalest first, and the tools the clone is set up for. settled is false
// when a read in it failed in a way the next may not, as one the clone's time ran out on,
// which keeps the modules it read and says of each other one that it wasn't read; a tree
// that couldn't be read fails.
func (s *Service) readContents(ctx context.Context, dir, commit string) (contents, bool, error) {
	pick := newPicker()

	entries, err := s.git.Tree(ctx, dir, commit, pick.wanted)
	if err != nil {
		return contents{}, false, err
	}

	files := pick.files(entries)
	r := &moduleReader{git: s.git, dir: dir, commit: commit, files: files}
	modules := r.readAll(ctx, files.manifests)

	sortModules(modules)

	return contents{
		modules: modules, tools: toolsOf(files, modules), workspaces: files.workspaces, truncated: files.truncated,
	}, !r.unsettled.Load(), nil
}

// fetchHead returns where the clone c keeps FETCH_HEAD, or "" when git can't say.
func (s *Service) fetchHead(ctx context.Context, c candidate) string {
	file, err := s.git.FetchHead(ctx, c.dir)
	if err != nil {
		s.logger.DebugContext(ctx, "couldn't read where a clone keeps FETCH_HEAD", "repo", c.name.String(), "error", err)

		return ""
	}

	return file
}

// shallow reports whether the clone c is shallow; one that can't be read counts as not.
func (s *Service) shallow(ctx context.Context, c candidate) bool {
	yes, err := s.git.Shallow(ctx, c.dir)
	if err != nil {
		s.logger.DebugContext(ctx, "couldn't read whether a clone is shallow", "repo", c.name.String(), "error", err)

		return false
	}

	return yes
}

// modTime returns when the file at path was last written, or the zero time when there is
// none, or it can't be read. It looks at the file alone, never into it.
func modTime(path string) time.Time {
	if path == "" {
		return time.Time{}
	}

	info, err := os.Lstat(path)
	if err != nil {
		return time.Time{}
	}

	return info.ModTime()
}

// keptFor returns what the last read of the clone whose folder resolves to key kept, and
// whether it kept anything.
func (s *Service) keptFor(key string) (keptClone, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	k, ok := s.kept[key]

	return k, ok
}

// keep keeps k as what the read of the clone whose folder resolves to key found.
func (s *Service) keep(key string, k keptClone) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.kept[key] = k
}
