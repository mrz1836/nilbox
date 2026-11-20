package draft

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/uierr"
)

const (
	// MaxNameRunes caps a routine's name.
	MaxNameRunes = 80

	// MaxDescriptionRunes caps a routine's description.
	MaxDescriptionRunes = 500

	// maxTemplateKey caps a template's key.
	maxTemplateKey = 64
)

var (
	// ErrInvalidName is returned for a name that is empty, too long, or not one line.
	ErrInvalidName = errors.New("invalid routine name")

	// ErrInvalidDescription is returned for a description that is too long or holds a
	// control character.
	ErrInvalidDescription = errors.New("invalid routine description")

	// ErrInvalidTemplate is returned for a template key that isn't one.
	ErrInvalidTemplate = errors.New("invalid template key")
)

// templateKey is what a template's key looks like: "fix-failing-checks-on-my-prs".
var templateKey = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// CheckName returns a routine's name, trimmed, refusing one that is empty, longer than
// MaxNameRunes, or not one line of plain text.
func CheckName(name string) (string, error) {
	name = strings.TrimSpace(name)

	switch {
	case name == "":
		return "", uierr.New("Give the routine a name.", ErrInvalidName)
	case utf8.RuneCountInString(name) > MaxNameRunes:
		return "", uierr.New(fmt.Sprintf("Keep the name to %d characters.", MaxNameRunes),
			fmt.Errorf("%w: a long name", ErrInvalidName))
	case !utf8.ValidString(name) || strings.ContainsFunc(name, Hidden):
		return "", uierr.New("Keep the name to one line of plain text.",
			fmt.Errorf("%w: a hidden character", ErrInvalidName))
	}

	return name, nil
}

// CheckDescription returns a routine's description, trimmed, refusing one longer than
// MaxDescriptionRunes or holding a control character but a line break.
func CheckDescription(desc string) (string, error) {
	desc = strings.TrimSpace(desc)

	switch {
	case utf8.RuneCountInString(desc) > MaxDescriptionRunes:
		return "", uierr.New(fmt.Sprintf("Keep the description to %d characters.", MaxDescriptionRunes),
			fmt.Errorf("%w: a long description", ErrInvalidDescription))
	case !utf8.ValidString(desc) || strings.ContainsFunc(desc, func(r rune) bool { return r != '\n' && Hidden(r) }):
		return "", uierr.New("Keep the description to plain text.",
			fmt.Errorf("%w: a hidden character", ErrInvalidDescription))
	}

	return desc, nil
}

// CheckTemplate returns a template's key, trimmed, refusing one that isn't one: "" is
// none.
func CheckTemplate(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || (len(key) <= maxTemplateKey && templateKey.MatchString(key)) {
		return key, nil
	}

	return "", uierr.New("That isn't one of the templates.", fmt.Errorf("%w %q", ErrInvalidTemplate, key))
}

// Hidden reports a rune that can't be shown in a line of text: a control character, or
// one that changes the text's direction.
func Hidden(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r)
}

// NameKey folds a routine's name for comparing it with another, ignoring case: each rune
// is replaced by the smallest rune it folds to under Unicode's simple case folding, so two
// names have one key exactly when strings.EqualFold says they are the same. Every
// comparison of names, in Go and in the database's uniqueness (name_key), uses it, since
// SQLite's NOCASE folds ASCII alone.
func NameKey(name string) string {
	return strings.Map(foldRune, name)
}

// FreeName returns name, or, when taken says another routine has it, the first of "name
// 2", "name 3", and so on that taken says none has, each cut to fit MaxNameRunes
// (FitName): the name a save, a copy, an import, or a restore gives a routine whose name
// is taken. taken compares names as NameKey does, and says no to all but a finite few.
func FreeName(name string, taken func(string) bool) string {
	if !taken(name) {
		return name
	}

	for i := 2; ; i++ {
		if n := FitName(name, " "+strconv.Itoa(i)); !taken(n) {
			return n
		}
	}
}

// FitName returns name with suffix, name cut so the whole fits MaxNameRunes and trimmed
// where it was cut.
func FitName(name, suffix string) string {
	runes := []rune(name)
	if keep := MaxNameRunes - utf8.RuneCountInString(suffix); len(runes) > keep {
		runes = runes[:max(keep, 0)]
	}

	return strings.TrimSpace(string(runes)) + suffix
}

// foldRune returns the smallest rune in r's simple case folding orbit.
func foldRune(r rune) rune {
	least := r

	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		least = min(least, f)
	}

	return least
}
