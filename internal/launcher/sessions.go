package launcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// promptHeadRunes is how much of a launch's prompt the ledger keeps, to tell the session's
// own conversation from another started in the same folder.
const promptHeadRunes = 200

var (
	// ErrNoLedger is returned by Resume when the Service has no SessionLedger.
	ErrNoLedger = errors.New("sessions are not followed")

	// ErrSessionRunning is returned by Resume for a session that has not ended.
	ErrSessionRunning = errors.New("the session is still running")

	// ErrNotLinked is returned by Resume for a session whose agent never said which
	// conversation it was.
	ErrNotLinked = errors.New("the session was never linked to the agent's own")

	// ErrSessionMoved is returned by Resume for a session whose folder is gone, or would
	// now be a different one.
	ErrSessionMoved = errors.New("the session's folder is gone")

	// ErrNotBackground is returned by TakeOver for a session that isn't a background run's,
	// and by Resume for one asked to resume under a fence it never had.
	ErrNotBackground = errors.New("the session isn't a background run's")

	// ErrNoFence is returned by Resume for a background run's session whose fence is no
	// longer kept, as once its history is cleared: it resumes only with the profile's
	// permissions then, and only when the user asks for those.
	ErrNoFence = errors.New("the run's fence is no longer kept")

	// ErrInvalidResumeAccess is returned for a ResumeAccess this build doesn't know.
	ErrInvalidResumeAccess = errors.New("unknown resume access")
)

// StartedSession is a session a launch started, as the ledger records it. Every field is a
// plain value.
type StartedSession struct {
	// ID is the session's own ID, which the app minted.
	ID string

	// AgentSessionID is the agent's ID of the session, when the launch knows it: the ID
	// passed to an agent that takes one, or the session resumed. It is "" until the app
	// finds it otherwise.
	AgentSessionID string

	// ResumedFrom is the ID of the session this one resumes, or "".
	ResumedFrom string

	// Agent is the agent the session runs, with its profile and the config directory the
	// profile points it at, or "" for the agent's default.
	Agent     agent.ID
	Profile   string
	ConfigDir string

	// Prompt is the nickname of the prompt the session was started with, and PromptHead
	// the start of the prompt itself, or "" for a session resumed.
	Prompt     string
	PromptHead string

	// Terminal is the terminal the session opened in.
	Terminal string

	// ItemID is the item's local ID; Owner, Name, Kind, Number, Title, and URL are the
	// item.
	ItemID int64
	Owner  string
	Name   string
	Kind   item.Kind
	Number int
	Title  string
	URL    string

	// Dir is the directory the session runs in: the item's worktree, named Worktree, in
	// the clone at ClonePath, or the clone itself, when Worktree is "".
	Dir       string
	ClonePath string
	Worktree  string

	// WorktreeAction is what the launch did to that worktree, or "" in the clone.
	WorktreeAction WorktreeAction

	// Permission summarizes the permissions the session starts with, and Notice says what
	// the launch worked around, or is "": the launch's own words for them (LaunchResult).
	Permission string
	Notice     string

	// LaunchedAt is when the terminal was asked to start the session.
	LaunchedAt time.Time
}

// ResumableSession is what Resume needs of a session the ledger recorded.
type ResumableSession struct {
	// ID is the session's own ID, and AgentSessionID the agent's, or "" when never linked.
	ID             string
	AgentSessionID string

	// Agent and Profile are what the session ran with.
	Agent   agent.ID
	Profile string

	// ItemID is the item the session was started on.
	ItemID int64

	// Dir is the directory it ran in, and Worktree the item's worktree there, or "" when
	// it ran in the clone.
	Dir      string
	Worktree string

	// Live reports a session that has not ended, and Carried one whose conversation
	// another session that has not ended carries on, as one resumed from it does.
	Live    bool
	Carried bool

	// RunID is the run in the run history a background run's session is, or 0 for a
	// session in the terminal; Fence is that run's fence, its preset and its own folder,
	// or nil when the run's record no longer holds it.
	RunID int64
	Fence *Fence
}

// ResumeAccess is what a resumed session may do without asking.
type ResumeAccess string

// The accesses a session resumes with.
const (
	// ResumeAsRan resumes a background run's session under its run's fence, and any other
	// with the profile's permissions, as the item's launch settings resolve them now.
	ResumeAsRan ResumeAccess = ""

	// ResumeFence resumes a background run's session under its run's fence, asking the
	// user about whatever the fence doesn't allow, since someone answers now: taking a run
	// over never widens what its conversation may do.
	ResumeFence ResumeAccess = "fence"

	// ResumeProfile resumes it with the profile's permissions, as a click's launch starts,
	// under the trust rule as always: the user's choice for one session.
	ResumeProfile ResumeAccess = "profile"
)

// ResumeRequest asks to resume an ended session. Every field is a plain value.
type ResumeRequest struct {
	// SessionID is the app's ID of the session to resume.
	SessionID string `json:"sessionId"`

	// Access is what the resumed session may do: as it ran, by default.
	Access ResumeAccess `json:"access"`

	// Alongside and Confirmed carry the keys of the holds the user confirmed, as a
	// LaunchRequest's do: a session running in the folder, and the pull request's files.
	Alongside string `json:"alongside"`
	Confirmed string `json:"confirmed"`
}

// TakeOverRequest asks to take a background run over: stop it, if it still works, and
// resume its conversation in the terminal chosen in settings. Every field is a plain value.
type TakeOverRequest struct {
	// SessionID is the app's ID of the run's session.
	SessionID string `json:"sessionId"`

	// Profile widens the resumed session to the profile's permissions, for this one
	// session, as the user ticked; otherwise it keeps the run's fence.
	Profile bool `json:"profile"`

	// Alongside and Confirmed carry the keys of the holds the user confirmed, as a
	// ResumeRequest's do.
	Alongside string `json:"alongside"`
	Confirmed string `json:"confirmed"`
}

// TakeOvers stops the background runs the user takes over: core adapts the background
// runner and Autopilot's engine.
type TakeOvers interface {
	// TakeOver tells Autopilot that the run with runID, its ID in the run history, is taken
	// over, so its match closes as taken over and nothing tells the user it stopped; stops
	// it while it still works, as its Stop does; and returns once its end is recorded, so
	// its conversation is whole on disk.
	TakeOver(ctx context.Context, runID int64) error
}

// RunningSession is a session that has not ended, as a launch held back for it shows it.
// Every field is a plain value.
type RunningSession struct {
	// SessionID is the session's own ID, and Agent its agent.
	SessionID string   `json:"sessionId"`
	Agent     agent.ID `json:"agent"`

	// Doing says what it is doing, after the agent's name: "is working", "needs
	// approval". Since is when it came to that, as RFC 3339.
	Doing string `json:"doing"`
	Since string `json:"since"`

	// Dir is the folder it runs in, with ~ for home, and Worktree the item's worktree
	// there, or "" for the clone.
	Dir      string `json:"dir"`
	Worktree string `json:"worktree"`

	// Repo is "owner/name" and Number the item the session was started on, which may be
	// another than the launch's when both run in the clone.
	Repo   string `json:"repo"`
	Number int    `json:"number"`

	// CanFocus reports a session whose terminal tab can be brought forward.
	CanFocus bool `json:"canFocus"`

	// Background reports an agent's run in the background, which the app started itself: a
	// launch there is always held back, never started beside it.
	Background bool `json:"background"`
}

// SessionLedger records the sessions launches start, so the app can follow each from its
// item. The app adapts the sessions service to it.
type SessionLedger interface {
	// Started records a session a launch just started.
	Started(ctx context.Context, s StartedSession) error

	// Resumable returns a recorded session, for resuming it.
	Resumable(ctx context.Context, id string) (ResumableSession, error)

	// Running returns the sessions that have not ended running in the folder dir, whichever
	// item each was started on, newest first; none while the app does not follow sessions.
	Running(ctx context.Context, dir string) ([]RunningSession, error)
}

// newSessionID mints a session's own ID: a random UUID.
func newSessionID() string {
	return uuid.NewString()
}

// sessionRef is the session a launch starts or resumes: id is minted for a new one, and
// resume is the agent's ID of the one it resumes; fence is the fence a background run's
// conversation resumes under, taken over, or nil.
type sessionRef struct {
	id     string
	resume string
	fence  *Fence
}

// Resume reopens an ended session in the terminal chosen in settings: the agent picks
// the conversation up where it stopped, in the same folder, with the item's launch
// settings as they now resolve for the agent and profile the session ran with. A
// background run's session resumes under its run's fence, asking about the rest, unless
// req asks for the profile's permissions (ResumeAccess). The session resumed is recorded
// as a new session that names the first, and nothing is fetched: the item's worktree must
// still be there. It is held back as a launch is, for a session running in the folder and
// for the pull request's files as last checked, until req confirms each. It returns
// ErrSessionRunning for a session whose conversation still runs, ErrNotLinked for one the
// agent never named, and ErrNoFence for a run's whose fence is no longer kept.
func (s *Service) Resume(ctx context.Context, req ResumeRequest) (LaunchResult, error) {
	prev, fenced, err := s.resumeOf(ctx, req)
	if err != nil {
		return LaunchResult{}, err
	}

	launch := LaunchRequest{
		ItemID: prev.ItemID, Agent: prev.Agent, Profile: prev.Profile, Worktree: new(prev.Worktree != ""),
		Alongside: req.Alongside,
	}
	ref := sessionRef{id: s.newID(), resume: prev.AgentSessionID, fence: fenced}

	p, err := s.plan(ctx, launch, planResume, ref)
	if errors.Is(err, item.ErrNotFound) {
		return LaunchResult{}, uierr.New("This item is no longer in your inbox, so its session can't be resumed "+
			"from here. Copy the session's ID to resume it yourself.", err)
	}

	if err != nil {
		return LaunchResult{}, err
	}

	if p.worktree != nil && p.worktree.Action != WorktreeResume || !paths.Same(p.plan.Dir, prev.Dir) {
		return LaunchResult{}, uierr.New(fmt.Sprintf("The session's folder, %s, is gone, so it can't be resumed.",
			paths.Tilde(prev.Dir, s.home)), ErrSessionMoved)
	}

	if hold := s.heldBack(p, req.Confirmed, nil); hold != nil {
		return LaunchResult{Hold: hold}, nil
	}

	return s.start(ctx, p, ref, prev.ID)
}

// resumeOf reads the session req resumes, checks it can be, and works out the fence it
// resumes under, or nil for the profile's permissions.
func (s *Service) resumeOf(ctx context.Context, req ResumeRequest) (ResumableSession, *Fence, error) {
	if s.ledger == nil {
		return ResumableSession{}, nil, ErrNoLedger
	}

	prev, err := s.ledger.Resumable(ctx, req.SessionID)
	if err != nil {
		return ResumableSession{}, nil, err
	}

	if err = resumable(prev); err != nil {
		return ResumableSession{}, nil, err
	}

	fenced, err := resumeFence(prev, req.Access)

	return prev, fenced, err
}

// TakeOver takes a background run over, as the run view's and the session panel's Take
// over do: it stops the run while it still works, through TakeOvers, which tells
// Autopilot the run is taken over and waits for its end, then resumes its conversation as
// Resume does, in the same worktree, with the same profile, in the terminal chosen in
// settings. The resumed session keeps the run's fence, asking about whatever it doesn't
// allow, unless req widens it to the profile's permissions. A hold comes back as Resume's
// does, for the window to confirm and take over again. It returns ErrNotBackground for a
// session in the terminal.
func (s *Service) TakeOver(ctx context.Context, req TakeOverRequest) (LaunchResult, error) {
	if s.ledger == nil {
		return LaunchResult{}, ErrNoLedger
	}

	prev, err := s.ledger.Resumable(ctx, req.SessionID)
	if err != nil {
		return LaunchResult{}, err
	}

	if prev.RunID == 0 {
		return LaunchResult{}, uierr.New("Only a background run can be taken over. Resume this session instead.",
			ErrNotBackground)
	}

	if prev.AgentSessionID == "" {
		return LaunchResult{}, resumable(ResumableSession{})
	}

	if s.takeOvers != nil {
		if err = s.takeOvers.TakeOver(ctx, prev.RunID); err != nil {
			return LaunchResult{}, err
		}
	}

	access := ResumeFence
	if req.Profile {
		access = ResumeProfile
	}

	return s.Resume(ctx, ResumeRequest{
		SessionID: req.SessionID, Access: access, Alongside: req.Alongside, Confirmed: req.Confirmed,
	})
}

// resumeFence returns the fence prev resumes under for access, or nil to resume with the
// profile's permissions: a background run's own fence, taken over, unless access asks for
// the profile's. A fence whose settings are gone from the run's folder, as once its history
// is cleared, can't be kept, and the session resumes only when the user asks for the
// profile's permissions.
func resumeFence(prev ResumableSession, access ResumeAccess) (*Fence, error) {
	fenced, err := fenceAccess(prev, access)
	if err != nil || !fenced {
		return nil, err
	}

	gone := uierr.New("This run's fence is no longer kept, so its conversation can't carry on under it. "+
		"Use the profile's permissions to take it over anyway.", ErrNoFence)

	if prev.Fence == nil || !filepath.IsAbs(prev.Fence.RunDir) {
		return nil, gone
	}

	if info, statErr := os.Stat(filepath.Join(prev.Fence.RunDir, agent.FenceSettingsName)); statErr != nil ||
		!info.Mode().IsRegular() {
		return nil, gone
	}

	return &Fence{Preset: prev.Fence.Preset, RunDir: prev.Fence.RunDir, TakenOver: true}, nil
}

// fenceAccess reports whether prev resumes under its run's fence for access: a background
// run's session does, unless access asks for the profile's permissions, and any other
// never does.
func fenceAccess(prev ResumableSession, access ResumeAccess) (bool, error) {
	switch access {
	case ResumeProfile:
		return false, nil
	case ResumeAsRan:
		return prev.RunID != 0, nil
	case ResumeFence:
		if prev.RunID == 0 {
			return false, uierr.New("Only a background run's session keeps a fence.", ErrNotBackground)
		}

		return true, nil
	default:
		return false, fmt.Errorf("%w %q", ErrInvalidResumeAccess, access)
	}
}

// runningHold holds a planned launch back while a session runs in the folder it would
// start in, the item's worktree or its clone, whichever item that session was started
// on, unless alongside confirms exactly those sessions; and always while one of the app's
// own runs works there, which the ledger leaves out while sessions aren't followed. Only a
// launch or a resume is held back, which holds reports; a preview never is. A ledger that
// cannot be read holds nothing back for its sessions, with a warning: following sessions
// never stops a launch.
func (s *Service) runningHold(ctx context.Context, p planned, alongside string, holds bool) *Hold {
	if !holds {
		return nil
	}

	var running []RunningSession

	if s.ledger != nil {
		got, err := s.ledger.Running(ctx, p.plan.Dir)
		if err != nil {
			s.logger.WarnContext(ctx, "could not read the sessions running in the folder", "error", err)
		}

		running = got
	}

	if s.runs != nil {
		running = joinRunning(running, s.runs.RunsIn(ctx, p.plan.Dir))
	}

	return holdRunning(running, p.worktreeName(), alongside)
}

// joinRunning returns the sessions of ledger with those of runs it doesn't hold, each
// session once: a background run's session the ledger follows is the run's.
func joinRunning(ledger, runs []RunningSession) []RunningSession {
	out := slices.Clone(ledger)

	for _, r := range runs {
		i := slices.IndexFunc(out, func(s RunningSession) bool { return s.SessionID != "" && s.SessionID == r.SessionID })
		if i >= 0 {
			out[i].Background = true

			continue
		}

		out = append(out, r)
	}

	return out
}

// holdRunning is the hold for running, the sessions running in the folder a launch would
// start in, the item's worktree or the clone for "", or nil when none does or alongside
// confirms exactly them.
func holdRunning(running []RunningSession, worktree, alongside string) *Hold {
	if len(running) == 0 {
		return nil
	}

	ids := make([]string, 0, len(running))
	for _, r := range running {
		ids = append(ids, r.SessionID)
	}

	slices.Sort(ids)

	key := string(HoldRunning) + ":" + strings.Join(ids, ",")
	if key == alongside && !slices.ContainsFunc(running, func(r RunningSession) bool { return r.Background }) {
		return nil
	}

	return &Hold{
		Key: key, Reason: HoldRunning, Running: running, Dir: running[0].Dir, Worktree: worktree,
		Files: []sensitive.Finding{},
	}
}

// resumable checks that a recorded session can be resumed.
func resumable(prev ResumableSession) error {
	switch {
	case prev.Live && prev.RunID != 0:
		return uierr.New("Autopilot's agent is still working on this. Use Take over to stop it and carry on in your "+
			"terminal.", ErrSessionRunning)
	case prev.Live:
		return uierr.New("This session is still running. Use Show in terminal to go to it.", ErrSessionRunning)
	case prev.Carried:
		return uierr.New("This conversation is already running in another session. Use Show in terminal on that "+
			"session to go to it.", ErrSessionRunning)
	case prev.AgentSessionID == "":
		return uierr.New("The agent never reported this session's conversation, so there is nothing to resume.",
			ErrNotLinked)
	default:
		return nil
	}
}

// start trusts the session's folder, writes its files, opens them in the terminal, and
// records the session: the worktree's latest session and the ledger's new one, which
// resumedFrom names the session it resumes, or is "".
func (s *Service) start(ctx context.Context, p planned, ref sessionRef, resumedFrom string) (LaunchResult, error) {
	if err := s.trustDir(ctx, p); err != nil {
		return LaunchResult{}, err
	}

	now := s.clock.Now()

	files, err := WriteLaunchFiles(s.paths, p.plan, p.prompt, now, s.logger)
	if err != nil {
		return LaunchResult{}, err
	}

	if err = p.driver.Open(ctx, files.Script); err != nil {
		return LaunchResult{}, err
	}

	s.recordSession(ctx, p)
	s.recordStarted(ctx, p, ref, resumedFrom, now)

	res := LaunchResult{
		ScriptPath: files.Script,
		ClonePath:  paths.Tilde(p.clone.Dir, s.home),
		Dir:        paths.Tilde(p.plan.Dir, s.home),
		Worktree:   p.worktreeName(),
		Prompt:     p.res.Prompt.Nickname,
		Profile:    p.res.Profile.Value,
		Permission: p.plan.Permission,
		Terminal:   p.driver.ID(),
		Agent:      p.res.Agent.ID,
		SessionID:  ref.id,
	}

	if p.worktree != nil {
		res.WorktreeAction, res.Notice = p.worktree.Action, p.worktree.Notice
	}

	return res, nil
}

// recordStarted records the session in the ledger, so the app follows it. The session has
// started by then, and a missing record only means its card does not follow it, so a
// failure is logged rather than returned.
func (s *Service) recordStarted(ctx context.Context, p planned, ref sessionRef, resumedFrom string, at time.Time) {
	if s.ledger == nil {
		return
	}

	started := StartedSession{
		ID:          ref.id,
		ResumedFrom: resumedFrom,
		Agent:       p.res.Agent.ID,
		Profile:     p.res.Profile.Value,
		ConfigDir:   p.plan.Env[p.res.Agent.ConfigEnv],
		Prompt:      p.res.Prompt.Nickname,
		PromptHead:  textutil.Clip(p.prompt, promptHeadRunes),
		Terminal:    string(p.driver.ID()),
		ItemID:      p.row.ID,
		Owner:       p.row.Owner,
		Name:        p.row.Name,
		Kind:        p.row.Kind,
		Number:      p.row.Number,
		Title:       p.row.Title,
		URL:         p.row.URL,
		Dir:         p.plan.Dir,
		ClonePath:   p.clone.Dir,
		Worktree:    p.worktreeName(),
		Permission:  p.plan.Permission,
		LaunchedAt:  at,
	}

	if p.worktree != nil {
		started.WorktreeAction, started.Notice = p.worktree.Action, p.worktree.Notice
	}

	switch {
	case ref.resume != "":
		started.AgentSessionID, started.Prompt = ref.resume, ""
	case p.res.Agent.SessionIDs:
		started.AgentSessionID = ref.id
	}

	if err := s.ledger.Started(ctx, started); err != nil {
		s.logger.WarnContext(ctx, "could not record the session to follow it", "error", err)
	}
}
