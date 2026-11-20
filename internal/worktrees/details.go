package worktrees

import (
	"context"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// detailLimit is how many changed files, and how many commits not on GitHub, Details lists
// at most.
const detailLimit = 200

// Details is what a worktree's drawer shows beyond its row, read when the drawer first
// opens. Every field is a plain value.
type Details struct {
	// Files are the worktree's changed and untracked files as its status lists them now, by
	// path, at most 200; none when it has none, or when they couldn't be read. They replace
	// the row's (Worktree.Files), which are as the listing found them.
	Files []FileEntry `json:"files"`

	// FileCount is how many changed and untracked files the status found, or -1 when it
	// couldn't be read.
	FileCount int `json:"fileCount"`

	// Commits are the commits on HEAD that aren't on GitHub, as Worktree.Unpushed counts
	// them, newest first, at most 200; none when every one is, or when they couldn't be
	// read.
	Commits []UnpushedCommit `json:"commits"`

	// Unpushed is how many such commits there are, or -1 when they couldn't be counted.
	Unpushed int `json:"unpushed"`

	// Copies says how comparing those commits with the pushed branches went, now: whether
	// it ran, against what, and what it found. Its zero value is for a worktree with none of
	// them, or whose commits couldn't be read.
	Copies CopyComparison `json:"copies"`

	// Base is origin's default branch as the clone last fetched it, as in "origin/main", or
	// "" when the clone doesn't say.
	Base string `json:"base"`

	// Behind is how many commits Base has that HEAD doesn't, or -1 when that is unknown.
	Behind int `json:"behind"`

	// CreatedAt is when the app made or first recorded the worktree, in RFC 3339, or "" for
	// one it has no record of.
	CreatedAt string `json:"createdAt"`

	// Errors say, each in a sentence, what couldn't be read and why; none when every read
	// worked. The fields it would have filled keep their empty values.
	Errors []string `json:"errors"`
}

// CopyComparison is how comparing a worktree's commits not on GitHub with the pushed
// branches went when Details compared them, or found what the listing compared still
// holds. It reads no copy record, so a worktree its record vouches for
// (Worktree.Copies.Recorded) may have nothing on a pushed branch now. Every field is a plain
// value.
type CopyComparison struct {
	// Compared reports that the comparison ran to completion, the pushed commits read and
	// every branch holding likely copies compared, byte for byte, or that there was no pushed
	// branch to compare with. With Identical at 0, none of the commits has an identical copy
	// on a pushed branch now; false says only that this is unknown (Note says why).
	Compared bool `json:"compared"`

	// Ref is the pushed branch they were compared with, as in origin/feature-x, or "" when
	// no pushed commit was a likely copy of any.
	Ref string `json:"ref"`

	// Branch is Ref's branch on its remote, as Copies.Branch says it.
	Branch string `json:"branch"`

	// Identical and Edited count the commits with an identical copy on Ref, and those with
	// a copy there whose change differs.
	Identical int `json:"identical"`
	Edited    int `json:"edited"`

	// Unless it is "", Note says in a sentence why the comparison couldn't run, or why
	// identical copies on several branches don't count.
	Note string `json:"note"`
}

// comparisonOf is how a comparison that found r went.
func comparisonOf(r copyResult) CopyComparison {
	return CopyComparison{
		Compared: r.compared, Ref: r.copies.Ref, Branch: r.copies.Branch, Identical: r.copies.Identical,
		Edited: r.copies.Edited, Note: r.copies.Note,
	}
}

// UnpushedCommit is a commit on a worktree's HEAD that isn't on GitHub.
type UnpushedCommit struct {
	// OID is its commit ID.
	OID string `json:"oid"`

	// Unix is its commit time, in Unix seconds.
	Unix int64 `json:"unix"`

	// Subject is the first line of its message.
	Subject string `json:"subject"`

	// Copy is how it compares with the pushed branch the worktree's commits were compared
	// with (Worktree.Copies.Ref): an identical copy there, an edited one, or none ("").
	Copy CopyMark `json:"copy"`

	// CopyOID is that copy's commit ID, or "".
	CopyOID string `json:"copyOid"`
}

// BaseReader reads where a clone's default branch stands, as the clone last fetched it,
// and how far a commit is behind it, from the clone's own refs, never asking a remote:
// gitread's OriginHead, TrackingBranch, and CommitsBetween, *gitread.Git.
type BaseReader interface {
	OriginHead(ctx context.Context, dir string) (string, error)
	TrackingBranch(ctx context.Context, dir, branch string) (string, error)
	CommitsBetween(ctx context.Context, dir, from, to string) (int, error)
}

// Details reads what the drawer of the worktree at path, one of the linked worktrees of
// the clone at repoPath, shows beyond its row: its changed files, the commits on it that
// aren't on GitHub, how far it is behind origin's default branch, and when the app made
// it. It reads the status and counts the commits not on GitHub every time, so the drawer
// shows the worktree as it is now, and lists those commits only when the count is over 0.
// It reads the base in steps, each only once the one before found what it needs. These
// three run side by side. A read that fails is a sentence in Errors, not an error. Details
// fails only when the request can't be checked (git or the settings can't be found, or
// git can't list the clone's worktrees), when it isn't about a linked worktree, whose
// folder is there, of a clone the repository locations hold, or when ctx ends. It reads
// the clone's git and the worktree records alone, never GitHub, takes no lock, and emits
// no event.
func (s *Service) Details(ctx context.Context, repoPath, path string) (Details, error) {
	g, e, err := s.linkedEntry(ctx, repoPath, path)
	if err != nil {
		return Details{}, err
	}

	record := recordsByDir(s.records(ctx))[paths.Resolved(e.Path)]

	fetched := ""
	if gitread.IsCommitID(record.FetchedHead) {
		fetched = record.FetchedHead
	}

	d := Details{Errors: []string{}}

	if !record.CreatedAt.IsZero() {
		d.CreatedAt = clock.Format(record.CreatedAt)
	}

	var (
		wg                                     sync.WaitGroup
		filesFailed, commitsFailed, baseFailed string
	)

	wg.Go(func() { d.Files, d.FileCount, filesFailed = detailFiles(ctx, g, e.Path) })
	wg.Go(func() { d.Commits, d.Unpushed, d.Copies, commitsFailed = s.detailCommits(ctx, g, repoPath, e, fetched) })
	wg.Go(func() { d.Base, d.Behind, baseFailed = s.detailBase(ctx, repoPath, e.HEAD) })
	wg.Wait()

	if err = ctx.Err(); err != nil {
		return Details{}, err
	}

	for _, failed := range []string{filesFailed, commitsFailed, baseFailed} {
		if failed != "" {
			d.Errors = append(d.Errors, failed)
		}
	}

	return d, nil
}

// linkedEntry checks a request about one worktree, which must be a linked worktree of a
// clone the repository locations hold, whose folder is there, and returns its entry, as
// git lists it now.
func (s *Service) linkedEntry(ctx context.Context, repoPath, path string) (git, Entry, error) {
	g, err := s.checkedGit(ctx, repoPath)
	if err != nil {
		return git{}, Entry{}, err
	}

	e, err := s.entryOf(ctx, g, repoPath, path)
	if err != nil {
		return git{}, Entry{}, err
	}

	if e.Prunable || !dirExists(e.Path) {
		return git{}, Entry{}, ErrPrunable
	}

	return g, e, nil
}

// detailFiles reads the changed files of the worktree at dir as they are now: the first by
// path, at most detailLimit, and how many there are; or why they couldn't be read, with -1.
func detailFiles(ctx context.Context, g git, dir string) ([]FileEntry, int, string) {
	count, files, stderr, err := g.firstChanged(ctx, dir, detailLimit)
	if err != nil {
		return []FileEntry{}, -1, readFailure("the changed files", err, stderr)
	}

	return files, count, ""
}

// detailCommits counts the commits on HEAD in the worktree e of the clone at dir that aren't
// on GitHub, past fetched, the commit it was made at from GitHub, when it is one, and lists
// them, at most detailLimit, only when there are any, each with how it compares with the
// pushed branches, and how that comparison went (copiesNow); or says why they couldn't be
// read.
func (s *Service) detailCommits(ctx context.Context, g git, dir string, e Entry, fetched string,
) ([]UnpushedCommit, int, CopyComparison, string) {
	count, stderr, err := g.unpushed(ctx, e.Path, fetched)

	switch {
	case err != nil:
		return []UnpushedCommit{}, -1, CopyComparison{}, readFailure("the commits not on GitHub", err, stderr)
	case count == 0:
		return []UnpushedCommit{}, 0, CopyComparison{}, ""
	}

	commits, stderr, err := g.unpushedCommits(ctx, e.Path, fetched)
	if err != nil {
		return []UnpushedCommit{}, count, CopyComparison{}, readFailure("the commits not on GitHub", err, stderr)
	}

	compared := s.copiesNow(ctx, g, dir, e, fetched, count)

	for i := range commits {
		if m, ok := compared.marks[commits[i].OID]; ok {
			commits[i].Copy, commits[i].CopyOID = m.mark, m.copy
		}
	}

	return commits, count, comparisonOf(compared), ""
}

// copiesNow is how the count commits not on GitHub of the worktree e, of the clone at dir,
// compare with the pushed branches now: what the listing found, when the clone's refs,
// read once, say nothing it was worked out from changed since, or else what a comparison of
// its own finds (see copies.go). It is a result that wasn't compared, with why, when that
// can't be told.
func (s *Service) copiesNow(ctx context.Context, g git, dir string, e Entry, fetched string, count int) copyResult {
	if count > copyLimit {
		return uncompared(tooManyToCompare())
	}

	refs, err := g.readRefs(ctx, dir)
	if err != nil {
		return uncompared(compareFailure(err))
	}

	if kept, ok := s.keptCopies(e, fetched, refs, count); ok {
		return kept
	}

	clone, err := localrepos.Inspect(ctx, dir, s.run)
	if err != nil {
		return uncompared(compareFailure(err))
	}

	wt := &Worktree{Path: e.Path, Head: e.HEAD, Unpushed: count, fetched: fetched}
	s.detectCloneCopies(ctx, g, dir, []*Worktree{wt}, clone.Remotes, withOwners(refs, clone.Names), false)

	if wt.copies.result == nil {
		return uncompared(compareFailure(nil))
	}

	return *wt.copies.result
}

// uncompared is a result of a comparison that couldn't be made, with why.
func uncompared(note string) copyResult {
	return copyResult{copies: Copies{Note: note}, marks: map[string]commitMark{}}
}

// detailBase reads origin's default branch in the clone at dir, as it last fetched it, and
// how many commits it has that head doesn't, each only once the step before it found what
// it needs: -1 when that is unknown. Without a BaseReader, neither is known.
func (s *Service) detailBase(ctx context.Context, dir, head string) (string, int, string) {
	if s.base == nil {
		return "", -1, ""
	}

	branch, err := s.base.OriginHead(ctx, dir)

	switch {
	case err != nil:
		return "", -1, baseFailure(originRemote+"'s default branch", err)
	case branch == "":
		return "", -1, ""
	}

	base := originRemote + "/" + branch

	oid, err := s.base.TrackingBranch(ctx, dir, branch)

	switch {
	case err != nil:
		return base, -1, baseFailure("where "+base+" is", err)
	case oid == "" || !gitread.IsCommitID(head):
		return base, -1, ""
	}

	behind, err := s.base.CommitsBetween(ctx, dir, head, oid)
	if err != nil {
		return base, -1, baseFailure("how far behind "+base+" the worktree is", err)
	}

	return base, behind, ""
}

// keptCopies returns what the listing found of the worktree e's count commits not on
// GitHub, when nothing it was worked out from changed since: its HEAD, the commit it was
// made at from GitHub, and its clone's remote-tracking branches, as refs has them now.
func (s *Service) keptCopies(e Entry, fetched string, refs cloneRefs, count int) (copyResult, bool) {
	kept, ok := s.copies.entry(paths.Resolved(e.Path))
	if !ok || kept.key.head != e.HEAD || kept.key.fetched != fetched || kept.key.refs != refs.fingerprint() {
		return copyResult{}, false
	}

	return kept.result, kept.result.copies.Count == 0 || kept.result.copies.Count == count
}

// readFailure words a read Details couldn't make with the service's own git, for the
// window: what couldn't be read, then why, in git's own words, or in the guards' for a
// command they refused before git ran.
func readFailure(what string, err error, stderr []byte) string {
	why := uierr.Message(err)
	if line := execpath.GitMessage(stderr); line != "" {
		why = textutil.Sentence(line)
	}

	return couldntRead(what, why)
}

// baseFailure words a read of the base Details couldn't make, for the window, in
// gitread's words.
func baseFailure(what string, err error) string {
	return couldntRead(what, textutil.Sentence(gitread.Message(err)))
}

// couldntRead says that what couldn't be read, and then why, a sentence, when there is one.
func couldntRead(what, why string) string {
	return strings.TrimSpace("Couldn't read " + what + ". " + why)
}
