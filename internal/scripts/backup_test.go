package scripts

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/store"
)

// importScripts plans in under mode and applies it as an import does, returning the plan
// and what the apply counted.
func (r *rig) importScripts(in PortableScripts, mode backup.Mode) (ScriptsPlan, backup.Count) {
	r.t.Helper()

	part := NewBackupPart(r.svc)

	plan, err := part.PlanBackup(r.t.Context(), in, mode)
	require.NoError(r.t, err)

	var count backup.Count

	require.NoError(r.t, store.InTx(r.t.Context(), r.runner.DB, func(tx *sql.Tx) error {
		var applyErr error

		count, applyErr = part.ApplyBackup(r.t.Context(), tx, plan)

		return applyErr
	}))

	return plan, count
}

// changes lists a plan's items as "<change> <label>".
func changes(p backup.Plan) []string {
	out := make([]string, 0, len(p.Items))
	for _, item := range p.Items {
		out = append(out, string(item.Change)+" "+item.Label)
	}

	return out
}

func TestPortableScriptsRoundTrip(t *testing.T) {
	t.Parallel()

	src := newRig(t)
	src.save(UserScript{
		Name: fixAllName, Command: "magex format:fix", Icon: IconLint, Scope: ScopeRepos, Repos: []string{"acme-org/sdk"},
		RunsIn: integrationprefs.RunsInTerminal, Confirm: true,
	})
	src.save(named(testName))

	out, err := NewBackupPart(src.svc).PortableScripts(t.Context())
	require.NoError(t, err)

	data, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"id"`, "a backup never names a script by its ID")

	var read PortableScripts
	require.NoError(t, json.Unmarshal(data, &read))

	dst := newRig(t)
	plan, count := dst.importScripts(read, backup.ModeMerge)
	assert.Equal(t, []string{"add " + fixAllName, "add " + testName}, changes(plan.Plan))
	assert.Equal(t, backup.Count{Add: 2}, count)

	back, err := NewBackupPart(dst.svc).PortableScripts(t.Context())
	require.NoError(t, err)
	assert.Equal(t, out, back, "every field comes back, in order")
}

func TestMergeSkipsAnIdenticalScriptAndAppendsTheRest(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	kept := r.save(named(firstName))
	r.save(named(testName))

	in := PortableScripts{Scripts: []PortableScript{
		portable(named("New")), portable(named(firstName)),
		{Name: "", Command: "x", Icon: IconPlay, Scope: ScopeAll},
		portable(named("Later")),
	}}

	plan, count := r.importScripts(in, backup.ModeMerge)
	assert.Equal(t, []string{"add New", "same " + firstName, "add Later"}, changes(plan.Plan))
	assert.Equal(t, []string{"The script “” was left out. Name the script."}, plan.Notes)
	assert.Equal(t, backup.Count{Add: 2, Same: 1}, count)
	assert.Equal(t, []string{firstName, testName, "New", "Later"}, r.order(), "the others are appended in order")

	got, ok, err := r.svc.yours.get(t.Context(), kept.ID)
	require.NoError(t, err)
	assert.True(t, ok, "the identical script keeps its ID")
	assert.Equal(t, kept, got)
}

func TestReplaceMakesTheScriptsExactlyTheFiles(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	kept := r.save(named(firstName))
	r.save(named(testName))

	in := PortableScripts{Scripts: []PortableScript{portable(named("New")), portable(named(firstName))}}

	plan, count := r.importScripts(in, backup.ModeReplace)
	assert.Equal(t, []string{"add New", "same " + firstName, "remove " + testName}, changes(plan.Plan))
	assert.Equal(t, backup.Count{Add: 1, Same: 1, Remove: 1}, count)
	assert.Equal(t, []string{"New", firstName}, r.order(), "exactly the file's, in its order")

	_, ok, err := r.svc.yours.get(t.Context(), kept.ID)
	require.NoError(t, err)
	assert.True(t, ok, "a script identical to one of the file's is kept as it is, so Run again still finds it")
}
