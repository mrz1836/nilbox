package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/trust"
)

// Bounds on the file checks a refresh runs. A check lists one pull request's files with
// a few REST requests, and only runs when the pull request's head commit moved, so most
// refreshes run none; these keep a refresh quick when many arrive at once, such as the
// first refresh after collaborators were added to the scope. The rest wait for the next
// refresh, and show as not checked yet until then.
const (
	// maxFileChecks caps the pull requests one refresh checks, in inbox order.
	maxFileChecks = 20

	// fileChecksParallel is how many checks run at once.
	fileChecksParallel = 4

	// fileCheckTimeout bounds a refresh's checks together.
	fileCheckTimeout = 30 * time.Second

	// maxFileErrorRunes caps a failed check's stored message.
	maxFileErrorRunes = 300
)

// fileScope decides whose pull requests have their files checked, and matches the
// files against the user's categories.
type fileScope struct {
	cfg     sensitive.Config
	policy  trust.Policy
	viewer  string
	matcher *sensitive.Matcher
}

// newFileScope reads the scope from the stored status: the sensitive-file settings, the
// trusted bots, and the signed-in account.
func newFileScope(status map[string]string) fileScope {
	raw, stored := status[trust.PrefBots]
	cfg := sensitive.DecodeConfig(status)

	return fileScope{
		cfg:     cfg,
		policy:  trust.NewPolicy(trust.DecodeBots(raw, stored)),
		viewer:  status[prefViewer],
		matcher: sensitive.NewMatcher(cfg),
	}
}

// checks reports whether an item's files are checked: a pull request from outside the
// repository, or a collaborator's under that scope, that the signed-in user did not open.
func (f fileScope) checks(isPR bool, author trust.Author, crossRepo bool) bool {
	if !isPR {
		return false
	}

	mine := f.viewer != "" && strings.EqualFold(author.Login, f.viewer)

	return sensitive.Checked(f.cfg.Scope, f.policy.Classify(author, true, crossRepo), author.Association, mine)
}

// filesStored is a stored check's head commit and outcome, with its item's node ID.
type filesStored = queries.ListStoredFilesRow

// fileCheck is the outcome of checking one pull request's files: the files, or why they
// could not be listed.
type fileCheck struct {
	files *github.ChangedFiles
	err   error
}

// fileTarget is a pull request a refresh checks.
type fileTarget struct {
	nodeID string
	ref    item.Ref
	head   string
}

// checkFiles lists the files of the pull requests in got whose files are checked and
// whose head commit moved since their last check (or whose last check failed), up to
// maxFileChecks of them, a few at a time. It returns each one's outcome by node ID. A
// check that fails is an outcome, not an error: the refresh stores it and goes on.
func (s *Service) checkFiles(ctx context.Context, tok github.Token, got refreshed) (map[string]fileCheck, error) {
	status, err := readStatus(ctx, s.prefs)
	if err != nil {
		return nil, err
	}

	// The checks stored are those of the account the last refresh read; the scope is
	// judged for the account this refresh read, which may be new.
	stored, err := s.q.ListStoredFiles(ctx, status[prefViewer])
	if err != nil {
		return nil, fmt.Errorf("read the checked files: %w", err)
	}

	status[prefViewer] = got.inbox.Viewer.Login

	targets := fileTargets(newFileScope(status), got, stored)
	if len(targets) == 0 {
		return map[string]fileCheck{}, nil
	}

	return s.runFileChecks(ctx, tok, targets)
}

// fileTargets picks the pull requests a refresh checks, in inbox order.
func fileTargets(scope fileScope, got refreshed, stored []filesStored) []fileTarget {
	last := make(map[string]filesStored, len(stored))
	for _, st := range stored {
		last[st.NodeID] = st
	}

	var targets []fileTarget

	for _, e := range mergeLists(got.inbox) {
		t, ok := scope.due(e.item, got.details.Items[e.item.NodeID].HeadOID, last)
		if !ok {
			continue
		}

		if targets = append(targets, t); len(targets) == maxFileChecks {
			break
		}
	}

	return targets
}

// due returns the check of an item whose head commit is now head, and whether one is
// due: its files are checked, and its last check (in last, by node ID) failed, is for
// another head, or is missing.
func (f fileScope) due(it github.Item, head string, last map[string]filesStored) (fileTarget, bool) {
	author := trust.Author{Login: it.AuthorLogin, Type: it.AuthorType, Association: it.AuthorAssociation}

	if it.NodeID == "" || head == "" || !f.checks(it.IsPR, author, it.IsCrossRepository) {
		return fileTarget{}, false
	}

	if st, ok := last[it.NodeID]; ok && st.Error == "" && strings.EqualFold(st.HeadOID, head) {
		return fileTarget{}, false
	}

	return fileTarget{
		nodeID: it.NodeID, ref: it.Ref(), head: head,
	}, true
}

// runFileChecks checks targets a few at a time, within fileCheckTimeout. It fails only
// when ctx itself ends.
func (s *Service) runFileChecks(ctx context.Context, tok github.Token, targets []fileTarget) (map[string]fileCheck, error) {
	checkCtx, cancel := context.WithTimeout(ctx, fileCheckTimeout)
	defer cancel()

	outcomes := make([]fileCheck, len(targets))

	var g errgroup.Group

	g.SetLimit(fileChecksParallel)

	for i, t := range targets {
		g.Go(func() error {
			files, err := s.client.FetchChangedFiles(checkCtx, tok, t.ref, t.head)
			outcomes[i] = fileCheck{files: files, err: err}

			return nil
		})
	}

	_ = g.Wait() // every check reports its own outcome

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out := make(map[string]fileCheck, len(targets))
	for i, t := range targets {
		out[t.nodeID] = outcomes[i]
	}

	return out, nil
}

// saveFiles stores the outcome of an item's file check, when this refresh ran one.
func (w *writer) saveFiles(ctx context.Context, id int64, it github.Item) error {
	check, ok := w.files[it.NodeID]
	if !ok {
		return nil
	}

	if err := upsertFiles(ctx, w.q, newFilesRow(id, check, w.now)); err != nil {
		return fmt.Errorf("save the files of %s: %w", it.Ref(), err)
	}

	return nil
}

// pruneFiles removes the checks of items no longer in any list.
func (w *writer) pruneFiles(ctx context.Context) error {
	if err := w.q.PruneFiles(ctx, w.accountID); err != nil {
		return fmt.Errorf("remove the files of items no longer listed: %w", err)
	}

	return nil
}

// storedFile is one changed file as the files column of item_files stores it: its path
// after the change, a renamed or copied file's path before it, and GitHub's word for the
// change.
type storedFile struct {
	Path     string `json:"path"`
	Previous string `json:"previous,omitempty"`
	Change   string `json:"change"`
}

// filesRow is one item's file check as item_files stores it. A failed check keeps its
// message in err and no files.
type filesRow struct {
	itemID    int64
	head      string
	total     int
	files     []storedFile
	complete  bool
	err       string
	checkedAt time.Time
}

// newFilesRow converts a check's outcome to its row. A failed check keeps its message
// and no files, so the next refresh tries again.
func newFilesRow(itemID int64, check fileCheck, now time.Time) filesRow {
	row := filesRow{itemID: itemID, checkedAt: now}

	if check.err != nil || check.files == nil {
		row.err = fileErrorMessage(check.err)

		return row
	}

	files := make([]storedFile, 0, len(check.files.Files))
	for _, f := range check.files.Files {
		files = append(files, storedFile{Path: f.Path, Previous: f.PreviousPath, Change: f.Status})
	}

	row.head, row.total, row.files, row.complete = check.files.Head, check.files.Total, files, check.files.Complete

	return row
}

// result returns the check as the sensitive package reads it.
func (r filesRow) result() *sensitive.Result {
	files := make([]sensitive.File, 0, len(r.files))
	for _, f := range r.files {
		files = append(files, sensitive.File{Path: f.Path, PreviousPath: f.Previous, Change: f.Change})
	}

	return &sensitive.Result{
		Head: r.head, Total: r.total, Files: files, Complete: r.complete, Error: r.err, CheckedAt: r.checkedAt,
	}
}

// upsertFiles stores row through q, replacing the item's earlier check.
func upsertFiles(ctx context.Context, q *queries.Queries, row filesRow) error {
	files, err := store.EncodeJSON(row.files)
	if err != nil {
		return err
	}

	return q.UpsertFiles(ctx, queries.UpsertFilesParams{
		ItemID: row.itemID, HeadOID: row.head, Total: int64(row.total), Files: files, Complete: row.complete,
		Error: row.err, CheckedAt: row.checkedAt,
	})
}

// fileErrorMessage is what a failed check stores: why it failed, briefly.
func fileErrorMessage(err error) string {
	switch {
	case err == nil:
		return "GitHub did not list the files"
	case errors.Is(err, context.DeadlineExceeded):
		return "GitHub took too long to list the files"
	}

	msg := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(msg); len(r) > maxFileErrorRunes {
		msg = string(r[:maxFileErrorRunes]) + "…"
	}

	return msg
}

// filesColumns are an item's last file check as ListItems and GetCheckItem read it, from
// a LEFT JOIN: every column is NULL when the item has no check.
type filesColumns struct {
	itemID    *int64
	head      *string
	total     *int64
	files     *string
	complete  *bool
	err       *string
	checkedAt *time.Time
}

// result decodes the stored check, or returns nil when there is none.
func (c filesColumns) result() (*sensitive.Result, error) {
	if c.itemID == nil {
		return nil, nil //nolint:nilnil // no check is not an error, and there is no result
	}

	files, err := store.DecodeJSON[[]storedFile](value(c.files))
	if err != nil {
		return nil, fmt.Errorf("read the checked files: %w", err)
	}

	return filesRow{
		itemID: *c.itemID, head: value(c.head), total: int(value(c.total)), files: files,
		complete: value(c.complete), err: value(c.err), checkedAt: value(c.checkedAt),
	}.result(), nil
}

// value returns what p points to, or T's zero value when p is nil: the value a column
// from a LEFT JOIN takes when the joined row is missing.
func value[T any](p *T) T {
	if p == nil {
		var zero T

		return zero
	}

	return *p
}

// checkItemRow is a stored item as checking its files reads it.
type checkItemRow struct {
	ID                int64
	Kind              item.Kind
	Number            int
	URL               string
	AuthorLogin       string
	AuthorType        string
	AuthorAssociation string
	IsCrossRepository bool
	RepoOwner         string
	RepoName          string

	// HeadOID is the head commit the last refresh read, or "" before one has.
	HeadOID string

	// Stored is the item's last file check, or nil when there is none.
	Stored *sensitive.Result
}

// checkItemRowOf converts a GetCheckItem row.
func checkItemRowOf(r queries.GetCheckItemRow) (checkItemRow, error) {
	stored, err := filesColumns{
		itemID: r.FilesItemID, head: r.FilesHeadOID, total: r.FilesTotal, files: r.Files,
		complete: r.FilesComplete, err: r.FilesError, checkedAt: r.FilesCheckedAt,
	}.result()
	if err != nil {
		return checkItemRow{}, err
	}

	return checkItemRow{
		ID: r.ID, Kind: item.Kind(r.Kind), Number: int(r.Number), URL: r.URL, AuthorLogin: r.AuthorLogin,
		AuthorType: r.AuthorType, AuthorAssociation: r.AuthorAssociation, IsCrossRepository: r.IsCrossRepository,
		RepoOwner: r.RepoOwner, RepoName: r.RepoName, HeadOID: value(r.HeadOID), Stored: stored,
	}, nil
}

// checked reports whether the row's files are checked under scope.
func (r checkItemRow) checked(scope fileScope) bool {
	author := trust.Author{Login: r.AuthorLogin, Type: r.AuthorType, Association: r.AuthorAssociation}

	return scope.checks(r.Kind == item.KindPR, author, r.IsCrossRepository)
}

// StoredFiles returns the last check of an item's changed files, without asking GitHub,
// with the user's categories applied: StatusPending when the item was pushed since. It
// returns sensitive.ErrNotChecked for an item whose files are not checked: an issue, or
// a pull request from the repository's own people, a trusted bot, or the signed-in user.
func (s *Service) StoredFiles(ctx context.Context, itemID int64) (*sensitive.Check, error) {
	row, scope, err := s.loadCheckItem(ctx, itemID)
	if err != nil {
		return nil, err
	}

	if !row.checked(scope) {
		return nil, sensitive.ErrNotChecked
	}

	check := scope.matcher.Evaluate(row.Stored, row.HeadOID, row.URL)

	return &check, nil
}

// CheckFiles checks an item's changed files against GitHub as of now, stores the
// outcome, and returns it with the user's categories applied, for a launch to decide on.
// It asks GitHub for the pull request's head first and lists the files again only when
// the head moved since the stored check, so a launch right after a refresh costs one
// request. It returns sensitive.ErrNotChecked for an item whose files are not checked
// (see StoredFiles).
//
// A check that cannot finish is returned with StatusFailed and its reason, never as an
// error, so the caller can hold back on it; an error means the item could not be read.
func (s *Service) CheckFiles(ctx context.Context, itemID int64) (*sensitive.Check, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row, scope, err := s.loadCheckItem(ctx, itemID)
	if err != nil {
		return nil, err
	}

	if !row.checked(scope) {
		return nil, sensitive.ErrNotChecked
	}

	check := s.checkItemFiles(ctx, row)
	if err = ctx.Err(); err != nil {
		return nil, err
	}

	stored := newFilesRow(row.ID, check, s.now())
	if err = upsertFiles(ctx, s.q, stored); err != nil {
		return nil, fmt.Errorf("save the checked files: %w", err)
	}

	s.notify()

	evaluated := scope.matcher.Evaluate(stored.result(), stored.head, row.URL)

	return &evaluated, nil
}

// checkItemFiles reads one pull request's head and, when it moved since the stored check
// or that check failed, its files.
func (s *Service) checkItemFiles(ctx context.Context, row checkItemRow) fileCheck {
	tok, err := s.tokens.Token(ctx)
	if err != nil {
		return fileCheck{err: err}
	}

	ref := item.Ref{Repo: repo.Name{Owner: row.RepoOwner, Repo: row.RepoName}, Number: row.Number, Kind: row.Kind}

	pull, err := s.client.FetchPullHead(ctx, tok, ref)
	if err != nil {
		return fileCheck{err: github.Redact(err, tok)}
	}

	if prev := row.Stored; prev != nil && prev.Error == "" && strings.EqualFold(prev.Head, pull.Head) {
		return fileCheck{files: storedChangedFiles(prev)}
	}

	files, err := s.client.FetchChangedFiles(ctx, tok, ref, pull.Head)

	return fileCheck{files: files, err: github.Redact(err, tok)}
}

// storedChangedFiles turns a stored check back into the list GitHub returned.
func storedChangedFiles(r *sensitive.Result) *github.ChangedFiles {
	files := make([]github.ChangedFile, 0, len(r.Files))
	for _, f := range r.Files {
		files = append(files, github.ChangedFile{Path: f.Path, PreviousPath: f.PreviousPath, Status: f.Change})
	}

	return &github.ChangedFiles{Head: r.Head, Total: r.Total, Files: files, Complete: r.Complete}
}

// loadCheckItem reads a stored item with its last check, and the scope it is judged by.
func (s *Service) loadCheckItem(ctx context.Context, itemID int64) (checkItemRow, fileScope, error) {
	status, err := readStatus(ctx, s.prefs)
	if err != nil {
		return checkItemRow{}, fileScope{}, err
	}

	stored, err := s.q.GetCheckItem(ctx, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return checkItemRow{}, fileScope{}, fmt.Errorf("%w (id %d)", item.ErrNotFound, itemID)
	}

	if err != nil {
		return checkItemRow{}, fileScope{}, fmt.Errorf("read the item: %w", err)
	}

	row, err := checkItemRowOf(stored)
	if err != nil {
		return checkItemRow{}, fileScope{}, fmt.Errorf("read the item: %w", err)
	}

	return row, newFileScope(status), nil
}

// checkUpdatedFiles checks the files of an item one refresh of its own read, when they
// are checked and its head moved since their last check. It returns no outcome when no
// check was due.
func (s *Service) checkUpdatedFiles(
	ctx context.Context, tok github.Token, id int64, update *github.ItemUpdate,
) (map[string]fileCheck, error) {
	if update == nil || !update.Found {
		return map[string]fileCheck{}, nil
	}

	row, scope, err := s.loadCheckItem(ctx, id)
	if err != nil {
		return nil, err
	}

	last := map[string]filesStored{}
	if row.Stored != nil {
		last[update.Item.NodeID] = filesStored{HeadOID: row.Stored.Head, Error: row.Stored.Error}
	}

	t, ok := scope.due(update.Item, update.Details.HeadOID, last)
	if !ok {
		return map[string]fileCheck{}, nil
	}

	return s.runFileChecks(ctx, tok, []fileTarget{t})
}
