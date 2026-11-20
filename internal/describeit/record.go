package describeit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// What a conversation's record keeps, and for how long.
const (
	// recordVersion is the record's format version.
	recordVersion = 1

	// maxRecordBytes caps a record as it is read: a few hundred turns, each with its
	// words and the agent's reply.
	maxRecordBytes = 32 << 20

	// keepConversations is how long a conversation is kept from its start: one that started
	// earlier is gone, its folder and its proposals with it.
	keepConversations = 30 * 24 * time.Hour
)

// errBadRecord is returned for a record that isn't the conversation its folder names.
var errBadRecord = errors.New("the conversation's record can't be read")

// record is a conversation as conversation.json keeps it: what the window shows of it,
// and what its next turn needs.
type record struct {
	Version int    `json:"version"`
	ID      string `json:"id"`

	// Agent is the agent it runs with, as chosen, and Profile, Model, and Effort what that
	// came to, Inherit resolved, as it was made or its last turn started.
	Agent   AgentChoice `json:"agent"`
	Profile string      `json:"profile"`
	Model   string      `json:"model"`
	Effort  string      `json:"effort"`

	CreatedAt time.Time `json:"createdAt"`

	// Started reports the agent's session started: a turn of it got as far as the agent's
	// own start, so the next resumes it.
	Started bool `json:"started"`

	// Told are the outcomes of its cards a turn told the agent of, by outcomeKey.
	Told []string `json:"told"`

	Turns []turnRecord `json:"turns"`
}

// turnRecord is one of a conversation's turns, as its record keeps it.
type turnRecord struct {
	N int `json:"n"`

	Text     string       `json:"text"`
	Attach   []Attachment `json:"attach"`
	Feedback string       `json:"feedback"`

	State TurnState `json:"state"`
	Error string    `json:"error"`

	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`

	Reply string `json:"reply"`

	// Cost is what the turn alone cost, and Total what the conversation cost so far as its
	// result said, nil for none; Estimated says both are estimates.
	Cost      *float64 `json:"cost"`
	Total     *float64 `json:"total"`
	Estimated bool     `json:"estimated"`

	TokensIn  int64 `json:"tokensIn"`
	TokensOut int64 `json:"tokensOut"`

	// Proposals are the IDs of the proposals the agent made in it, in the order they came.
	Proposals []string `json:"proposals"`
}

// newRecord returns a conversation with no turns yet, made at now, with the agent a, Inherit
// resolved as start says.
func newRecord(id string, a AgentChoice, start Start, now time.Time) *record {
	r := &record{Version: recordVersion, ID: id, Agent: a, CreatedAt: now.UTC(), Told: []string{}, Turns: []turnRecord{}}
	r.resolve(start)

	return r
}

// resolve notes what the record's agent comes to, Inherit resolved as start says.
func (r *record) resolve(start Start) {
	r.Profile = start.Profile
	if r.Profile == "" {
		r.Profile = r.Agent.Profile
	}

	r.Model, r.Effort = resolved(r.Agent.Model, start.Model), resolved(r.Agent.Effort, start.Effort)
}

// resolved returns the choice, or what inheriting it comes to for "".
func resolved(choice, inherited string) string {
	if choice != "" {
		return choice
	}

	return inherited
}

// expired reports a conversation that started more than keepConversations before now.
func (r *record) expired(now time.Time) bool {
	return now.Sub(r.CreatedAt) > keepConversations
}

// turn returns the record's turn n, or nil.
func (r *record) turn(n int) *turnRecord {
	if n < 1 || n > len(r.Turns) {
		return nil
	}

	return &r.Turns[n-1]
}

// proposals returns every proposal ID its turns made.
func (r *record) proposals() map[string]int {
	ids := map[string]int{}

	for _, t := range r.Turns {
		for _, id := range t.Proposals {
			ids[id] = t.N
		}
	}

	return ids
}

// clone returns a copy of the record that shares nothing it may change.
func (r *record) clone() record {
	c := *r
	c.Told = slices.Clone(r.Told)
	c.Turns = slices.Clone(r.Turns)

	for i := range c.Turns {
		c.Turns[i].Attach = slices.Clone(c.Turns[i].Attach)
		c.Turns[i].Proposals = slices.Clone(c.Turns[i].Proposals)
	}

	return c
}

// readRecord reads the record of the conversation id, which must be the one its folder
// names: a file of the app's own, read as an untrusted one is.
func (s *Service) readRecord(id string) (*record, error) {
	data, err := readFile(s.recordFile(id), maxRecordBytes)
	if err != nil {
		return nil, err
	}

	var r record
	if err = json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%w: %w", errBadRecord, err)
	}

	if r.ID != id || r.Version != recordVersion {
		return nil, fmt.Errorf("%w: it names conversation %q, version %d", errBadRecord, textutil.Truncate(r.ID, 40),
			r.Version)
	}

	r.Told = textutil.NonNil(r.Told)
	r.Turns = textutil.NonNil(r.Turns)

	for i := range r.Turns {
		t := &r.Turns[i]
		t.N = i + 1
		t.Attach = textutil.NonNil(t.Attach)
		t.Proposals = slices.DeleteFunc(textutil.NonNil(t.Proposals), func(p string) bool { return !kit.ValidProposalID(p) })
	}

	return &r, nil
}

// saveRecord writes the conversation's record whole.
func (s *Service) saveRecord(r *record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode the conversation's record: %w", err)
	}

	if err = ensureDir(s.conversationDir(r.ID)); err != nil {
		return err
	}

	return appdata.WriteFile(s.recordFile(r.ID), data)
}

// conversationIDs returns the IDs of the conversations kept, by their folders' names.
func (s *Service) conversationIDs() []string {
	entries, err := os.ReadDir(s.path(conversationsName))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.logger.Warn("describe it couldn't list its conversations", "error", err)
		}

		return []string{}
	}

	ids := make([]string, 0, len(entries))

	for _, e := range entries {
		if e.IsDir() && agent.ValidSessionID(e.Name()) {
			ids = append(ids, e.Name())
		}
	}

	return ids
}

// currentLocked returns the current conversation, nil for none, looking for it the first
// time: the newest of the conversations kept, by when each started. The caller holds mu.
func (s *Service) currentLocked() *record {
	if s.looked {
		return s.rec
	}

	s.looked = true

	for _, id := range s.conversationIDs() {
		r, err := s.readRecord(id)
		if err != nil {
			s.logger.Warn("describe it couldn't read a conversation", "conversation", id, "error", err)

			continue
		}

		if s.rec == nil || r.CreatedAt.After(s.rec.CreatedAt) || (r.CreatedAt.Equal(s.rec.CreatedAt) && r.ID > s.rec.ID) {
			s.rec = r
		}
	}

	return s.rec
}

// removeConversation removes the conversation id's folder and its proposals.
func (s *Service) removeConversation(id string) {
	for _, dir := range []string{s.conversationDir(id), s.proposalsDir(id)} {
		if err := os.RemoveAll(dir); err != nil {
			s.logger.Warn("describe it couldn't remove an old conversation", "conversation", id, "error", err)
		}
	}
}
