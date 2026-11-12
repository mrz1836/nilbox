// Package trust decides whether a pull request or issue comes from someone trusted with
// the repository. Launches on untrusted items start in plan mode, because their content
// (code, descriptions, comments) was written by someone outside the project.
//
// An item falls in one of three classes:
//
//   - member: its author is the repository's owner, a member of its org, or a
//     collaborator, and a pull request's branch is in the repository itself;
//   - bot: GitHub reports its author as a bot (a GitHub App), the bot is on the user's
//     trusted list, and a pull request's branch is in the repository itself;
//   - outside: everyone and everything else, including bots not on the list, unknown
//     associations, and every pull request from a fork.
//
// A bot is matched by both its type and its login, so a person whose login resembles a
// bot's never borrows its trust.
package trust

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// Author associations GitHub reports for an item's author. Only the first three are
// trusted.
const (
	AssociationOwner        = "OWNER"
	AssociationMember       = "MEMBER"
	AssociationCollaborator = "COLLABORATOR"
)

// Class is how far an item's author is trusted.
type Class string

// Trust classes.
const (
	// ClassMember is an item from the repository's owner, an org member, or a collaborator.
	ClassMember Class = "member"

	// ClassBot is an item from a trusted bot.
	ClassBot Class = "bot"

	// ClassOutside is an item from an outside contributor, an untrusted bot, or a fork.
	ClassOutside Class = "outside"
)

// TypeBot is the GraphQL type GitHub reports for a bot or GitHub App author.
const TypeBot = "Bot"

// PrefBots is the preference that lists the trusted bots, as a JSON array of logins. The
// settings package writes it.
const PrefBots = "trust.bots"

// maxBots caps the trusted list.
const maxBots = 100

var (
	// ErrInvalidBot is returned for a bot login that GitHub could not have issued.
	ErrInvalidBot = errors.New("not a bot login")

	// ErrTooManyBots is returned for a trusted list over 100 bots.
	ErrTooManyBots = errors.New("too many trusted bots")
)

// botPattern is what a bot login (a GitHub App's slug) looks like once normalized.
var botPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,38})$`)

// DefaultBots returns the bots trusted until the user changes the list: GitHub's
// Dependabot and Actions, and Renovate.
func DefaultBots() []string {
	return []string{"dependabot", "github-actions", "renovate"}
}

// Author is who opened an item.
type Author struct {
	// Login is the author's login. GitHub reports a bot by its app's slug, such as
	// "dependabot", without the "[bot]" suffix its REST login carries.
	Login string

	// Type is the author's GraphQL type: User, Bot, or another actor type; "" before a
	// refresh has read it.
	Type string

	// Association is the author's relationship to the repository.
	Association string
}

// IsBot reports whether GitHub reports the author as a bot.
func (a Author) IsBot() bool {
	return a.Type == TypeBot
}

// Policy decides items' trust with a list of trusted bots.
type Policy struct {
	bots map[string]bool
}

// NewPolicy returns a Policy that trusts the given bots. Logins are matched ignoring
// case and any "[bot]" suffix.
func NewPolicy(bots []string) Policy {
	p := Policy{bots: make(map[string]bool, len(bots))}

	for _, b := range bots {
		p.bots[foldBot(b)] = true
	}

	return p
}

// Classify returns an item's trust class: ClassMember, ClassBot, or ClassOutside. A pull
// request from a fork (crossRepo) is always outside.
func (p Policy) Classify(a Author, isPR, crossRepo bool) Class {
	if isPR && crossRepo {
		return ClassOutside
	}

	switch strings.ToUpper(strings.TrimSpace(a.Association)) {
	case AssociationOwner, AssociationMember, AssociationCollaborator:
		return ClassMember
	}

	if a.IsBot() && p.bots[foldBot(a.Login)] {
		return ClassBot
	}

	return ClassOutside
}

// Trusted reports whether an item's class is trusted: anything but ClassOutside.
func (p Policy) Trusted(a Author, isPR, crossRepo bool) bool {
	return p.Classify(a, isPR, crossRepo) != ClassOutside
}

// NormalizeBots cleans a trusted list as the user entered it: each login is trimmed and
// lowercased, an "app/" prefix or "[bot]" suffix is dropped, empty entries and
// duplicates are removed, and the list is sorted. A login GitHub could not have issued is
// an ErrInvalidBot.
func NormalizeBots(logins []string) ([]string, error) {
	out := make([]string, 0, len(logins))

	for _, login := range logins {
		b := foldBot(login)
		if b == "" {
			continue
		}

		if !botPattern.MatchString(b) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidBot, strings.TrimSpace(login))
		}

		if !slices.Contains(out, b) {
			out = append(out, b)
		}
	}

	if len(out) > maxBots {
		return nil, fmt.Errorf("%w: at most %d", ErrTooManyBots, maxBots)
	}

	slices.Sort(out)

	return out, nil
}

// EncodeBots writes a trusted list for PrefBots, a nil list as an empty one.
func EncodeBots(bots []string) string {
	return prefval.EncodeList(bots)
}

// DecodeBots reads PrefBots: the defaults while the preference is unset (stored false)
// or unreadable, else the stored list, which may be empty.
func DecodeBots(raw string, stored bool) []string {
	if !stored {
		return DefaultBots()
	}

	bots, ok := prefval.List[string](raw)
	if !ok {
		return DefaultBots()
	}

	if bots == nil {
		return []string{}
	}

	return bots
}

// foldBot returns a login as the trusted list stores it: trimmed, lowercased, without
// an "app/" prefix or a "[bot]" suffix.
func foldBot(login string) string {
	b := strings.ToLower(strings.TrimSpace(login))
	b = strings.TrimPrefix(b, "app/")

	return strings.TrimSpace(strings.TrimSuffix(b, "[bot]"))
}
