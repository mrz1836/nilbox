package autopilot_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// pastSyncSpec is a routine that, when the checks pass on the user's own pull request,
// replies with Sync OK, then squash-merges it, past the rules when an approval from
// someone else is all it lacks.
func pastSyncSpec() spec.Spec {
	s := notifySpec()
	s.Trigger = spec.Trigger{Event: catalog.EventChecksPassed}
	s.Steps = []spec.Step{
		{GitHub: []spec.GitHubAction{{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Sync OK"}}}}},
		{Merge: &spec.MergeStep{How: catalog.MergeWhenMergeable, Method: catalog.MergeSquash, AllowBypass: true}},
	}

	return s
}

// acceptedDraft is pastSyncSpec's routine, Merge my syncs, accepted as one that merges past
// the rules.
func acceptedDraft() autopilot.Draft {
	d := draft("Merge my syncs", pastSyncSpec())
	d.AcceptPastRules = true

	return d
}

// storedHash returns the routine id's past_rules_hash and its spec's own full SHA-256.
func (r *rig) storedHash(t *testing.T, id int64) (string, string) {
	t.Helper()

	var hash, stored string

	require.NoError(t, r.db.QueryRowContext(t.Context(),
		`SELECT past_rules_hash, spec FROM autopilot_routines WHERE id = ?`, id).Scan(&hash, &stored))

	sum := sha256.Sum256([]byte(stored))

	return hash, hex.EncodeToString(sum[:])
}

func TestSavingAMergePastTheRulesNeedsTheUsersAcceptance(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAuto)

	_, err := r.svc.Save(t.Context(), draft("Merge my syncs", pastSyncSpec()))
	require.Error(t, err)

	var problems *autopilot.ProblemsError
	require.ErrorAs(t, err, &problems)
	assert.Contains(t, uierr.Message(err), "Confirm that this routine may merge your own pull requests past the rules")
	assert.Empty(t, r.list(t), "nothing is saved")

	saved := r.save(t, acceptedDraft())
	assert.Equal(t, autopilot.PastRulesAllowed, saved.PastRules)

	hash, want := r.storedHash(t, saved.ID)
	assert.Equal(t, want, hash, "the full SHA-256 of the spec text accepted")

	renamed := draft("Merge my sync pull requests", pastSyncSpec())
	renamed.ID = saved.ID
	got := r.save(t, renamed)
	assert.Equal(t, autopilot.PastRulesAllowed, got.PastRules, "the same spec text needs no new tick")

	edited := draft("Merge my sync pull requests", pastSyncSpec())
	edited.ID = saved.ID
	edited.Spec.Steps[1].Merge.Method = catalog.MergeRebase

	_, err = r.svc.Save(t.Context(), edited)
	require.ErrorAs(t, err, &problems, "an edit of the spec needs the tick again")

	stillHash, _ := r.storedHash(t, saved.ID)
	assert.Equal(t, hash, stillHash, "a refused save stores nothing")

	edited.AcceptPastRules = true
	got = r.save(t, edited)
	assert.Equal(t, autopilot.PastRulesAllowed, got.PastRules)

	hash, want = r.storedHash(t, saved.ID)
	assert.Equal(t, want, hash, "accepted as the new text")

	plain := draft("Merge my sync pull requests", pastSyncSpec())
	plain.ID = saved.ID
	plain.Spec.Steps[1].Merge.AllowBypass = false
	got = r.save(t, plain)
	assert.Equal(t, autopilot.PastRulesNone, got.PastRules)

	hash, _ = r.storedHash(t, saved.ID)
	assert.Empty(t, hash, "a spec with no tick keeps no acceptance")
}

func TestACopyOfARoutineIsNeverAcceptedAsMergingPastTheRules(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAuto)
	saved := r.save(t, acceptedDraft())

	copied, err := r.svc.Duplicate(t.Context(), saved.ID)
	require.NoError(t, err)
	assert.Equal(t, autopilot.PastRulesNeedsTick, copied.PastRules)

	hash, _ := r.storedHash(t, copied.ID)
	assert.Empty(t, hash)

	original, err := r.svc.Get(t.Context(), saved.ID)
	require.NoError(t, err)
	assert.Equal(t, autopilot.PastRulesAllowed, original.PastRules, "the original keeps its own")
}

func TestNoCopyOutsideTheDatabaseCarriesTheAcceptance(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "set-aside.db")
	used := rigAt(t, testutil.DBAt(t, path), filepath.Join(dir, "autopilot.json"), autopilotprefs.ModeAuto)
	saved := used.save(t, acceptedDraft())
	require.Equal(t, autopilot.PastRulesAllowed, saved.PastRules)

	copyFile, err := os.ReadFile(used.file.Path())
	require.NoError(t, err)
	assert.NotContains(t, string(copyFile), "astRules", "autopilot.json never holds it")
	assert.NotContains(t, string(copyFile), hashOf(t, used, saved.ID))

	exportPath := filepath.Join(dir, "routines.json")
	_, err = used.svc.Export(t.Context(), exportPath, nil, false)
	require.NoError(t, err)

	exportFile, err := os.ReadFile(exportPath) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.NotContains(t, string(exportFile), "astRules", "an export never holds it")

	t.Run("a restore", func(t *testing.T) {
		t.Parallel()

		fresh := newRig(t, autopilotprefs.ModeAuto)

		_, err := autopilot.Restore(t.Context(), fresh.db, fresh.clock, path, fresh.file)
		require.NoError(t, err)

		routines := fresh.list(t)
		require.Len(t, routines, 1)
		assert.Equal(t, autopilot.PastRulesNeedsTick, routines[0].PastRules)
	})

	t.Run("an import", func(t *testing.T) {
		t.Parallel()

		to := newRig(t, autopilotprefs.ModeAuto)

		preview, err := to.svc.PreviewImport(t.Context(), exportPath)
		require.NoError(t, err)

		_, err = to.svc.Import(t.Context(), autopilot.ImportRequest{Path: exportPath, SHA256: preview.SHA256})
		require.NoError(t, err)

		routines := to.list(t)
		require.Len(t, routines, 1)
		assert.Equal(t, autopilot.PastRulesNeedsTick, routines[0].PastRules)
	})

	t.Run("a backup", func(t *testing.T) {
		t.Parallel()

		b := exported(t, used)
		raw, err := autopilot.EncodeBundle(b)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), "astRules", "a backup never holds it")

		to := newRig(t, autopilotprefs.ModeAuto)
		mine := to.save(t, acceptedDraft())

		// The backup's routine differs from the one here, so the import rewrites it.
		b.Routines[0].Description = "From the backup."
		b.Routines[0].Spec = bytes.Replace(b.Routines[0].Spec, []byte(`"squash"`), []byte(`"rebase"`), 1)
		require.Contains(t, string(b.Routines[0].Spec), `"rebase"`)
		importInto(t, to, b, backup.ModeMerge)

		got, err := to.svc.Get(t.Context(), mine.ID)
		require.NoError(t, err)
		assert.Equal(t, autopilot.PastRulesNeedsTick, got.PastRules, "a backup's routine is accepted again on its own")

		hash, _ := to.storedHash(t, mine.ID)
		assert.Empty(t, hash)
	})
}

// hashOf returns the routine id's stored past_rules_hash, which must be set.
func hashOf(t *testing.T, r *rig, id int64) string {
	t.Helper()

	hash, _ := r.storedHash(t, id)
	require.NotEmpty(t, hash)

	return hash
}

func TestARenamedPromptKeepsTheAcceptanceOnlyWhereItStood(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAuto)
	accepted := r.save(t, acceptedDraft())

	never := r.insertRaw(t, "Merge them too", "auto", mustEncode(t, pastSyncSpec()))

	n, err := autopilot.NewPromptFollower(r.svc).Renamed(t.Context(), "Sync OK", "Synced")
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	got, err := r.svc.Get(t.Context(), accepted.ID)
	require.NoError(t, err)
	assert.Equal(t, "Synced", got.Spec.Steps[0].GitHub[0].Reply.Prompt.Nickname)
	assert.Equal(t, autopilot.PastRulesAllowed, got.PastRules, "the rename carries what stood")

	hash, want := r.storedHash(t, accepted.ID)
	assert.Equal(t, want, hash)

	other, err := r.svc.Get(t.Context(), never)
	require.NoError(t, err)
	assert.Equal(t, autopilot.PastRulesNeedsTick, other.PastRules, "and nothing more")
}

// mustEncode returns s as stored.
func mustEncode(t *testing.T, s spec.Spec) string {
	t.Helper()

	data, err := spec.Encode(spec.Normalize(s))
	require.NoError(t, err)

	return string(data)
}
