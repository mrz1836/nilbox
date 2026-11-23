package autopilot_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/item"
)

// fixChecksKey is the Fix checks starter's key.
const fixChecksKey = "fix-checks"

// UUIDs of the follow rig's prompts and scripts.
const (
	triageUUID = "6f9619ff-8b86-4d01-b42d-00cf4fc964ff"
	fixUUID    = "5d2e8f41-7a63-4c90-9b18-e4f6a2c3d5b7"
	lintUUID   = "3a7f1c52-9d84-4e6b-8f21-6c0d5e9b4a13"
	otherUUID  = "7c4e2a91-3b5d-4f68-a0c1-9d8e7f6b5a43"
)

// liveLibrary is a prompt library a test changes as it goes, or makes unreadable.
type liveLibrary struct {
	mu      sync.Mutex
	prompts []autopilot.LibraryPrompt
	err     error
}

func (l *liveLibrary) Prompts(context.Context) ([]autopilot.LibraryPrompt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]autopilot.LibraryPrompt(nil), l.prompts...), l.err
}

// set makes the library hold prompts.
func (l *liveLibrary) set(prompts ...autopilot.LibraryPrompt) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.prompts = prompts
}

// liveScripts are the user's scripts, which a test changes as it goes.
type liveScripts struct {
	mu      sync.Mutex
	scripts []autopilot.MyScript
}

func (s *liveScripts) MyScripts(context.Context) ([]autopilot.MyScript, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]autopilot.MyScript(nil), s.scripts...), nil
}

// set makes the user's scripts scripts.
func (s *liveScripts) set(scripts ...autopilot.MyScript) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scripts = scripts
}

// triage is the user's own Triage prompt, for pull requests, under nickname.
func triage(nickname string) autopilot.LibraryPrompt {
	return autopilot.LibraryPrompt{UUID: triageUUID, Nickname: nickname, Kinds: []item.Kind{item.KindPR}}
}

// fixChecks is the user's copy of the Fix checks starter.
func fixChecks() autopilot.LibraryPrompt {
	return autopilot.LibraryPrompt{
		UUID: fixUUID, Starter: fixChecksKey, Nickname: "Fix checks", Kinds: []item.Kind{item.KindPR},
	}
}

// lint is the user's Lint script, ID 7, under name.
func lint(name string) autopilot.MyScript {
	return autopilot.MyScript{UUID: lintUUID, ID: 7, Name: name, Command: "magex lint", Look: "v1", Shows: "every clone"}
}

// followRig is a rig whose Service reads lib and mine.
type followRig struct {
	*rig

	lib  *liveLibrary
	mine *liveScripts
}

// newFollowRig returns a rig over Triage and Fix checks, and the Lint script.
func newFollowRig(t *testing.T) *followRig {
	t.Helper()

	r := newRig(t, autopilotprefs.ModeAsk)
	lib := &liveLibrary{prompts: []autopilot.LibraryPrompt{triage(triageNick), fixChecks()}}
	mine := &liveScripts{scripts: []autopilot.MyScript{lint("Lint")}}

	r.svc = autopilot.NewService(autopilot.Config{
		Pool: r.db, Prefs: prefs{cfg: onSettings()}, Accounts: r.accounts, Names: names{}, File: r.file, Clock: r.clock,
		Logger: r.logs.Logger(), Offers: rigOffers(), Library: lib, MyScripts: mine,
	})

	return &followRig{rig: r, lib: lib, mine: mine}
}

// triageSpec is a routine that runs prompt in the user's terminal on their failing pull
// requests.
func triageSpec(prompt spec.PromptRef) spec.Spec {
	s := agentSpec()
	s.Steps[0].Agent.Prompt = prompt

	return s
}

// lintRoutineSpec is a routine that tells the user of a pull request of theirs that
// appears, then runs the script step names.
func lintRoutineSpec(step spec.ScriptStep) spec.Spec {
	s := notifySpec()
	s.Trigger = spec.Trigger{Event: catalog.EventAppears}
	s.Steps = append(s.Steps, spec.Step{Script: &step})

	return s
}

// versionOne is s as a build from before UUIDs stored it: version 1.
func versionOne(t *testing.T, s spec.Spec) string {
	t.Helper()

	data := mustEncode(t, s)
	require.Contains(t, data, `"version":2`)

	return strings.Replace(data, `"version":2`, `"version":1`, 1)
}

// routineUUID returns the routine id's UUID as stored.
func (r *rig) routineUUID(t *testing.T, id int64) string {
	t.Helper()

	var u string

	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT uuid FROM autopilot_routines WHERE id = ?`, id).Scan(&u))

	return u
}

func TestFollowStampsARoutineStoredBeforeUUIDsOnceAndKeepsWhenItChanged(t *testing.T) {
	t.Parallel()

	r := newFollowRig(t)
	ctx := t.Context()

	byName := r.insertRaw(t, "Triage it", "ask", versionOne(t, triageSpec(spec.PromptRef{Nickname: "triage"})))
	byScript := r.insertRaw(t, "Lint it", "ask", versionOne(t, lintRoutineSpec(spec.ScriptStep{ID: 7, Name: "Lint"})))

	before, err := r.svc.Get(ctx, byName)
	require.NoError(t, err)
	assert.Empty(t, r.routineUUID(t, byName))

	f := autopilot.NewFollower(r.svc)

	n, err := f.Follow(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	got, err := r.svc.Get(ctx, byName)
	require.NoError(t, err)
	assert.Equal(t, spec.PromptRef{UUID: triageUUID, Nickname: triageNick}, got.Spec.Steps[0].Agent.Prompt,
		"named by its UUID, and by its nickname as the library spells it")
	assert.Equal(t, spec.Version, got.Spec.Version)
	assert.Contains(t, r.storedSpec(t, byName), `"version":2`, "written in this build's version of the format")
	assert.Regexp(t, uuidPattern, r.routineUUID(t, byName), "the routine gets its own UUID")
	assert.Equal(t, before.UpdatedAt, got.UpdatedAt, "a write of UUIDs and the version keeps when it last changed")

	script, err := r.svc.Get(ctx, byScript)
	require.NoError(t, err)
	assert.Equal(t, spec.ScriptStep{UUID: lintUUID, ID: 7, Name: "Lint"}, *script.Spec.Steps[1].Script)

	stored := r.storedSpec(t, byName)

	n, err = f.Follow(ctx, nil)
	require.NoError(t, err)
	assert.Zero(t, n, "once stamped, a follow only reads")
	assert.Equal(t, stored, r.storedSpec(t, byName))
}

func TestFollowWritesTheNameNowOfAPromptRenamedOrResetAndOfAScriptRenamed(t *testing.T) {
	t.Parallel()

	r := newFollowRig(t)
	ctx := t.Context()

	prompted := r.save(t, draft("Triage it", triageSpec(spec.PromptRef{Nickname: triageNick})))
	scripted := r.save(t, draft("Lint it", lintRoutineSpec(spec.ScriptStep{ID: 7, Name: "Lint"})))
	require.Equal(t, triageUUID, prompted.Spec.Steps[0].Agent.Prompt.UUID, "Save stamps the UUID")
	require.Equal(t, lintUUID, scripted.Spec.Steps[1].Script.UUID)

	// The prompt is renamed, as a reset or an import over it may rename it, without a word
	// to the routines; another prompt takes its old nickname; and a restore gives the script
	// another ID and the user renames it.
	r.lib.set(triage("sort-it"), fixChecks(), autopilot.LibraryPrompt{
		UUID: otherUUID, Nickname: triageNick, Kinds: []item.Kind{item.KindPR},
	})

	renamedScript := lint("Lint all")
	renamedScript.ID = 12
	r.mine.set(renamedScript)

	n, err := autopilot.NewFollower(r.svc).Follow(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	got, err := r.svc.Get(ctx, prompted.ID)
	require.NoError(t, err)
	assert.Equal(t, spec.PromptRef{UUID: triageUUID, Nickname: "sort-it"}, got.Spec.Steps[0].Agent.Prompt,
		"the UUID finds it, never the prompt that took the old nickname")
	assert.Contains(t, got.Sentence, `"sort-it"`, "its sentence names it as it is called now")
	assert.True(t, got.UpdatedAt.After(prompted.UpdatedAt), "a name the user reads changed, so it changed now")

	lintIt, err := r.svc.Get(ctx, scripted.ID)
	require.NoError(t, err)
	assert.Equal(t, spec.ScriptStep{UUID: lintUUID, ID: 12, Name: "Lint all"}, *lintIt.Spec.Steps[1].Script)
}

func TestFollowReplaysRenamesOnlyOnARoutineThatNamesThePromptByNameAlone(t *testing.T) {
	t.Parallel()

	r := newFollowRig(t)
	ctx := t.Context()

	// One stored before UUIDs names Triage by an old nickname; one names it by its UUID
	// beside a nickname a rename's from has, which it keeps following by UUID.
	old := r.insertRaw(t, "Old", "ask", versionOne(t, triageSpec(spec.PromptRef{Nickname: "first"})))
	stamped := r.insertRaw(t, "Stamped", "ask", mustEncode(t, triageSpec(spec.PromptRef{UUID: triageUUID, Nickname: "first"})))
	r.lib.set(triage("third"), fixChecks())

	n, err := autopilot.NewFollower(r.svc).Follow(ctx, []autopilot.PromptRename{
		{From: "FIRST", To: "second"}, {From: "second", To: "third"},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	for _, id := range []int64{old, stamped} {
		got, err := r.svc.Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, spec.PromptRef{UUID: triageUUID, Nickname: "third"}, got.Spec.Steps[0].Agent.Prompt, id)
	}
}

func TestFollowLeavesWhatItCanNotResolveOrReadAsItIs(t *testing.T) {
	t.Parallel()

	r := newFollowRig(t)
	ctx := t.Context()

	gone := mustEncode(t, triageSpec(spec.PromptRef{Nickname: "Gone"}))
	id := r.insertRaw(t, "Gone", "ask", gone)
	broken := r.insertRaw(t, "From the future", "ask", `{"version":3,"somethingNew":true}`)

	n, err := autopilot.NewFollower(r.svc).Follow(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "each gets its UUID, nothing more")
	assert.Equal(t, gone, r.storedSpec(t, id), "a prompt the library doesn't have stays named as it was")
	assert.JSONEq(t, `{"version":3,"somethingNew":true}`, r.storedSpec(t, broken), "a newer build's is never rewritten")

	r.lib.err = errLocked

	_, err = autopilot.NewFollower(r.svc).Follow(ctx, nil)
	require.ErrorIs(t, err, errLocked, "a library it can't read stamps nothing")
}

func TestFollowsAtOnceWriteEachRoutineOnce(t *testing.T) {
	t.Parallel()

	r := newFollowRig(t)
	id := r.insertRaw(t, "Triage it", "ask", versionOne(t, triageSpec(spec.PromptRef{Nickname: triageNick})))
	f := autopilot.NewFollower(r.svc)

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		total int
		errs  []error
	)

	for range 6 {
		wg.Go(func() {
			n, err := f.Follow(t.Context(), nil)

			mu.Lock()
			total += n

			errs = append(errs, err)
			mu.Unlock()
		})
	}

	wg.Wait()
	require.NoError(t, errors.Join(errs...))
	assert.Equal(t, 1, total, "one follow writes it, and the rest find nothing to do")
	assert.Contains(t, r.storedSpec(t, id), triageUUID)
}

func TestImportStampsARoutineWithThePromptsAndScriptsHere(t *testing.T) {
	t.Parallel()

	from := newFollowRig(t)
	from.save(t, draft("Triage it", triageSpec(spec.PromptRef{Nickname: triageNick})))
	from.save(t, draft("Lint it", lintRoutineSpec(spec.ScriptStep{ID: 7, Name: "Lint"})))

	path := filepath.Join(t.TempDir(), "routines.json")
	_, err := from.svc.Export(t.Context(), path, nil, false)
	require.NoError(t, err)

	// This Mac has Triage under another UUID, and Lint under another ID and UUID.
	to := newFollowRig(t)
	to.lib.set(autopilot.LibraryPrompt{UUID: otherUUID, Nickname: triageNick, Kinds: []item.Kind{item.KindPR}})
	here := autopilot.MyScript{UUID: fixUUID, ID: 3, Name: "Lint", Command: "magex lint", Look: "v1", Shows: "every clone"}
	to.mine.set(here)

	_, err = to.svc.Import(t.Context(), autopilot.ImportRequest{Path: path})
	require.NoError(t, err)

	got := to.list(t)
	require.Len(t, got, 2)
	assert.Equal(t, spec.PromptRef{UUID: otherUUID, Nickname: triageNick}, got[0].Spec.Steps[0].Agent.Prompt,
		"a UUID this Mac doesn't know falls back to the nickname, and names the prompt here by its own")
	assert.Equal(t, lintUUID, got[1].Spec.Steps[1].Script.UUID,
		"a script found by neither its UUID nor its name under its ID is left as it came")
}

// records are what the follow rig's import transaction holds: its library and scripts as
// they are now, and links, the prompts the file brought.
func (r *followRig) records(t *testing.T, links map[string]string) autopilot.BackupRecords {
	t.Helper()

	lib, err := r.lib.Prompts(t.Context())
	require.NoError(t, err)

	mine, err := r.mine.MyScripts(t.Context())
	require.NoError(t, err)

	return autopilot.BackupRecords{Prompts: lib, Scripts: mine, Links: links}
}

func TestABackupFindsARoutineByItsUUIDAndKeepsItsNameAndHistory(t *testing.T) {
	t.Parallel()

	r := newFollowRig(t)
	mine := r.save(t, draft("Triage it", triageSpec(spec.PromptRef{Nickname: triageNick})))
	b := exported(t, r.rig)

	// Since the backup, the user renamed it and changed it, and made another of its old
	// name.
	d := draft("Sort my checks", triageSpec(spec.PromptRef{Starter: fixChecksKey}))
	d.ID = mine.ID
	r.save(t, d)
	other := r.save(t, draft("Triage it", notifySpec()))

	plan, count := importWith(t, r.rig, b, backup.ModeReplace, r.records(t, nil))
	assert.Equal(t, map[string]backup.Change{"Sort my checks": backup.ChangeUpdate, "Triage it": backup.ChangeRemove},
		changes(plan.Plan), "found by its UUID, listed under its name here; the other of its old name goes")
	assert.Equal(t, backup.Count{Update: 1, Remove: 1}, count)

	got := r.list(t)
	require.Len(t, got, 1)
	assert.Equal(t, mine.ID, got[0].ID, "the same routine, so its history stays its")
	assert.Equal(t, mine.UUID, got[0].UUID)
	assert.Equal(t, "Sort my checks", got[0].Name, "it keeps its name here")
	assert.Equal(t, spec.PromptRef{UUID: triageUUID, Nickname: triageNick}, got[0].Spec.Steps[0].Agent.Prompt,
		"and takes the backup's definition")

	_, err := r.svc.Get(t.Context(), other.ID)
	require.Error(t, err, "Replace deleted the routine the file doesn't match")
}

func TestABackupAddsARoutineUnderAFreeNameBesideOneItsUUIDFound(t *testing.T) {
	t.Parallel()

	r := newFollowRig(t)
	first := r.save(t, draft("First", triageSpec(spec.PromptRef{Nickname: triageNick})))
	r.save(t, draft("Second", notifySpec()))
	b := exported(t, r.rig)

	// First is renamed Second here, and the routine that was Second is deleted: the file's
	// Second, whose UUID no routine has now, is added beside it under a name of its own.
	second := r.list(t)[1]
	require.NoError(t, r.svc.Delete(t.Context(), second.ID))

	d := draft("Second", triageSpec(spec.PromptRef{Nickname: triageNick}))
	d.ID = first.ID
	r.save(t, d)

	plan, count := importWith(t, r.rig, b, backup.ModeMerge, r.records(t, nil))
	assert.Equal(t, map[string]backup.Change{"Second": backup.ChangeSame, "Second 2": backup.ChangeAdd},
		changes(plan.Plan))
	assert.Equal(t, backup.Count{Same: 1, Add: 1}, count)

	routines := r.list(t)
	names := make([]string, 0, len(routines))

	for _, rt := range routines {
		names = append(names, rt.Name)
	}

	assert.Equal(t, []string{"Second", "Second 2"}, names)
}

func TestABackupsRoutineRunsThePromptTheSameFileBrought(t *testing.T) {
	t.Parallel()

	from := newFollowRig(t)
	from.save(t, draft("Triage it", triageSpec(spec.PromptRef{Nickname: triageNick})))
	b := exported(t, from.rig)

	// This Mac has a Triage of its own, so the file's arrived as Triage 2.
	to := newFollowRig(t)
	brought := autopilot.LibraryPrompt{UUID: otherUUID, Nickname: "Triage 2", Kinds: []item.Kind{item.KindPR}}
	mine := autopilot.LibraryPrompt{UUID: fixUUID, Nickname: triageNick, Kinds: []item.Kind{item.KindPR}}
	to.lib.set(mine, brought)

	_, count := importWith(t, to.rig, b, backup.ModeMerge, to.records(t, map[string]string{triageNick: otherUUID}))
	assert.Equal(t, backup.Count{Add: 1}, count)

	got := to.list(t)
	require.Len(t, got, 1)
	assert.Equal(t, spec.PromptRef{UUID: otherUUID, Nickname: "Triage 2"}, got[0].Spec.Steps[0].Agent.Prompt,
		"the file's own Triage, never the one this Mac had")
}

func TestABackupsPreviewAndItsImportAgreeOnARoutineTheSame(t *testing.T) {
	t.Parallel()

	r := newFollowRig(t)
	r.save(t, draft("Triage it", triageSpec(spec.PromptRef{Nickname: triageNick})))

	// A backup from before UUIDs names the same prompt by its nickname alone.
	old := exported(t, r.rig)
	old.Routines[0].UUID = ""
	old.Routines[0].Spec = json.RawMessage(versionOne(t, triageSpec(spec.PromptRef{Nickname: triageNick})))

	plan, count := importWith(t, r.rig, old, backup.ModeMerge, r.records(t, nil))
	assert.Equal(t, map[string]backup.Change{"Triage it": backup.ChangeSame}, changes(plan.Plan),
		"stamped as this Mac names it, it is the same")
	assert.Equal(t, backup.Count{Same: 1}, count, "and the import finds it the same too")
}
