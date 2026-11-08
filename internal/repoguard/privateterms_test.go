package repoguard_test

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// privateTermsFile is where a contributor lists the names this public repository must
// never contain: their employer, clients, private organizations and repositories. It is
// relative to the clone's shared git directory, so git can never commit it and every
// worktree of the clone reads the same list.
const privateTermsFile = "info/private-terms"

// privateTermsPath returns the path of this clone's private-terms file, or ok=false when
// git cannot report the clone's git directory.
func privateTermsPath(t *testing.T, root string) (string, bool) {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", "-C", root, //nolint:gosec // argv only, no shell
		"rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", false
	}

	return filepath.Join(strings.TrimSpace(string(out)), filepath.FromSlash(privateTermsFile)), true
}

// parsePrivateTerms returns the terms a private-terms file lists: one per line, trimmed,
// skipping blank lines and lines starting with #.
func parsePrivateTerms(text string) []string {
	var terms []string

	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			terms = append(terms, line)
		}
	}

	return terms
}

// isWordRune reports whether r is part of a word: a letter or a number.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

// privateTermsPattern matches any of terms, ignoring case, as a whole word: a term that
// starts or ends with a letter or number must not continue into another one, so "acme"
// matches "Acme", "acme-org", and "acme_api" but not "acmetools" or "AcmeClient".
func privateTermsPattern(terms []string) *regexp.Regexp {
	const edge = `[^\p{L}\p{N}]`

	alternatives := make([]string, 0, len(terms))

	for _, term := range terms {
		expr := regexp.QuoteMeta(term)

		if first, _ := utf8.DecodeRuneInString(term); isWordRune(first) {
			expr = `(?:^|` + edge + `)` + expr
		}

		if last, _ := utf8.DecodeLastRuneInString(term); isWordRune(last) {
			expr += `(?:` + edge + `|$)`
		}

		alternatives = append(alternatives, expr)
	}

	return regexp.MustCompile(`(?i)(?:` + strings.Join(alternatives, "|") + `)`)
}

// gitText runs a read-only git command in root and returns its output.
func gitText(t *testing.T, root string, args ...string) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...).Output() //nolint:gosec // argv only, no shell
	require.NoError(t, err, "git %s", strings.Join(args, " "))

	return string(out)
}

// loadPrivateTerms returns the path of this clone's private-terms file and the terms it
// lists. It skips t when git cannot find the clone's git directory, when there is no
// file (as in CI), or when the file lists no terms.
func loadPrivateTerms(t *testing.T, root string) (string, []string) {
	t.Helper()

	termsPath, ok := privateTermsPath(t, root)
	if !ok {
		t.Skip("git cannot report this clone's git directory, so there is no private-terms file to read")
	}

	body, err := os.ReadFile(termsPath) //nolint:gosec // the contributor's own private-terms file
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("no private-terms file; list your private names in %s to check for them", termsPath)
	}

	require.NoError(t, err)

	terms := parsePrivateTerms(string(body))
	if len(terms) == 0 {
		t.Skipf("%s lists no terms", termsPath)
	}

	return termsPath, terms
}

// privateTermsInFiles returns every repository file whose path pattern matches, and
// every line of a text file that it matches.
func privateTermsInFiles(t *testing.T, root string, pattern *regexp.Regexp) []string {
	t.Helper()

	var found []string

	for _, rel := range repoFiles(t, root) {
		if pattern.MatchString(rel) {
			found = append(found, rel+": (the path)")
		}

		if text, ok := readText(t, root, rel); ok && pattern.MatchString(text) {
			found = append(found, hits(rel, text, pattern.MatchString)...)
		}
	}

	return found
}

// privateTermsInGit returns the current branch name if pattern matches it, and every
// line it matches in the message of a commit no remote has yet: the commits a push
// would publish.
func privateTermsInGit(t *testing.T, root string, pattern *regexp.Regexp) []string {
	t.Helper()

	var found []string

	if branch := strings.TrimSpace(gitText(t, root, "branch", "--show-current")); pattern.MatchString(branch) {
		found = append(found, "branch name: "+branch)
	}

	// Each record is the short hash, a NUL, and the message, ended by an ASCII record
	// separator.
	log := gitText(t, root, "log", "HEAD", "--not", "--remotes", "--format=%h%x00%B%x1e")

	for record := range strings.SplitSeq(log, "\x1e") {
		hash, message, ok := strings.Cut(strings.TrimSpace(record), "\x00")
		if ok && pattern.MatchString(message) {
			found = append(found, hits("commit "+hash, message, pattern.MatchString)...)
		}
	}

	return found
}

// reportPrivateTerms fails t when found has entries, listing one per line so that a long
// list stays readable.
func reportPrivateTerms(t *testing.T, found []string, advice string) {
	t.Helper()

	if len(found) > 0 {
		t.Errorf("%s:\n\t%s", advice, strings.Join(found, "\n\t"))
	}
}

// TestNoPrivateTerms fails wherever a term from this clone's private-terms file appears:
// in a file's contents or path, in the current branch name, or in a commit no remote has
// yet. Without the file (as in CI) it skips. See README.md, "Keep private names out of the
// repository".
func TestNoPrivateTerms(t *testing.T) {
	root := moduleRoot(t)
	termsPath, terms := loadPrivateTerms(t, root)
	pattern := privateTermsPattern(terms)

	t.Run("files", func(t *testing.T) {
		reportPrivateTerms(t, privateTermsInFiles(t, root, pattern), "a term from "+termsPath+
			" is in these files; replace it with a neutral placeholder such as acme-org or example-repo")
	})

	t.Run("git metadata", func(t *testing.T) {
		reportPrivateTerms(t, privateTermsInGit(t, root, pattern), "a term from "+termsPath+
			" is in the branch name or an unpushed commit message; rename the branch or reword "+
			"the commit before you push")
	})
}

func TestParsePrivateTerms(t *testing.T) {
	text := "# employers and clients\n\n  acme-corp  \n\t# an indented comment\nWidget Works\r\nwidget\n"

	assert.Equal(t, []string{"acme-corp", "Widget Works", "widget"}, parsePrivateTerms(text))
	assert.Empty(t, parsePrivateTerms("# only comments\n\n"))
}

func TestPrivateTermsPattern(t *testing.T) {
	pattern := privateTermsPattern([]string{"acme-corp", "widget", "@secret-org", "ex.co", "team42", "Zürich"})

	cases := map[string]bool{
		"built for Acme-Corp.":             true,
		"acme-corp/example-repo":           true,
		"github.com/ACME-CORP":             true,
		"acme-corporation":                 false,
		"notacme-corp":                     false,
		"widget":                           true,
		"widget_api and widget-cli":        true,
		"WIDGET.":                          true,
		"widgets":                          false,
		"WidgetFactory":                    false,
		"mail@secret-org.example":          true,
		"@secret-orgs":                     false,
		"ex.co":                            true,
		"exxco":                            false,
		"team42":                           true,
		"team420":                          false,
		"office in ZÜRICH":                 true,
		"nothing private in this sentence": false,
		"":                                 false,
	}

	for text, want := range cases {
		assert.Equal(t, want, pattern.MatchString(text), "%q", text)
	}
}
