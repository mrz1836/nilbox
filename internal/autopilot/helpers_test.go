package autopilot_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// startedAt is when the tests' clock starts.
func startedAt() time.Time { return time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC) }

// viewer is the account the tests sign in as, and firstName the name of the routine most
// tests make first.
const (
	viewer    = "octo-user"
	firstName = "First"
)

// accounts is a fake account signed in, which a test may change. hold, when set, holds the
// next read until it is closed, and closes arrived once that read waits.
type accounts struct {
	mu    sync.Mutex
	login string
	err   error

	hold, arrived chan struct{}
}

// ViewerLogin returns the account signed in, once any hold set is let go.
func (a *accounts) ViewerLogin(context.Context) (string, error) {
	a.mu.Lock()
	login, err, hold, arrived := a.login, a.err, a.hold, a.arrived
	a.hold = nil
	a.mu.Unlock()

	if hold != nil {
		close(arrived)
		<-hold
	}

	return login, err
}

// holdNext holds the next read of the account until the returned func lets it go, and
// returns a channel closed once that read waits.
func (a *accounts) holdNext() (<-chan struct{}, func()) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.hold, a.arrived = make(chan struct{}), make(chan struct{})
	hold := a.hold

	return a.arrived, func() { close(hold) }
}

// signIn signs login in.
func (a *accounts) signIn(login string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.login = login
}

// onSettings are Autopilot's defaults with Autopilot (beta) on, as a test that runs it
// needs: a fresh database has it off.
func onSettings() autopilotprefs.Config {
	c := autopilotprefs.Defaults()
	c.Enabled = true

	return c
}

// prefs is fake Autopilot settings.
type prefs struct {
	cfg autopilotprefs.Config
	err error
}

// Autopilot returns the settings.
func (p prefs) Autopilot(context.Context) (autopilotprefs.Config, error) {
	return p.cfg, p.err
}

// names names the starters as the library does.
type names struct{}

// StarterNames returns the starters' nicknames.
func (names) StarterNames(context.Context) (map[string]string, error) {
	return map[string]string{"fix-checks": "Fix checks"}, nil
}

// rig is a Service over a fresh database, with the account signed in, its settings, and
// its copy beside the database.
type rig struct {
	db       *sql.DB
	svc      *autopilot.Service
	file     *autopilot.File
	accounts *accounts
	clock    *testutil.Clock
	logs     *testutil.Logs
}

// newRig returns a rig whose new routines start in mode.
func newRig(t *testing.T, mode autopilotprefs.Mode) *rig {
	t.Helper()

	return rigAt(t, testutil.DB(t), filepath.Join(t.TempDir(), "autopilot.json"), mode)
}

// rigAt returns a rig over db, with its copy at path, and Autopilot on.
func rigAt(t *testing.T, db *sql.DB, path string, mode autopilotprefs.Mode) *rig {
	t.Helper()

	cfg := autopilotprefs.Defaults()
	cfg.Enabled, cfg.DefaultMode = true, mode

	r := &rig{
		db: db, accounts: &accounts{login: viewer}, clock: testutil.NewSteppingClock(startedAt(), time.Second),
		logs: &testutil.Logs{},
	}
	r.file = autopilot.NewFile(path, "v0.0.0-test", r.clock)
	r.svc = autopilot.NewService(autopilot.Config{
		Pool: db, Prefs: prefs{cfg: cfg}, Accounts: r.accounts, Names: names{}, File: r.file, Clock: r.clock,
		Logger: r.logs.Logger(), Offers: rigOffers(),
	})

	return r
}

// rigOffers are the rig's build: every step the catalog offers but the GitHub step, so a
// routine with one (rebaseSpec) stands for a step a build can't run yet, which keeps its
// routine in Dry run.
func rigOffers() *autopilot.Offers {
	out := &autopilot.Offers{}

	for _, st := range catalog.Steps() {
		if st.Offered && st.Kind != catalog.StepGitHub {
			out.Steps = append(out.Steps, st.Kind)
		}
	}

	for _, p := range catalog.Places() {
		if p.Offered {
			out.Places = append(out.Places, p.ID)
		}
	}

	return out
}

// save saves d, which must be saved.
func (r *rig) save(t *testing.T, d autopilot.Draft) autopilot.Routine {
	t.Helper()

	got, err := r.svc.Save(t.Context(), d)
	require.NoError(t, err)

	return got
}

// list returns the routines, which must be read.
func (r *rig) list(t *testing.T) []autopilot.Routine {
	t.Helper()

	got, err := r.svc.List(t.Context())
	require.NoError(t, err)

	return got
}

// storedSpec returns the routine id's spec as stored.
func (r *rig) storedSpec(t *testing.T, id int64) string {
	t.Helper()

	var stored string

	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT spec FROM autopilot_routines WHERE id = ?`, id).
		Scan(&stored))

	return stored
}

// insertRaw stores a routine by hand, as a newer build might have, and returns its ID.
func (r *rig) insertRaw(t *testing.T, name, mode, specJSON string) int64 {
	t.Helper()

	res, err := r.db.ExecContext(t.Context(), `INSERT INTO autopilot_routines
		(account_login, name, name_key, mode, spec, spec_version, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 2, 99, ?, ?)`, viewer, name, autopilot.NameKey(name), mode, specJSON, startedAt(),
		startedAt())
	require.NoError(t, err)

	id, err := res.LastInsertId()
	require.NoError(t, err)

	return id
}

// notifySpec is a routine that tells the user when one of their pull requests' checks
// fail.
func notifySpec() spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{{Lists: []catalog.ListSource{catalog.SourceAuthored}}}},
		Trigger: spec.Trigger{Event: catalog.EventChecksFailed},
		Steps:   []spec.Step{{Notify: &spec.NotifyStep{Note: "Have a look"}}},
	}
}

// agentSpec is a routine that runs the Fix checks starter on the user's failing pull
// requests, in the user's terminal.
func agentSpec() spec.Spec {
	s := notifySpec()
	s.Steps = []spec.Step{{Agent: &spec.AgentStep{Prompt: spec.PromptRef{Starter: "fix-checks"}, Where: catalog.WhereTerminal}}}

	return s
}

// backgroundSpec is agentSpec with its agent in the background, which this build can't
// run yet.
func backgroundSpec() spec.Spec {
	s := agentSpec()
	s.Steps[0].Agent.Where = catalog.WhereBackground

	return s
}

// verifySpec is a routine whose agent pushes, then waits for the checks on its commit.
func verifySpec() spec.Spec {
	s := backgroundSpec()
	s.Steps = append(s.Steps, spec.Step{WaitForChecks: &spec.WaitStep{}, OnlyIf: catalog.OutcomePushed})

	return s
}

// rebaseSpec is a routine with a GitHub step, which the rig's build can't send (rigOffers),
// so it runs in Dry run alone there.
func rebaseSpec() spec.Spec {
	s := notifySpec()
	s.Steps = []spec.Step{{GitHub: []spec.GitHubAction{{Rebase: &spec.Empty{}}}}}

	return s
}

// draft is a new routine named name over s.
func draft(name string, s spec.Spec) autopilot.Draft {
	return autopilot.Draft{Name: name, Spec: s}
}
