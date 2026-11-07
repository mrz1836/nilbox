package prompts

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// Triggers that make one kind of statement on one table fail, the way a full disk or a
// locked file would, so a test can reach the error a write reports.
const (
	refusePromptInserts = `CREATE TRIGGER refuse_prompt_inserts BEFORE INSERT ON prompts
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`
	refusePromptUpdates = `CREATE TRIGGER refuse_prompt_updates BEFORE UPDATE ON prompts
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`
	refusePromptDeletes = `CREATE TRIGGER refuse_prompt_deletes BEFORE DELETE ON prompts
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`
	refusePrefInserts = `CREATE TRIGGER refuse_pref_inserts BEFORE INSERT ON prefs
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`
)

// promptPrefs returns the preferences the prompt library keeps, by key.
func promptPrefs(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), "SELECT key, value FROM prefs WHERE key LIKE 'prompts.%'")
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	out := map[string]string{}

	for rows.Next() {
		var key, value string
		require.NoError(t, rows.Scan(&key, &value))
		out[key] = value
	}

	require.NoError(t, rows.Err())

	return out
}

func TestSeedRecordsWhatItAddedAndTheDefaults(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	review, work := byNickname(t, s, "Review"), byNickname(t, s, "Work on it")

	assert.Equal(t, map[string]string{
		"prompts.seeded":        `["review-pr","work-on-issue","quick-look","fix-checks"]`,
		"prompts.default.pr":    strconv.FormatInt(review.ID, 10),
		"prompts.default.issue": strconv.FormatInt(work.ID, 10),
	}, promptPrefs(t, db))
}

func TestSetDefaultStoresThePromptID(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	fix, work := byNickname(t, s, "Fix checks"), byNickname(t, s, "Work on it")

	require.NoError(t, s.SetDefault(t.Context(), string(item.KindPR), fix.ID))

	stored := promptPrefs(t, db)
	assert.Equal(t, strconv.FormatInt(fix.ID, 10), stored["prompts.default.pr"])
	assert.Equal(t, strconv.FormatInt(work.ID, 10), stored["prompts.default.issue"], "the other kind keeps its default")
}

func TestSeedRefusesAnUnreadableRecordOfStarters(t *testing.T) {
	t.Parallel()

	s, _ := empty(t, nil)
	require.NoError(t, s.store.prefs.Set(t.Context(), prefSeeded, "not json"))

	require.ErrorContains(t, Seed(t.Context(), s.store), "read the starter prompts added")

	all, err := s.store.All(t.Context())
	require.NoError(t, err)
	assert.Empty(t, all, "nothing is added")
}

func TestSeedAddsNothingWhenAWriteFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		trigger string
		want    string
	}{
		{name: "a starter cannot be added", trigger: refusePromptInserts, want: `add the "Review a pull request" starter prompt`},
		{name: "a default cannot be saved", trigger: refusePrefInserts, want: "save setting " + prefDefaultPR},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, db := empty(t, nil)
			exec(t, db, tt.trigger)

			require.ErrorContains(t, Seed(t.Context(), s.store), tt.want)

			all, err := s.store.All(t.Context())
			require.NoError(t, err)
			assert.Empty(t, all, "the starters added before the failure are rolled back")
			assert.Empty(t, promptPrefs(t, db))
		})
	}
}

func TestWritesReportWhatFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		trigger string
		run     func(t *testing.T, s *Service) error
		want    string
	}{
		{
			name: "create", trigger: refusePromptInserts, want: "save the prompt",
			run: func(t *testing.T, s *Service) error { _, err := s.Create(t.Context(), validInput()); return err },
		},
		{
			name: "duplicate", trigger: refusePromptInserts, want: "save the copy",
			run: func(t *testing.T, s *Service) error {
				_, err := s.Duplicate(t.Context(), byNickname(t, s, "Review").ID)
				return err
			},
		},
		{
			name: "update", trigger: refusePromptUpdates, want: "save the prompt",
			run: func(t *testing.T, s *Service) error {
				in := validInput()
				in.Nickname = "Fix checks"
				_, err := s.Update(t.Context(), byNickname(t, s, "Fix checks").ID, in)

				return err
			},
		},
		{
			name: "reset", trigger: refusePromptUpdates, want: "reset the prompt",
			run: func(t *testing.T, s *Service) error {
				_, err := s.Reset(t.Context(), byNickname(t, s, "Review").ID)
				return err
			},
		},
		{
			name: "delete", trigger: refusePromptDeletes, want: "delete the prompt",
			run: func(t *testing.T, s *Service) error { return s.Delete(t.Context(), byNickname(t, s, "Quick look").ID) },
		},
		{
			name: "set a default", trigger: refusePrefInserts, want: "save setting " + prefDefaultPR,
			run: func(t *testing.T, s *Service) error {
				return s.SetDefault(t.Context(), string(item.KindPR), byNickname(t, s, "Fix checks").ID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, db := seeded(t, nil)
			exec(t, db, tt.trigger)

			err := tt.run(t, s)
			require.ErrorContains(t, err, tt.want)
			require.NotErrorIs(t, err, ErrInvalidPrompt, "a failed write is not a validation problem")
		})
	}
}

func TestReadsReportAMissingTable(t *testing.T) {
	t.Parallel()

	// Each case gets the IDs of the Review and Quick look starters, looked up while every
	// table was still there.
	tests := []struct {
		name  string
		table string
		run   func(t *testing.T, s *Service, review, quick int64) error
		want  string
	}{
		{
			name: "list without the launch defaults", table: "launch_defaults", want: "read where the prompts are used",
			run: func(t *testing.T, s *Service, _, _ int64) error { _, err := s.List(t.Context()); return err },
		},
		{
			name: "update without the launch defaults", table: "launch_defaults", want: "read where the prompts are used",
			run: func(t *testing.T, s *Service, review, _ int64) error {
				_, err := s.Update(t.Context(), review, validInput())
				return err
			},
		},
		{
			// Only a prompt in use reads them, to say where: the review starter is the
			// default for pull requests.
			name: "delete without the launch defaults", table: "launch_defaults", want: "read where the prompts are used",
			run: func(t *testing.T, s *Service, review, _ int64) error { return s.Delete(t.Context(), review) },
		},
		{
			name: "reset without the launch defaults", table: "launch_defaults", want: "read where the prompts are used",
			run: func(t *testing.T, s *Service, review, _ int64) error {
				_, err := s.Reset(t.Context(), review)
				return err
			},
		},
		{
			name: "check without the launch defaults", table: "launch_defaults", want: "read where the prompts are used",
			run: func(t *testing.T, s *Service, review, _ int64) error {
				_, err := s.Check(t.Context(), CheckRequest{ID: review, Input: validInput()})
				return err
			},
		},
		{
			name: "list without the prefs", table: "prefs", want: "read setting " + prefDefaultPR,
			run: func(t *testing.T, s *Service, _, _ int64) error { _, err := s.List(t.Context()); return err },
		},
		{
			name: "delete without the prefs", table: "prefs", want: "read setting " + prefDefaultPR,
			run: func(t *testing.T, s *Service, _, quick int64) error { return s.Delete(t.Context(), quick) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, db := seeded(t, nil)
			review, quick := byNickname(t, s, "Review").ID, byNickname(t, s, "Quick look").ID

			exec(t, db, "DROP TABLE "+tt.table)

			require.ErrorContains(t, tt.run(t, s, review, quick), tt.want)
		})
	}
}

func TestAClosedDatabaseIsReported(t *testing.T) {
	t.Parallel()

	s := closedService(t)
	ctx := t.Context()

	_, err := s.List(ctx)
	require.ErrorContains(t, err, "read the prompts")

	_, err = s.store.All(ctx)
	require.ErrorContains(t, err, "read the prompts")

	_, err = s.Defaults(ctx)
	require.ErrorContains(t, err, "read setting "+prefDefaultPR)

	_, err = s.store.Lookup(ctx, 1)
	require.ErrorContains(t, err, "read the prompt")

	require.ErrorContains(t, s.SetDefault(ctx, string(item.KindPR), 1), "read the prompt")

	_, err = s.Check(ctx, CheckRequest{ID: 1, Input: validInput()})
	require.ErrorContains(t, err, "read setting "+prefDefaultPR)

	// The writes run in a transaction, which cannot start.
	_, err = s.Create(ctx, validInput())
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrInvalidPrompt)

	_, err = s.Update(ctx, 1, validInput())
	require.Error(t, err)

	require.Error(t, s.Delete(ctx, 1))

	_, err = s.Duplicate(ctx, 1)
	require.Error(t, err)

	_, err = s.Reset(ctx, 1)
	require.Error(t, err)

	require.Error(t, Seed(ctx, s.store))
}

func TestCheckReportsANicknameItCannotCheck(t *testing.T) {
	t.Parallel()

	s := closedService(t)

	got, err := s.Check(t.Context(), CheckRequest{Input: validInput()})
	require.NoError(t, err, "a new prompt's check reads nothing else")
	assert.Equal(t, []agent.FieldError{{Field: FieldNickname, Message: "Couldn't check whether the nickname is free."}}, got.Fields)
	assert.NotEmpty(t, got.Preview, "the preview still fills in")
}

func TestResetRefusesAMissingPrompt(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)

	_, err := s.Reset(t.Context(), 9999)
	require.ErrorIs(t, err, ErrPromptNotFound)
}

// promptStamps returns a stored prompt's created_at and updated_at as the database holds
// them.
func promptStamps(t *testing.T, db *sql.DB, id int64) (created, updated string) {
	t.Helper()

	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT CAST(created_at AS TEXT), CAST(updated_at AS TEXT) FROM prompts WHERE id = ?", id,
	).Scan(&created, &updated))

	return created, updated
}

// countingUses is a UsageSource that counts its calls and finds no uses.
type countingUses struct {
	calls int
}

func (c *countingUses) PromptUses(context.Context) ([]Use, error) {
	c.calls++

	return nil, nil
}

// failingUses is a UsageSource that cannot read the launch defaults.
type failingUses struct{}

func (failingUses) PromptUses(context.Context) ([]Use, error) {
	return nil, errUsesUnreadable
}

var errUsesUnreadable = errors.New("launch defaults unreadable")

func TestDeletingAPromptLaunchDefaultsNameIsRefusedByTheForeignKey(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	ctx := t.Context()

	mine, err := s.Create(ctx, validInput())
	require.NoError(t, err)

	addLaunchDefault(t, db, "acme-org", "", mine.ID)
	addLaunchDefault(t, db, "acme-org", "api", mine.ID)

	// The store checks only the app's default prompts itself, which this is not; the
	// launch defaults' foreign keys refuse the delete.
	var inUse *InUseError

	require.ErrorAs(t, s.store.remove(ctx, mine.ID), &inUse)
	assert.Equal(t, InUseError{Nickname: "Feedback"}, *inUse, "the store leaves the usage to the service")

	err = s.Delete(ctx, mine.ID)
	require.ErrorIs(t, err, ErrPromptInUse)
	require.ErrorAs(t, err, &inUse)
	assert.Equal(t, `"Feedback" can't be deleted while it's a default for acme-org and acme-org/api; `+
		"choose other defaults there first", err.Error())

	kept, err := s.store.Lookup(ctx, mine.ID)
	require.NoError(t, err, "the prompt is still there")
	assert.Equal(t, mine.Body, kept.Body)
}

func TestDeletingAnUnusedPromptReadsNoUsage(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	uses := &countingUses{}
	s := NewService(Config{Store: NewStore(db, nil), Usage: uses})
	ctx := t.Context()

	require.NoError(t, Seed(ctx, s.store))

	quick := idOf(t, s, "Quick look")
	require.NoError(t, s.Delete(ctx, quick))
	assert.Zero(t, uses.calls, "the foreign keys guard the delete; usage is read only to explain a refusal")

	require.ErrorIs(t, s.Delete(ctx, quick), ErrPromptNotFound)
	assert.Zero(t, uses.calls)
}

func TestDeleteReportsUsageItCannotRead(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	s := NewService(Config{Store: NewStore(db, nil), Usage: failingUses{}})
	ctx := t.Context()

	require.NoError(t, Seed(ctx, s.store))

	err := s.Delete(ctx, idOf(t, s, "Review"))
	require.ErrorIs(t, err, errUsesUnreadable)
	require.ErrorContains(t, err, "read where the prompts are used")
	require.NotErrorIs(t, err, ErrPromptInUse, "nothing says where it is used")
}

func TestDeleteWithoutAUsageSourceNamesOnlyTheDefaults(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	s := NewService(Config{Store: NewStore(db, nil)})
	ctx := t.Context()

	require.NoError(t, Seed(ctx, s.store))

	review := idOf(t, s, "Review")
	err := s.Delete(ctx, review)
	require.ErrorIs(t, err, ErrPromptInUse)
	assert.Contains(t, err.Error(), "the default for pull requests")

	listed, err := s.List(ctx)
	require.NoError(t, err)
	assert.Len(t, listed, len(Starters()))
}

// idOf returns the ID of the stored prompt with nickname.
func idOf(t *testing.T, s *Service, nickname string) int64 {
	t.Helper()

	all, err := s.store.All(t.Context())
	require.NoError(t, err)

	for _, p := range all {
		if p.Nickname == nickname {
			return p.ID
		}
	}

	require.Failf(t, "no prompt", "nickname %q", nickname)

	return 0
}

func TestWritesStampTheRowsFromTheClock(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	clk := testutil.NewClock(time.Date(2026, 1, 10, 7, 0, 0, 0, time.FixedZone("UTC-5", -5*60*60)))
	s := NewService(Config{Store: NewStore(db, clk)})
	ctx := t.Context()

	created, err := s.Create(ctx, validInput())
	require.NoError(t, err)
	assert.Equal(t, "2026-01-10T12:00:00Z", created.UpdatedAt)

	createdAt, updatedAt := promptStamps(t, db, created.ID)
	assert.Equal(t, "2026-01-10 12:00:00+00:00", createdAt, "stored in UTC")
	assert.Equal(t, createdAt, updatedAt)

	clk.Advance(time.Hour)

	in := validInput()
	in.Body = "Address every review comment on {{.URL}}"

	updated, err := s.Update(ctx, created.ID, in)
	require.NoError(t, err)
	assert.Equal(t, "2026-01-10T13:00:00Z", updated.UpdatedAt)

	_, updatedAt = promptStamps(t, db, created.ID)
	assert.Equal(t, "2026-01-10 13:00:00+00:00", updatedAt, "an update advances updated_at")

	again, _ := promptStamps(t, db, created.ID)
	assert.Equal(t, createdAt, again, "and keeps created_at")

	clk.Advance(time.Hour)

	dup, err := s.Duplicate(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "2026-01-10T14:00:00Z", dup.UpdatedAt, "a copy is stamped when it is made")
}

func TestStoreReportsAMissingPrompt(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	_, err := s.store.Lookup(ctx, 9999)
	require.ErrorIs(t, err, ErrPromptNotFound)

	require.ErrorIs(t, s.store.update(ctx, 9999, fromInput(validInput(), nil)), ErrPromptNotFound)
	require.ErrorIs(t, s.store.remove(ctx, 9999), ErrPromptNotFound)

	// A stored default naming a prompt that is gone does not make it in use.
	require.NoError(t, s.store.setDefault(ctx, item.KindPR, 9999))
	require.ErrorIs(t, s.store.remove(ctx, 9999), ErrPromptNotFound)
}

func TestStoreRefusesUnreadableExtraArguments(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	ctx := t.Context()
	review := idOf(t, s, "Review")

	// The schema takes any JSON array; the library only stores arrays of strings.
	exec(t, db, "UPDATE prompts SET extra_args = '[1]' WHERE id = ?", review)

	_, err := s.store.Lookup(ctx, review)
	require.ErrorContains(t, err, `read the prompt: prompt "Review": extra arguments`)

	_, err = s.store.All(ctx)
	require.ErrorContains(t, err, `read the prompts: prompt "Review": extra arguments`)

	_, err = s.store.Library(ctx)
	require.ErrorContains(t, err, "read the prompts")

	_, err = s.Defaults(ctx)
	require.ErrorContains(t, err, "read the prompts")
}

func TestLibraryHoldsEveryPromptAndTheDefaults(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	lib, err := s.store.Library(ctx)
	require.NoError(t, err)

	all, err := s.store.All(ctx)
	require.NoError(t, err)

	defaults, err := s.store.Defaults(ctx)
	require.NoError(t, err)

	assert.Equal(t, Library{Prompts: all, Defaults: defaults}, lib)
	assert.Equal(t, DefaultPrompts{PR: idOf(t, s, "Review"), Issue: idOf(t, s, "Work on it")}, lib.Defaults)
}

func TestLibraryReportsUnreadableDefaults(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	exec(t, db, "DROP TABLE prefs")

	_, err := s.store.Library(t.Context())
	require.ErrorContains(t, err, "read setting "+prefDefaultPR)
}

func TestAStoreJoinedToATransactionRollsBackWithIt(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	s := NewStore(db, nil)
	ctx := t.Context()

	err := store.InTx(ctx, db, func(tx *sql.Tx) error {
		joined := s.WithTx(tx)
		require.NoError(t, Seed(ctx, joined), "seeding joins the transaction rather than waiting on it")

		all, err := joined.All(ctx)
		require.NoError(t, err)
		assert.Len(t, all, len(Starters()), "the transaction sees its own writes")

		return errUsesUnreadable
	})
	require.ErrorIs(t, err, errUsesUnreadable)

	all, err := s.All(ctx)
	require.NoError(t, err)
	assert.Empty(t, all, "the starters rolled back with the transaction")
	assert.Empty(t, promptPrefs(t, db))
}
