package describeit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun/stream"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/mcpserver"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// What a message and a turn may be.
const (
	// maxMessageRunes caps what the user writes in one message.
	maxMessageRunes = 20000

	// maxAttachments caps what one message attaches, and maxAttachmentBytes the words of
	// each.
	maxAttachments     = 10
	maxAttachmentBytes = 64 << 10

	// maxLabelRunes caps an attachment's chip.
	maxLabelRunes = 120

	// turnBudgetUSD is what one turn may spend, which Claude Code keeps to itself: a turn's,
	// never the conversation's (the spike's S5).
	turnBudgetUSD = 2.0

	// sessionSuffix ends the name a conversation's session goes by in the user's own lists
	// of Claude Code's sessions, after the app's name.
	sessionSuffix = ": Describe it"
)

// Send sends m as the current conversation's next turn, making a conversation with
// Agent's agent when there is none, and returns at once with the turn running: the
// window follows it through describeit:updated and Events. The turn's prompt is m's text,
// its attachments' words (Attachments), and what the user did with the cards since the
// last turn (kit.OutcomeWords); a fresh snapshot is written first. It is refused while a
// turn runs, for an empty message, and while Status isn't ready, each worded.
func (s *Service) Send(ctx context.Context, m Message) (Conversation, error) {
	text := strings.TrimSpace(strings.ReplaceAll(m.Text, "\x00", ""))
	if err := checkMessage(text, m.Attach); err != nil {
		return emptyConversation(), err
	}

	lt, err := s.reserve()
	if err != nil {
		return emptyConversation(), err
	}

	plan, err := s.planTurn(ctx, text, m.Attach)
	if err != nil {
		s.release(lt)

		return emptyConversation(), err
	}

	return s.startTurn(ctx, lt, plan)
}

// checkMessage refuses a message with nothing to say, or too much.
func checkMessage(text string, refs []AttachRef) error {
	switch {
	case text == "":
		return uierr.New("Write what you want Autopilot to do.", errEmpty)
	case utf8.RuneCountInString(text) > maxMessageRunes:
		return uierr.New(fmt.Sprintf("Keep a message under %d characters.", maxMessageRunes), errTooLong)
	case len(refs) > maxAttachments:
		return uierr.New(fmt.Sprintf("Attach at most %d things to a message.", maxAttachments), errTooLong)
	default:
		return nil
	}
}

// reserve makes lt the turn running, while none runs and the app isn't quitting, so no
// other turn starts while it gets ready.
func (s *Service) reserve() (*liveTurn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case s.closing:
		return nil, uierr.New(closingWords, errClosing)
	case s.live != nil:
		return nil, uierr.New(busyWords, errBusy)
	}

	lt := &liveTurn{done: make(chan struct{})}
	s.live = lt

	return lt, nil
}

// release lets go of the turn lt reserved, which never started.
func (s *Service) release(lt *liveTurn) {
	s.mu.Lock()

	if s.live == lt {
		s.live = nil
	}

	s.mu.Unlock()
	close(lt.done)
}

// turnPlan is a turn ready to start: its conversation (a new one not yet kept, when there was none), how to
// start its agent, what it attaches, and what it tells the agent of the cards.
type turnPlan struct {
	rec      *record
	start    Start
	exe      string
	text     string
	attach   []Attachment
	feedback string
	told     []string
}

// planTurn readies everything a turn needs before its agent starts: its conversation, the
// agent, its tools' program, the attachments, the snapshot, and the instructions.
func (s *Service) planTurn(ctx context.Context, text string, refs []AttachRef) (turnPlan, error) {
	rec := s.carriedOn()
	plan := turnPlan{rec: rec, text: text, exe: s.executable()}

	var choice AgentChoice
	if rec != nil {
		choice = rec.Agent
	} else {
		choice = s.agentChoice(ctx)
	}

	start, err := s.conversationStart(ctx, choice.Profile)
	if ready, problem, fix := readiness(start, err); !ready {
		return turnPlan{}, uierr.New(strings.TrimSpace(problem+" "+fix), errors.Join(errNotReady, err))
	}

	plan.start = start

	if plan.exe == "" {
		return turnPlan{}, uierr.New(brand.Name+" can't find its own program to give "+agentWords+" its tools.",
			errNoExecutable)
	}

	if plan.attach, err = s.attachments(ctx, refs); err != nil {
		return turnPlan{}, err
	}

	if err = s.readyFolder(ctx); err != nil {
		return turnPlan{}, err
	}

	if rec == nil {
		plan.rec = newRecord(uuid.NewString(), choice, start, s.clock.Now())
	}

	plan.feedback, plan.told = s.feedback(ctx, plan.rec)

	return plan, nil
}

// carriedOn returns the conversation a message carries on: the current one, unless it
// started more than keepConversations ago, when it is gone and the message starts another.
func (s *Service) carriedOn() *record {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := s.currentLocked()
	if rec != nil && rec.expired(s.clock.Now()) {
		s.removeConversation(rec.ID)
		s.rec, rec = nil, nil
	}

	return rec
}

// readyFolder readies the describe folder for a turn: the folder it runs in, the snapshot
// written fresh, and the instructions.
func (s *Service) readyFolder(ctx context.Context) error {
	if err := ensureDir(s.workDir()); err != nil {
		return uierr.New("Describe it couldn't make the folder "+agentWords+" runs in.", err)
	}

	s.forget()

	if err := s.writeContext(ctx); err != nil {
		return err
	}

	return s.writeInstructions()
}

// attachments reads what a message attaches, through Attachments.
func (s *Service) attachments(ctx context.Context, refs []AttachRef) ([]Attachment, error) {
	out := make([]Attachment, 0, len(refs))
	if len(refs) == 0 {
		return out, nil
	}

	if s.cfg.Attachments == nil {
		return nil, uierr.New("Describe it can't attach anything in this build.", errNoAttachments)
	}

	for _, ref := range refs {
		a, err := s.attachment(ctx, ref)
		if err != nil {
			return nil, err
		}

		out = append(out, a)
	}

	return out, nil
}

// attachment reads one thing a message attaches, through Attachments: its chip one short
// line, and its words within maxAttachmentBytes.
func (s *Service) attachment(ctx context.Context, ref AttachRef) (Attachment, error) {
	var (
		a   Attachment
		err error
	)

	switch ref.Kind {
	case AttachRoutine:
		a, err = s.cfg.Attachments.Routine(ctx, ref.ID)
	case AttachItem:
		a, err = s.cfg.Attachments.Item(ctx, ref.ID)
	default:
		return Attachment{}, uierr.New("Describe it can't attach that.", fmt.Errorf("%w: %q", errAttachKind, ref.Kind))
	}

	if err != nil {
		if _, worded := errors.AsType[*uierr.Error](err); worded {
			return Attachment{}, err
		}

		return Attachment{}, uierr.New("Describe it couldn't read what you attached.", err)
	}

	a.Kind, a.ID = ref.Kind, ref.ID
	a.Label = textutil.Truncate(textutil.OneLine(a.Label), maxLabelRunes)
	a.Words = strings.TrimSpace(strings.ReplaceAll(a.Words, "\x00", ""))

	if len(a.Words) > maxAttachmentBytes {
		a.Words = strings.ToValidUTF8(a.Words[:maxAttachmentBytes], "")
	}

	return a, nil
}

// feedback returns what the user did with the conversation's cards that no turn told the
// agent yet, as the next turn tells it (kit.OutcomeWords), and their keys.
func (s *Service) feedback(ctx context.Context, rec *record) (string, []string) {
	told := map[string]bool{}
	for _, key := range rec.Told {
		told[key] = true
	}

	var (
		items []kit.Outcome
		keys  []string
	)

	for _, o := range s.outcomes(ctx, rec.ID) {
		key := outcomeKey(o.Proposal, o.Part, o.Index)
		if told[key] {
			continue
		}

		told[key] = true

		items = append(items, o)
		keys = append(keys, key)
	}

	return kit.OutcomeWords(items), keys
}

// promptText is a turn's prompt: what the user did with the cards first, as the agent's
// instructions say a message may start, then what they wrote, then what they attached.
func promptText(feedback, text string, attach []Attachment) string {
	parts := make([]string, 0, 2+len(attach))

	if feedback != "" {
		parts = append(parts, feedback)
	}

	parts = append(parts, text)

	for _, a := range attach {
		if a.Words != "" {
			parts = append(parts, a.Words)
		}
	}

	return strings.Join(parts, "\n\n") + "\n"
}

// startTurn starts the planned turn as the turn lt reserved: its record kept, its files
// written, and its agent started with its follower, and returns the conversation with it
// running. A turn whose agent couldn't start is kept as failed, and the error says why.
func (s *Service) startTurn(ctx context.Context, lt *liveTurn, plan turnPlan) (Conversation, error) {
	rec, now := plan.rec, s.clock.Now().UTC()
	n := len(rec.Turns) + 1

	s.mu.Lock()

	// A turn a crash left running, which the worker didn't close yet, is closed first.
	s.closeInterrupted(rec)

	rec.Turns = append(rec.Turns, turnRecord{
		N: n, Text: plan.text, Attach: plan.attach, Feedback: plan.feedback, State: TurnRunning, StartedAt: now,
		Proposals: []string{},
	})
	rec.resolve(plan.start)
	s.rec, s.looked = rec, true
	lt.rec, lt.n, lt.log, lt.resumed = rec, n, s.turnLog(rec.ID, n), rec.Started
	s.saveLocked(rec)

	s.mu.Unlock()

	proc, kill, err := s.launch(ctx, lt, plan)
	if err != nil {
		s.mu.Lock()

		if t := rec.turn(n); t != nil {
			t.State, t.Error, t.EndedAt = TurnFailed, uierr.Message(err), s.clock.Now().UTC()
		}

		s.saveLocked(rec)
		s.mu.Unlock()

		s.release(lt)
		s.updated(rec.ID, n)

		return s.view(ctx, s.copyOf(rec), false), err
	}

	s.mu.Lock()

	lt.proc, lt.kill, lt.parser = proc, kill, stream.Claude(s.workDir())

	rec.Told = append(rec.Told, plan.told...)
	s.saveLocked(rec)

	if s.closing {
		lt.quit.Store(true)
	}

	stopNow := lt.stopped.Load() || lt.quit.Load()
	copied := rec.clone()

	s.mu.Unlock()

	if stopNow {
		lt.stop(proc)
	}

	go s.follow(lt)

	s.updated(rec.ID, n)

	return s.view(ctx, copied, true), nil
}

// copyOf returns a copy of rec, read under mu.
func (s *Service) copyOf(rec *record) record {
	s.mu.Lock()
	defer s.mu.Unlock()

	return rec.clone()
}

// launch writes the turn's files and starts its agent: its MCP config, its prompt on its
// stdin, its stream on its stdout and stderr, in the one folder every turn runs in.
func (s *Service) launch(ctx context.Context, lt *liveTurn, plan turnPlan) (process, context.CancelFunc, error) {
	id := plan.rec.ID

	args, err := s.turnArgs(plan, lt.resumed)
	if err != nil {
		return nil, nil, uierr.New("Describe it couldn't ready "+agentWords+"'s turn.", err)
	}

	if err = ensureDir(s.proposalsDir(id)); err != nil {
		return nil, nil, uierr.New("Describe it couldn't make the folder its proposals go in.", err)
	}

	before, err := proposalIDs(s.proposalsDir(id))
	if err != nil {
		return nil, nil, uierr.New("Describe it couldn't read the folder its proposals go in.", err)
	}

	lt.before = make(map[string]bool, len(before))
	for _, p := range before {
		lt.before[p] = true
	}

	if err = appdata.WriteFile(s.turnPrompt(id, lt.n), []byte(promptText(plan.feedback, plan.text, plan.attach))); err != nil {
		return nil, nil, uierr.New("Describe it couldn't write your message for "+agentWords+".", err)
	}

	stdin, err := paths.OpenRegular(s.turnPrompt(id, lt.n))
	if err != nil {
		return nil, nil, uierr.New("Describe it couldn't read your message back.", err)
	}

	defer func() { _ = stdin.Close() }()

	out, err := os.OpenFile(lt.log, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, appdata.PrivateFilePerm)
	if err != nil {
		return nil, nil, uierr.New("Describe it couldn't make the file "+agentWords+" writes to.", err)
	}

	defer func() { _ = out.Close() }()

	// The turn outlives the call that starts it: its process ends with it, or with kill.
	turnCtx, kill := context.WithCancel(context.WithoutCancel(ctx))

	proc, err := startProcess(turnCtx, execpath.Cmd{
		Path: plan.start.Program, Args: args, Dir: s.workDir(), Env: plan.start.Env, Output: out, Stdin: stdin,
	})
	if err != nil {
		kill()

		return nil, nil, uierr.New(agentWords+" couldn't start, so the turn didn't run.", err)
	}

	return proc, kill, nil
}

// turnArgs writes the turn's MCP config, which names the app's own tool mode for its
// conversation alone, and returns the turn's arguments: resuming its session, or starting it.
// The turn runs the model and effort its conversation's header shows, the record's with
// Inherit resolved as the turn's start says (record.resolve), so a choice of Inherit runs the
// profile's own, as a background run does, never Claude Code's default in its place.
func (s *Service) turnArgs(plan turnPlan, resume bool) ([]string, error) {
	id := plan.rec.ID

	files, err := agent.ConversationFiles(agent.Claude, agent.ConversationSpec{
		Server: brand.Slug, Command: plan.exe, Args: []string{mcpserver.Arg(s.cfg.Dir), mcpserver.ConversationArg(id)},
	})
	if err != nil {
		return nil, err
	}

	for _, f := range files {
		if _, err = writeIfChanged(filepath.Join(s.conversationDir(id), filepath.FromSlash(f.Path)), f.Data); err != nil {
			return nil, err
		}
	}

	return agent.ConversationArgs(agent.Claude, agent.ConversationOptions{
		SessionID: id, Resume: resume, Name: brand.Name + sessionSuffix, Model: plan.rec.Model, Effort: plan.rec.Effort,
		MCPConfig: filepath.Join(s.conversationDir(id), agent.MCPConfigName), SystemPromptFile: s.instructionsFile(),
		Server: brand.Slug, BudgetUSD: turnBudgetUSD,
	})
}

// emptyConversation is no conversation: every list there, none nil.
func emptyConversation() Conversation {
	return Conversation{Turns: []Turn{}}
}
