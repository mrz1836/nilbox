package repoguard_test

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
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

// foldRune returns the smallest rune that matches r when case is ignored: the least of
// r's case-folding orbit, the set of runes an (?i) pattern accepts for r.
func foldRune(r rune) rune {
	least := r

	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		least = min(least, f)
	}

	return least
}

// foldCase maps every rune of s to foldRune's. Two strings an (?i) pattern takes as equal
// fold to the same string, so a text that does not contain a folded term cannot match
// the term either.
func foldCase(s string) string {
	return strings.Map(foldRune, s)
}

// mayHoldATerm reports whether text contains any of the folded terms, ignoring case as
// privateTermsPattern does. It never says no for a text the pattern matches, and it is
// far cheaper than the pattern, which then runs only on the few texts it lets through.
func mayHoldATerm(text string, folded []string) bool {
	text = foldCase(text)

	return slices.ContainsFunc(folded, func(term string) bool { return strings.Contains(text, term) })
}

// privateTermsInFiles returns every repository file whose path pattern matches, and
// every line of a text file that it matches, in file order. folded is terms, the terms
// pattern was built from, each through foldCase. The files are scanned side by side, and
// a file whose text holds no term in any case is not matched line by line.
func privateTermsInFiles(t *testing.T, root string, pattern *regexp.Regexp, terms []string) []string {
	t.Helper()

	folded := make([]string, 0, len(terms))
	for _, term := range terms {
		folded = append(folded, foldCase(term))
	}

	files := repoFiles(t, root)
	found := make([][]string, len(files))

	var g errgroup.Group

	g.SetLimit(runtime.GOMAXPROCS(0))

	for i, rel := range files {
		g.Go(func() error {
			var err error

			found[i], err = privateTermsInFile(root, rel, pattern, folded)

			return err
		})
	}

	require.NoError(t, g.Wait())

	return slices.Concat(found...)
}

// privateTermsInFile returns rel's path when pattern matches it, and every line of its
// text that pattern matches.
func privateTermsInFile(root, rel string, pattern *regexp.Regexp, folded []string) ([]string, error) {
	var found []string

	if pattern.MatchString(rel) {
		found = append(found, rel+": (the path)")
	}

	text, ok, err := readTextFile(root, rel)
	if err != nil {
		return nil, err
	}

	if ok && mayHoldATerm(text, folded) && pattern.MatchString(text) {
		found = append(found, hits(rel, text, pattern.MatchString)...)
	}

	return found, nil
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
	t.Parallel()

	root := moduleRoot(t)
	termsPath, terms := loadPrivateTerms(t, root)
	pattern := privateTermsPattern(terms)

	t.Run("files", func(t *testing.T) {
		t.Parallel()

		reportPrivateTerms(t, privateTermsInFiles(t, root, pattern, terms), "a term from "+termsPath+
			" is in these files; replace it with a neutral placeholder such as acme-org or example-repo")
	})

	t.Run("git metadata", func(t *testing.T) {
		t.Parallel()

		reportPrivateTerms(t, privateTermsInGit(t, root, pattern), "a term from "+termsPath+
			" is in the branch name or an unpushed commit message; rename the branch or reword "+
			"the commit before you push")
	})
}

func TestParsePrivateTerms(t *testing.T) {
	t.Parallel()

	text := "# employers and clients\n\n  acme-corp  \n\t# an indented comment\nWidget Works\r\nwidget\n"

	assert.Equal(t, []string{"acme-corp", "Widget Works", "widget"}, parsePrivateTerms(text))
	assert.Empty(t, parsePrivateTerms("# only comments\n\n"))
}

func TestPrivateTermsPattern(t *testing.T) {
	t.Parallel()

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

func TestMayHoldATermLetsEveryMatchThrough(t *testing.T) {
	t.Parallel()

	terms := []string{"acme-corp", "widget", "Zürich", "kelvin", "class"}
	pattern := privateTermsPattern(terms)

	folded := make([]string, 0, len(terms))
	for _, term := range terms {
		folded = append(folded, foldCase(term))
	}

	texts := []string{
		"built for ACME-CORP.",
		"WiDgEt_api",
		"office in ZÜRICH",
		"\u212Aelvin",           // the Kelvin sign folds with k
		"cla\u017Fs of its own", // the long s folds with s
		"\xffwidget\xfe",
		"nothing private in this sentence",
		"widgets and acme-corporation",
		"",
	}

	for _, text := range texts {
		if pattern.MatchString(text) {
			assert.True(t, mayHoldATerm(text, folded), "the prefilter must let %q through", text)
		}
	}

	assert.False(t, mayHoldATerm("nothing private in this sentence", folded))
	assert.True(t, pattern.MatchString("\u212Aelvin"), "the pattern ignores case the way Go folds it")
}
