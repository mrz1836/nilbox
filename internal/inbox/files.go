package inbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	"gorm.io/gorm/clause"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/model"
	"github.com/mrz1836/nilbox/internal/sensitive"
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

// storedFilesSQL reads the head commit and outcome of each stored check, by the item's
// node ID.
const storedFilesSQL = `SELECT i.node_id, f.head_oid, f.error
FROM item_files f
JOIN items i ON i.id = f.item_id
WHERE i.account_id = (SELECT id FROM accounts WHERE login = (SELECT value FROM prefs WHERE key = ?))
  AND i.node_id <> ''`

// pruneFilesSQL removes the checks of an account's items that left every list.
const pruneFilesSQL = `DELETE FROM item_files WHERE item_id IN (
    SELECT id FROM items
    WHERE account_id = ? AND reason_review_requested = 0 AND reason_authored = 0 AND reason_assigned = 0
)`

// checkItemSQL reads what checking one stored item's files needs.
const checkItemSQL = `SELECT i.id, i.kind, i.number, i.url, i.author_login, i.author_type, i.author_association,
    i.is_cross_repository, r.owner AS repo_owner, r.name AS repo_name, COALESCE(d.head_oid, '') AS head_oid,
    f.item_id IS NOT NULL AS has_files, COALESCE(f.head_oid, '') AS files_head, COALESCE(f.total, 0) AS files_total,
    COALESCE(f.files, '[]') AS files, COALESCE(f.complete, 0) AS files_complete,
    COALESCE(f.error, '') AS files_error, f.checked_at AS files_checked_at
FROM items i
JOIN repos r ON r.id = i.repo_id
LEFT JOIN item_details d ON d.item_id = i.id
LEFT JOIN item_files f ON f.item_id = i.id
WHERE i.id = ?`

// fileScope decides whose pull requests have their files checked, and matches the
// files against the user's categories.
type fileScope struct {
	cfg     sensitive.Config
	policy  trust.Policy
	viewer  string
	matcher *sensitive.Matcher
}

// newFileScope reads the scope from the stored prefs: the sensitive-file settings, the
// trusted bots, and the signed-in account.
func newFileScope(prefs map[string]string) fileScope {
	raw, stored := prefs[trust.PrefBots]
	cfg := sensitive.DecodeConfig(prefs)

	return fileScope{
		cfg:     cfg,
		policy:  trust.NewPolicy(trust.DecodeBots(raw, stored)),
		viewer:  prefs[prefViewer],
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

// filesStored is a stored check's head commit and outcome.
type filesStored struct {
	NodeID  string `gorm:"column:node_id"`
	HeadOID string `gorm:"column:head_oid"`
	Error   string `gorm:"column:error"`
}

// fileCheck is the outcome of checking one pull request's files: the files, or why they
// could not be listed.
type fileCheck struct {
	files *github.ChangedFiles
	err   error
}

// fileTarget is a pull request a refresh checks.
type fileTarget struct {
	nodeID string
	ref    github.ItemRef
	head   string
}

// checkFiles lists the files of the pull requests in got whose files are checked and
// whose head commit moved since their last check (or whose last check failed), up to
// maxFileChecks of them, a few at a time. It returns each one's outcome by node ID. A
// check that fails is an outcome, not an error: the refresh stores it and goes on.
func (s *Service) checkFiles(ctx context.Context, tok github.Token, got refreshed) (map[string]fileCheck, error) {
	db := s.db.WithContext(ctx)

	prefs, err := readPrefs(db)
	if err != nil {
		return nil, err
	}

	// The scope is judged for the account this refresh read, which may be new.
	prefs[prefViewer] = got.inbox.Viewer.Login

	var stored []filesStored
	if err = db.Raw(storedFilesSQL, prefViewer).Scan(&stored).Error; err != nil {
		return nil, fmt.Errorf("read the checked files: %w", err)
	}

	targets := fileTargets(newFileScope(prefs), got, stored)
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
		nodeID: it.NodeID, ref: github.ItemRef{Owner: it.RepoOwner, Name: it.RepoName, Number: it.Number}, head: head,
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
func (w *writer) saveFiles(id int64, it github.Item) error {
	check, ok := w.files[it.NodeID]
	if !ok {
		return nil
	}

	row := filesRow(id, check, w.now)

	err := w.tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "item_id"}}, UpdateAll: true}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("save the files of %s/%s#%d: %w", it.RepoOwner, it.RepoName, it.Number, err)
	}

	return nil
}

// pruneFiles removes the checks of items no longer in any list.
func (w *writer) pruneFiles() error {
	return w.exec("remove the files of items no longer listed", pruneFilesSQL, w.accountID)
}

// filesRow converts a check's outcome to its row. A failed check keeps its message and
// no files, so the next refresh tries again.
func filesRow(itemID int64, check fileCheck, now time.Time) model.ItemFiles {
	row := model.ItemFiles{ItemID: itemID, Files: model.ChangedFiles{}, CheckedAt: now}

	if check.err != nil || check.files == nil {
		row.Error = fileErrorMessage(check.err)

		return row
	}

	files := make(model.ChangedFiles, 0, len(check.files.Files))
	for _, f := range check.files.Files {
		files = append(files, model.ChangedFile{Path: f.Path, Previous: f.PreviousPath, Change: f.Status})
	}

	row.HeadOID, row.Total, row.Files, row.Complete = check.files.Head, check.files.Total, files, check.files.Complete

	return row
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

// filesColumns are a stored check, as loadItemsSQL and checkItemSQL read it.
type filesColumns struct {
	HasFiles       bool               `gorm:"column:has_files"`
	FilesHead      string             `gorm:"column:files_head"`
	FilesTotal     int                `gorm:"column:files_total"`
	Files          model.ChangedFiles `gorm:"column:files"`
	FilesComplete  bool               `gorm:"column:files_complete"`
	FilesError     string             `gorm:"column:files_error"`
	FilesCheckedAt *time.Time         `gorm:"column:files_checked_at"`
}

// result returns the stored check, or nil when there is none.
func (c filesColumns) result() *sensitive.Result {
	if !c.HasFiles {
		return nil
	}

	files := make([]sensitive.File, 0, len(c.Files))
	for _, f := range c.Files {
		files = append(files, sensitive.File{Path: f.Path, PreviousPath: f.Previous, Change: f.Change})
	}

	r := &sensitive.Result{
		Head: c.FilesHead, Total: c.FilesTotal, Files: files, Complete: c.FilesComplete, Error: c.FilesError,
	}

	if c.FilesCheckedAt != nil {
		r.CheckedAt = *c.FilesCheckedAt
	}

	return r
}

// checkItemRow is one row of checkItemSQL.
type checkItemRow struct {
	ID                int64  `gorm:"column:id"`
	Kind              string `gorm:"column:kind"`
	Number            int    `gorm:"column:number"`
	URL               string `gorm:"column:url"`
	AuthorLogin       string `gorm:"column:author_login"`
	AuthorType        string `gorm:"column:author_type"`
	AuthorAssociation string `gorm:"column:author_association"`
	IsCrossRepository bool   `gorm:"column:is_cross_repository"`
	RepoOwner         string `gorm:"column:repo_owner"`
	RepoName          string `gorm:"column:repo_name"`
	HeadOID           string `gorm:"column:head_oid"`

	// Stored is the item's last file check.
	Stored filesColumns `gorm:"embedded"`
}

// checked reports whether the row's files are checked under scope.
func (r checkItemRow) checked(scope fileScope) bool {
	author := trust.Author{Login: r.AuthorLogin, Type: r.AuthorType, Association: r.AuthorAssociation}

	return scope.checks(r.Kind == model.KindPR, author, r.IsCrossRepository)
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

	check := scope.matcher.Evaluate(row.Stored.result(), row.HeadOID, row.URL)

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

	stored := filesRow(row.ID, check, s.now())
	if err = s.storeFiles(ctx, stored); err != nil {
		return nil, err
	}

	s.notify()

	evaluated := scope.matcher.Evaluate(filesColumnsOf(stored).result(), stored.HeadOID, row.URL)

	return &evaluated, nil
}

// checkItemFiles reads one pull request's head and, when it moved since the stored check
// or that check failed, its files.
func (s *Service) checkItemFiles(ctx context.Context, row checkItemRow) fileCheck {
	tok, err := s.tokens.Token(ctx)
	if err != nil {
		return fileCheck{err: err}
	}

	ref := github.ItemRef{Owner: row.RepoOwner, Name: row.RepoName, Number: row.Number}

	pull, err := s.client.FetchPullHead(ctx, tok, ref)
	if err != nil {
		return fileCheck{err: github.Redact(err, tok)}
	}

	if prev := row.Stored.result(); prev != nil && prev.Error == "" && strings.EqualFold(prev.Head, pull.Head) {
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

// storeFiles stores one item's check outside a refresh.
func (s *Service) storeFiles(ctx context.Context, row model.ItemFiles) error {
	err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "item_id"}}, UpdateAll: true}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("save the checked files: %w", err)
	}

	return nil
}

// filesColumnsOf reads a row back as the columns a snapshot reads.
func filesColumnsOf(row model.ItemFiles) filesColumns {
	checkedAt := row.CheckedAt

	return filesColumns{
		HasFiles: true, FilesHead: row.HeadOID, FilesTotal: row.Total, Files: row.Files,
		FilesComplete: row.Complete, FilesError: row.Error, FilesCheckedAt: &checkedAt,
	}
}

// loadCheckItem reads a stored item with its last check, and the scope it is judged by.
func (s *Service) loadCheckItem(ctx context.Context, itemID int64) (checkItemRow, fileScope, error) {
	db := s.db.WithContext(ctx)

	prefs, err := readPrefs(db)
	if err != nil {
		return checkItemRow{}, fileScope{}, err
	}

	var rows []checkItemRow
	if err = db.Raw(checkItemSQL, itemID).Scan(&rows).Error; err != nil {
		return checkItemRow{}, fileScope{}, fmt.Errorf("read the item: %w", err)
	}

	if len(rows) == 0 {
		return checkItemRow{}, fileScope{}, fmt.Errorf("%w (id %d)", ErrItemNotFound, itemID)
	}

	return rows[0], newFileScope(prefs), nil
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
	if row.Stored.HasFiles {
		last[update.Item.NodeID] = filesStored{HeadOID: row.Stored.FilesHead, Error: row.Stored.FilesError}
	}

	t, ok := scope.due(update.Item, update.Details.HeadOID, last)
	if !ok {
		return map[string]fileCheck{}, nil
	}

	return s.runFileChecks(ctx, tok, []fileTarget{t})
}
