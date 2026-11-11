package scripts

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errHelp is a magex whose help:commands fails.
var errHelp = errors.New("help: exit status 1")

func TestTheScriptsSwitch(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{t3File: t3With(magexTest)})
	e := r.entry(dir, "t3:Test")

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256, Remember: true})
	require.NoError(t, err)
	r.ended(id)

	r.settings.scripts(integrationprefs.SwitchOff)

	p := r.list(dir)
	assert.True(t, p.Off)
	assert.Equal(t, "Scripts are turned off in Settings → Integrations.", p.Why)
	assert.Empty(t, p.Sections)
	assert.Equal(t, "acme-org/sdk", p.Repo, "the page still names the clone")

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref})
	require.ErrorIs(t, err, ErrCantRun)
	assert.Equal(t, p.Why, uierr.Message(err))

	_, err = r.runs.RunAgain(t.Context(), id)
	require.ErrorIs(t, err, ErrCantRun, "Run again too")

	r.settings.scripts(integrationprefs.SwitchAuto)
	r.tools.on(false, false)

	p = r.list(dir)
	assert.True(t, p.Off, "automatic, with neither mage-x nor go-pre-commit on")
	assert.Contains(t, p.Why, "Scripts turn on with mage-x or go-pre-commit")

	r.tools.on(false, true)
	assert.False(t, r.list(dir).Off, "automatic, with go-pre-commit on")

	r.tools.on(true, false)
	assert.False(t, r.list(dir).Off, "automatic, with mage-x on")

	r.tools.on(false, false)
	r.settings.scripts(integrationprefs.SwitchOn)
	assert.False(t, r.list(dir).Off, "on, whatever is found")
}

func TestAFailedListOfMagexsTargetsIsKept(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.thisRepo()
	r.help.answer("", errHelp)

	magex := r.list(dir).Sections[3]
	assert.Equal(t, "Couldn't list magex's targets.", magex.Error)
	assert.Empty(t, magex.Entries)

	r.list(dir)

	_, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: "magex:test"})
	require.Error(t, err)

	dirs, _ := r.help.calls()
	assert.Len(t, dirs, 1, "no page or run waits on magex again")

	r.help.answer(fixture(t, "magex-1.30.2-help-commands.txt"), nil)

	p, err := r.svc.List(t.Context(), r.shown(dir), true)
	require.NoError(t, err)
	assert.NotEmpty(t, p.Sections[3].Entries, "a refresh lists them again")

	dirs, _ = r.help.calls()
	assert.Len(t, dirs, 2)

	r.magex("1.30.3", testutil.FakeRun{})
	r.help.answer("", errHelp)
	assert.Equal(t, "Couldn't list magex's targets.", r.list(dir).Sections[3].Error, "another copy lists its own")
}

func TestAConfirmationIsKeptOnlyOnceTheCommandIsWorkedOut(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{tasksFile: `{"tasks": [
		{"label": "test", "type": "shell", "command": "magex test", "options": {"shell": {"executable": "no-such-shell"}}}]}`})
	e := r.entry(dir, "tasks:test")
	require.True(t, e.Runnable)

	_, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256, Remember: true})
	require.Error(t, err, "its shell isn't found")
	assert.Equal(t, TrustAsk, r.entry(dir, "tasks:test").Trust, "nothing that didn't run is trusted")
	assert.Zero(t, r.recorded())
}

func TestAVersionBumpIsCaughtAsItRuns(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{tasksFile: `{
		"inputs": [{"id": "target", "type": "pickString", "options": ["test", "version:bump"], "default": "test"}],
		"tasks": [{"label": "magex", "type": "shell", "command": "magex ${input:target}"}]}`})

	e := r.entry(dir, "tasks:magex")
	require.False(t, e.Release, "the page can't tell")

	req := RunRequest{
		Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256, Remember: true,
		Inputs: map[string]string{"target": "version:bump"},
	}

	_, err := r.svc.Run(t.Context(), req)
	require.ErrorIs(t, err, ErrRelease, "the input made it one")
	assert.Equal(t, "Release does this, with its checks. Open Release, or run it anyway.", uierr.Message(err))
	assert.Equal(t, TrustAsk, r.entry(dir, "tasks:magex").Trust, "refused before it was trusted")

	req.Anyway = true
	id, err := r.svc.Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, []string{r.shell, "-c", "magex version:bump"}, r.ended(id).Argv)

	req.Anyway, req.Inputs = false, map[string]string{"target": "test"}
	id, err = r.svc.Run(t.Context(), req)
	require.NoError(t, err)
	r.ended(id)
}

func TestAMagexAliasThatBumpsIsCaughtAsItRuns(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.help.answer(`{"commands": [{"name": "test"}, {"name": "version:bump", "aliases": ["bump"]}]}`, nil)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{t3File: t3With("magex bump")})

	e := r.entry(dir, "t3:Test")
	require.False(t, e.Release)

	_, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256})
	require.ErrorIs(t, err, ErrRelease, "bump is version:bump to magex")
}

func TestTheQuestionShowsWhatWillRun(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{tasksFile: `{
		"inputs": [{"id": "kind", "type": "pickString", "options": [{"label": "Patch", "value": "patch; echo extra"}]}],
		"tasks": [{"label": "bump", "type": "shell", "command": "magex bump:it kind=${input:kind}"}]}`})

	e := r.entry(dir, "tasks:bump")
	assert.Equal(t, "Patch (patch; echo extra)", e.Inputs[0].Options[0].Shown, "the form shows the value that runs")

	_, err := r.svc.Run(t.Context(), RunRequest{
		Dir: r.shown(dir), Ref: e.Ref,
		Inputs: map[string]string{"kind": "patch; echo extra"},
	})
	require.ErrorIs(t, err, ErrNeedsTrust)

	q, ok := errors.AsType[*QuestionError](err)
	require.True(t, ok, "the refusal carries its question")
	assert.Equal(t, Question{
		Ref: e.Ref, Name: "bump", Source: SourceTasks, Command: "magex bump:it kind=patch; echo extra", Trust: TrustAsk,
		SHA256: e.SHA256,
	}, q.Question, "the command with the chosen inputs filled in")
	assert.Equal(t, q.Question, q.Detail())

	id, err := r.svc.Run(t.Context(), RunRequest{
		Dir: r.shown(dir), Ref: e.Ref, Confirmed: q.Question.SHA256,
		Inputs: map[string]string{"kind": "patch; echo extra"},
	})
	require.NoError(t, err, "the answer runs it")
	r.ended(id)

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Inputs: map[string]string{"kind": "a\nb"}})
	require.ErrorIs(t, err, ErrInvalidRequest, "a value that hides what runs")
}
