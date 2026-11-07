package prompts

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
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

// pref is one row of the prefs table.
type pref struct {
	Key   string
	Value string
}

// promptPrefs returns the preferences the prompt library keeps, by key.
func promptPrefs(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()

	var rows []pref
	require.NoError(t, db.Raw("SELECT key, value FROM prefs WHERE key LIKE 'prompts.%'").Scan(&rows).Error)

	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Key] = r.Value
	}

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

	db := testutil.DB(t)
	require.NoError(t, setPref(db, prefSeeded, "not json"))

	require.ErrorContains(t, Seed(t.Context(), db), "read the starter prompts added")

	all, err := All(t.Context(), db)
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

			db := testutil.DB(t)
			require.NoError(t, db.Exec(tt.trigger).Error)

			require.ErrorContains(t, Seed(t.Context(), db), tt.want)

			all, err := All(t.Context(), db)
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
			require.NoError(t, db.Exec(tt.trigger).Error)

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
			name: "delete without the launch defaults", table: "launch_defaults", want: "read where the prompts are used",
			run: func(t *testing.T, s *Service, _, quick int64) error { return s.Delete(t.Context(), quick) },
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, db := seeded(t, nil)
			review, quick := byNickname(t, s, "Review").ID, byNickname(t, s, "Quick look").ID

			require.NoError(t, db.Exec("DROP TABLE "+tt.table).Error)

			require.ErrorContains(t, tt.run(t, s, review, quick), tt.want)
		})
	}
}

func TestAClosedDatabaseIsReported(t *testing.T) {
	t.Parallel()

	db := testutil.ClosedDB(t)
	s := NewService(Config{DB: db})
	ctx := t.Context()

	_, err := s.List(ctx)
	require.ErrorContains(t, err, "read the prompts")

	_, err = All(ctx, db)
	require.ErrorContains(t, err, "read the prompts")

	_, err = s.Defaults(ctx)
	require.ErrorContains(t, err, "read setting "+prefDefaultPR)

	_, err = Lookup(ctx, db, 1)
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

	require.Error(t, Seed(ctx, db))
}

func TestCheckReportsANicknameItCannotCheck(t *testing.T) {
	t.Parallel()

	s := NewService(Config{DB: testutil.ClosedDB(t)})

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
