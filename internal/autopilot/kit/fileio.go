package kit

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// What the shared files may be.
const (
	// MaxProposalFileBytes caps a proposal file: a proposal at its largest, with room for
	// what writing its text again may add, and the file's own fields.
	MaxProposalFileBytes = 2 * MaxProposalBytes

	// MaxOutcomesBytes caps an outcomes file, which holds a line or so for each part of
	// each proposal in its folder.
	MaxOutcomesBytes = 4 << 20

	// MaxOutcomeRunes caps what an outcome says.
	MaxOutcomeRunes = 300

	// proposalFileDepth fits a proposal at its deepest under the file that holds it.
	proposalFileDepth = 1 + proposalDepth

	// outcomesDepth is how deep an outcomes file nests: the file, its items, an item.
	outcomesDepth = 3

	// proposalIDTime is the layout of a proposal ID's moment, in UTC.
	proposalIDTime = "20060102T150405Z"
)

var (
	// ErrUnreadableProposalFile is returned for a proposal file that isn't one: not JSON,
	// of another version, a key it doesn't have or doesn't spell exactly, a field that
	// isn't one, or a proposal that can't be read (ErrUnreadableProposal).
	ErrUnreadableProposalFile = errors.New("the proposal file can't be read")

	// ErrUnreadableOutcomes is returned for an outcomes file that isn't one, or one holding
	// an outcome that isn't one.
	ErrUnreadableOutcomes = errors.New("the outcomes can't be read")
)

// proposalIDPattern is what a proposal's ID looks like: its moment in UTC, a hyphen, and
// 12 lowercase hex digits.
var proposalIDPattern = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{12}$`)

// conversationPattern is what an in-app conversation's ID looks like: a UUID.
var conversationPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// NewProposalID returns a new proposal's ID, which names its file: now, in UTC, to the
// second, a hyphen, and 12 lowercase hex digits from 6 bytes of random, so two sessions
// writing in one second never pick one name ("20261120T053103Z-9f2c4100ab31"). It fails
// only when random can't give 6 bytes.
func NewProposalID(now time.Time, random io.Reader) (string, error) {
	var b [6]byte

	if _, err := io.ReadFull(random, b[:]); err != nil {
		return "", fmt.Errorf("make a proposal's ID: %w", err)
	}

	return now.UTC().Format(proposalIDTime) + "-" + hex.EncodeToString(b[:]), nil
}

// ValidProposalID reports an ID NewProposalID could have made, which a file's name may
// hold.
func ValidProposalID(id string) bool {
	return proposalIDPattern.MatchString(id)
}

// EncodeProposalFile writes f as the tool mode stores it: at this build's version, its
// times in UTC, its proposal as DecodeProposal returns one, compact JSON. It refuses a
// file DecodeProposalFile would refuse, so none is written that can't be read.
func EncodeProposalFile(f ProposalFile) ([]byte, error) {
	f.Version, f.At, f.SnapshotAt = ProposalFileVersion, f.At.UTC(), f.SnapshotAt.UTC()

	f, err := readProposalFile(f)
	if err != nil {
		return nil, err
	}

	data, err := storedJSON(f)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadableProposalFile, err)
	}

	if len(data) > MaxProposalFileBytes {
		return nil, fmt.Errorf("%w: it would be larger than %d bytes", ErrUnreadableProposalFile, MaxProposalFileBytes)
	}

	return data, nil
}

// DecodeProposalFile reads a proposal file, an untrusted one: its version first, then
// strictly, as spec.DecodeExact reads one, each of its fields held to what the tool mode
// writes, and its proposal to DecodeProposal's caps. Each refusal is an
// ErrUnreadableProposalFile.
func DecodeProposalFile(data []byte) (ProposalFile, error) {
	if err := checkVersion(data, MaxProposalFileBytes, ProposalFileVersion); err != nil {
		return ProposalFile{}, fmt.Errorf("%w: %w", ErrUnreadableProposalFile, err)
	}

	f, err := spec.DecodeExact[ProposalFile](data, MaxProposalFileBytes, proposalFileDepth)
	if err != nil {
		return ProposalFile{}, fmt.Errorf("%w: %w", ErrUnreadableProposalFile, err)
	}

	return readProposalFile(f)
}

// readProposalFile returns f with its proposal as DecodeProposal returns one, refusing a
// field that isn't one.
func readProposalFile(f ProposalFile) (ProposalFile, error) {
	var why string

	switch {
	case !ValidProposalID(f.ID):
		why = fmt.Sprintf("its ID %q isn't a proposal's", textutil.Truncate(f.ID, 40))
	case f.At.IsZero():
		why = "it says no moment it was written"
	case f.Conversation != "" && !conversationPattern.MatchString(f.Conversation):
		why = fmt.Sprintf("its conversation %q isn't one", textutil.Truncate(f.Conversation, 40))
	case f.Conversation != "" && f.Where != "":
		why = "it says both the conversation it came from and the folder"
	case f.Where != Name(f.Where):
		why = "the folder it came from isn't one line of plain text"
	}

	if why != "" {
		return ProposalFile{}, fmt.Errorf("%w: %s", ErrUnreadableProposalFile, why)
	}

	p, err := readProposal(f.Proposal)
	if err != nil {
		return ProposalFile{}, fmt.Errorf("%w: %w", ErrUnreadableProposalFile, err)
	}

	f.Proposal = p

	return f, nil
}

// EncodeOutcomes writes o as the app stores it: at this build's version, every list
// there, compact JSON. It refuses outcomes DecodeOutcomes would refuse.
func EncodeOutcomes(o Outcomes) ([]byte, error) {
	o.Version, o.Items = OutcomesVersion, textutil.NonNil(o.Items)

	if err := checkOutcomes(o.Items); err != nil {
		return nil, err
	}

	data, err := storedJSON(o)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadableOutcomes, err)
	}

	if len(data) > MaxOutcomesBytes {
		return nil, fmt.Errorf("%w: they would be larger than %d bytes", ErrUnreadableOutcomes, MaxOutcomesBytes)
	}

	return data, nil
}

// DecodeOutcomes reads an outcomes file: its version first, then strictly, each outcome
// one there is; its items are never nil. Each refusal is an ErrUnreadableOutcomes.
func DecodeOutcomes(data []byte) (Outcomes, error) {
	if err := checkVersion(data, MaxOutcomesBytes, OutcomesVersion); err != nil {
		return Outcomes{}, fmt.Errorf("%w: %w", ErrUnreadableOutcomes, err)
	}

	o, err := spec.DecodeExact[Outcomes](data, MaxOutcomesBytes, outcomesDepth)
	if err != nil {
		return Outcomes{}, fmt.Errorf("%w: %w", ErrUnreadableOutcomes, err)
	}

	o.Items = textutil.NonNil(o.Items)

	if err = checkOutcomes(o.Items); err != nil {
		return Outcomes{}, err
	}

	return o, nil
}

// OutcomeWords says what the user did with the parts of proposals, as the next turn tells
// the agent: each outcome's words, one line each, joined with "; " into one sentence,
// `Added "Fix checks", switched off; discarded the settings change.`, and "" for none.
func OutcomeWords(items []Outcome) string {
	words := make([]string, 0, len(items))

	for _, o := range items {
		w := strings.TrimSpace(strings.TrimSuffix(textutil.OneLine(o.Words), "."))
		if w == "" {
			continue
		}

		if len(words) > 0 {
			w = lowerLead(w)
		}

		words = append(words, w)
	}

	if len(words) == 0 {
		return ""
	}

	return textutil.Sentence(strings.Join(words, "; "))
}

// lowerLead lowers the first letter of a capitalized word that leads s, for s to carry on
// a sentence, and leaves an acronym, "LGTM", as it is.
func lowerLead(s string) string {
	first, size := utf8.DecodeRuneInString(s)
	next, _ := utf8.DecodeRuneInString(s[size:])

	if !isUpper(first) || isUpper(next) {
		return s
	}

	return strings.ToLower(string(first)) + s[size:]
}

// isUpper reports an upper-case letter.
func isUpper(r rune) bool {
	return r != utf8.RuneError && strings.ToUpper(string(r)) == string(r) && strings.ToLower(string(r)) != string(r)
}

// checkOutcomes refuses an outcome that isn't one there is.
func checkOutcomes(items []Outcome) error {
	for i, o := range items {
		if why := outcomeProblem(o); why != "" {
			return fmt.Errorf("%w: items[%d]: %s", ErrUnreadableOutcomes, i, why)
		}
	}

	return nil
}

// outcomeProblem says why o isn't an outcome there is, or "".
func outcomeProblem(o Outcome) string {
	switch {
	case !ValidProposalID(o.Proposal):
		return fmt.Sprintf("%q isn't a proposal's ID", textutil.Truncate(o.Proposal, 40))
	case !slices.Contains([]Part{PartPrompt, PartRoutine, PartSettings, PartRules}, o.Part):
		return fmt.Sprintf("%q isn't a part of a proposal", textutil.Truncate(string(o.Part), 40))
	case !slices.Contains([]OutcomeKind{OutcomeAdded, OutcomeSaved, OutcomeApplied, OutcomeBuilder, OutcomeDiscarded},
		o.Kind):
		return fmt.Sprintf("%q isn't something done with a part", textutil.Truncate(string(o.Kind), 40))
	case o.Index < 0 || o.RoutineID < 0:
		return "it counts below zero"
	case utf8.RuneCountInString(o.Words) > MaxOutcomeRunes || strings.ContainsFunc(o.Words, notInALine):
		return fmt.Sprintf("its words aren't one line of plain text within %d characters", MaxOutcomeRunes)
	case o.At.IsZero():
		return "it says no moment it was done"
	default:
		return ""
	}
}

// checkVersion refuses data larger than limit, and data that isn't a document of the
// version want, by its version alone, so a file another version wrote says so rather
// than that its keys are unknown.
func checkVersion(data []byte, limit, want int) error {
	if len(data) > limit {
		return fmt.Errorf("%w %d bytes", errLarger, limit)
	}

	var head struct {
		Version json.RawMessage `json:"version"`
	}

	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&head); err != nil {
		return err
	}

	switch v := string(bytes.TrimSpace(head.Version)); {
	case v == "" || v == "null":
		return errNoVersion
	case v != strconv.Itoa(want):
		return fmt.Errorf("%w %s, and this build reads version %d", errOtherVersion, textutil.Truncate(v, 20), want)
	}

	return nil
}

// Why checkVersion refuses a document.
var (
	errLarger       = errors.New("it is larger than")
	errNoVersion    = errors.New("it has no version")
	errOtherVersion = errors.New("it is version")
)
