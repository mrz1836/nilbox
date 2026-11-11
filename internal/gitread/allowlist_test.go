package gitread

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mainBranch = "main"
	headSHA    = "fc1c3975011e66491785ace891c893e4fc716587"
	otherSHA   = "0b1c3975011e66491785ace891c893e4fc716588"
	sha256ID   = "4b825dc642cb6eb9a060e54bf8d69288fbee49044b825dc642cb6eb9a060e54b"
	injection  = "--upload-pack=touch /srv/pwned"
)

// cloneChangingVerbs mirrors the git subcommands internal/repoguard's
// TestNoGitCommandChangesAClone keeps out of the app: the ones that change a clone's
// checkout, index, history, or remotes' refs.
func cloneChangingVerbs() []string {
	return []string{
		"checkout", "stash", "fetch", "switch", "pull", "reset", "merge", "rebase", "commit", "push", "restore",
		"cherry-pick", "clean",
	}
}

// readOnlyForms are the git subcommands gitread may run, each with a word that keeps it
// read-only: worktree only lists, tag only lists, config only gets, and symbolic-ref only
// reads HEAD. An empty word needs none.
func readOnlyForms() map[string]string {
	return map[string]string{
		revParse: "", "status": "--porcelain", "symbolic-ref": "--short", "worktree": "list", configCmd: getFlag,
		"rev-list": "--count", "merge-base": "--is-ancestor", "tag": "--list", lsRemote: remoteName,
	}
}

// samples are values a slot accepts, the first of each used to fill a command, and values
// no command accepts in its place.
type samples struct {
	good []string
	bad  []string
}

// slotSamples returns samples for every slot that takes a value.
func slotSamples() map[slot]samples {
	return map[slot]samples{
		slotGitPath: {
			good: []string{"MERGE_HEAD", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD", "REVERT_HEAD", "hooks"},
			bad:  []string{"index", "config", "../MERGE_HEAD", "hooks/../config", "MERGE_HEAD\n", "/srv/hooks"},
		},
		slotLocalRef: {
			good: []string{"refs/heads/main", "refs/heads/feature/x", "refs/heads/release-1.4"},
			bad: []string{
				mainBranch, "refs/heads/-x", "refs/heads/a..b", "refs/heads/", "refs/heads/x\n", "refs/heads/a b",
				"refs/heads/x.lock", "refs/heads/.x", "refs/heads/x@{1}", "refs/heads/x^{tree}", "refs/heads/x~1",
				"refs/heads/feature//x", "refs/tags/v1.0.0",
			},
		},
		slotTrackingRef: {
			good: []string{"refs/remotes/origin/main", "refs/remotes/origin/feature/x"},
			bad: []string{
				"refs/remotes/upstream/main", "refs/remotes/origin/", "refs/remotes/origin/a..b",
				"refs/remotes/origin/-x", "origin/main", "refs/remotes/origin/x\x00y",
			},
		},
		slotCommitPeel: {
			good: []string{headSHA + "^{commit}", sha256ID + "^{commit}"},
			bad: []string{
				headSHA, strings.Repeat("0", 40) + "^{commit}", "HEAD^{commit}", headSHA + "^{tree}", "fc1c397^{commit}",
				"main^{commit}",
			},
		},
		slotCommitID: {
			good: []string{headSHA, otherSHA, sha256ID},
			bad:  []string{"HEAD", mainBranch, "fc1c397", strings.Repeat("0", 40), strings.ToUpper(headSHA), headSHA + "^"},
		},
		slotConfigKey: {
			good: []string{
				"gpg.format", "user.signingkey", "branch.main.remote", "branch.feature/x.merge",
				"branch.release-1.4.remote",
			},
			bad: []string{
				"core.hooksPath", "core.sshCommand", "core.askPass", "remote.origin.url", "credential.helper", "branch.main.pushRemote", "branch..remote",
				"branch.-x.remote", "branch.a b.merge", "branch.a..b.remote", "tag.gpgSign", "user.signingkey\n",
			},
		},
		slotBoolKey: {
			good: []string{"tag.gpgSign", "tag.forceSignAnnotated"},
			bad:  []string{"gpg.format", "tag.gpgsign", "core.sshCommand", "branch.main.remote"},
		},
		slotUnpushedRange: {
			good: []string{"refs/remotes/origin/main..refs/heads/main", "refs/remotes/origin/a/b..refs/heads/a/b"},
			bad: []string{
				"refs/remotes/origin/main..refs/heads/other", "refs/heads/main..refs/remotes/origin/main",
				"refs/remotes/origin/main...refs/heads/main", "main..main", "refs/remotes/origin/main..",
				"refs/remotes/upstream/main..refs/heads/main", "refs/remotes/origin/main..refs/heads/main..x",
			},
		},
		slotVersionPattern: {
			good: []string{"v*", "v1.*", "v1.4*", "v1.4.*"},
			bad:  []string{"*", "v", "v**", "x*", "refs/tags/v*", "v1..*", "v*/..", "v[0-9]*"},
		},
		slotTagRef: {
			good: []string{"refs/tags/v1.4.2", "refs/tags/v2.0.0-rc.1"},
			bad: []string{
				"refs/tags/1.4.2", "refs/tags/v", "refs/tags/v1..2", "refs/heads/v1.4.2", "refs/tags/v1.4.2^{}",
				"refs/tags/-v1", "refs/tags/v1 x", "v1.4.2", "refs/tags/v*",
			},
		},
		slotPeeledTagRef: {
			good: []string{"refs/tags/v1.4.2^{}", "refs/tags/v2.0.0-rc.1^{}"},
			bad:  []string{"refs/tags/v1.4.2^{commit}", "refs/tags/v^{}", "refs/tags/1.4.2^{}", "v1.4.2^{}"},
		},
		slotTagCommit: {
			good: []string{"refs/tags/v1.4.2^{commit}", "refs/tags/v2.0.0-rc.1^{commit}"},
			bad: []string{
				"refs/tags/v1.4.2", "refs/tags/v1.4.2^{}", "refs/tags/v1.4.2^{tree}", "refs/heads/main^{commit}",
				"refs/tags/1.4.2^{commit}", "v1.4.2^{commit}", "refs/tags/v1..2^{commit}",
			},
		},
		slotCommitRange: {
			good: []string{headSHA + ".." + otherSHA, otherSHA + ".." + headSHA, sha256ID + ".." + sha256ID},
			bad: []string{
				headSHA, headSHA + "...", headSHA + "..." + otherSHA, headSHA + "..HEAD", "HEAD.." + headSHA,
				"main.." + headSHA, "fc1c397.." + headSHA, strings.Repeat("0", 40) + ".." + headSHA,
				headSHA + ".." + otherSHA + "..x", "^" + headSHA + ".." + otherSHA, headSHA + "..-x",
			},
		},
	}
}

// commonBad are values no word of any command takes.
func commonBad() []string {
	return []string{"", injection, "-x", "--", "\x00", "a\nb", "origin\x1b[2J"}
}

// fill returns c's words with each slot's first good sample, or, at index at, with
// replacement.
func fill(t *testing.T, c command, at int, replacement string) []string {
	t.Helper()

	all := slotSamples()
	args := make([]string, 0, len(c.words))

	for i, w := range c.words {
		switch {
		case i == at:
			args = append(args, replacement)
		case w.slot == slotFixed:
			args = append(args, w.text)
		default:
			s, ok := all[w.slot]
			require.True(t, ok, "slot %d has no samples", w.slot)

			args = append(args, s.good[0])
		}
	}

	return args
}

func TestEverySlotHasAValidatorAndSamples(t *testing.T) {
	t.Parallel()

	checks := validators()
	all := slotSamples()

	for s := slotGitPath; s <= slotCommitRange; s++ {
		assert.Contains(t, checks, s, "slot %d has no validator", s)
		assert.Contains(t, all, s, "slot %d has no samples", s)
	}

	assert.Len(t, checks, len(all))
}

func TestValidators(t *testing.T) {
	t.Parallel()

	checks := validators()

	for s, sample := range slotSamples() {
		for _, good := range sample.good {
			assert.True(t, checks[s](good), "slot %d refuses %q", s, good)
		}

		for _, bad := range append(slices.Clone(sample.bad), commonBad()...) {
			assert.False(t, checks[s](bad), "slot %d takes %q", s, bad)
		}
	}
}

func TestAllowlist(t *testing.T) {
	t.Parallel()

	guarded := cloneChangingVerbs()
	forms := readOnlyForms()
	names := map[string]bool{}

	for _, c := range allowlist() {
		require.NotEmpty(t, c.words, c.name)
		assert.False(t, names[c.name], "%q is listed twice", c.name)
		names[c.name] = true

		verb := c.words[0]
		require.Equal(t, slotFixed, verb.slot, "%s: the subcommand is fixed", c.name)

		form, ok := forms[verb.text]
		require.True(t, ok, "%s: %q is not a read-only subcommand", c.name, verb.text)

		var words []string

		for _, w := range c.words {
			if w.slot != slotFixed {
				continue
			}

			words = append(words, w.text)
			assert.NotContains(t, guarded, w.text, "%s: %q changes a clone", c.name, w.text)
			assert.False(t, strings.HasPrefix(w.text, "-c") || strings.Contains(w.text, "hooksPath"),
				"%s: %q sets config", c.name, w.text)
			// Only git's own upload-pack, which the clone's config can't then replace.
			assert.False(t, (strings.HasPrefix(w.text, "--upload-pack") && w.text != "--upload-pack=git-upload-pack") ||
				strings.HasPrefix(w.text, "--exec"), "%s: %q runs a program", c.name, w.text)
		}

		assertReadOnlyForm(t, c.name, verb.text, words, form)

		assert.True(t, allowed(fill(t, c, -1, "")), "%s: its own words are refused", c.name)
	}
}

func TestAllowlistTakesEveryGoodSample(t *testing.T) {
	t.Parallel()

	all := slotSamples()
	good := func(w word) []string { return all[w.slot].good }

	for _, c := range allowlist() {
		for _, args := range variants(t, c, good) {
			if c.together != nil && !c.together(args) {
				continue // the remote tag's two refs must name one tag
			}

			assert.True(t, allowed(args), "%s refuses %q", c.name, args)
		}
	}
}

func TestAllowlistRefusesEveryBadWord(t *testing.T) {
	t.Parallel()

	all := slotSamples()
	bad := func(w word) []string {
		values := slices.DeleteFunc(commonBad(), func(v string) bool { return w.slot == slotFixed && v == w.text })

		return append(values, all[w.slot].bad...)
	}

	for _, c := range allowlist() {
		for _, args := range variants(t, c, bad) {
			assert.False(t, allowed(args), "%s takes %q", c.name, args)
		}

		args := fill(t, c, -1, "")
		assert.False(t, allowed(args[:len(args)-1]), "%s without its last word", c.name)
		assert.False(t, allowed(append(slices.Clone(args), injection)), "%s with a word more", c.name)
		assert.False(t, allowed(append([]string{injection}, args...)), "%s after an option", c.name)
	}
}

// variants returns c filled with each of values(w) in place of each of its words w, one
// value at a time.
func variants(t *testing.T, c command, values func(w word) []string) [][]string {
	t.Helper()

	var out [][]string

	for i, w := range c.words {
		for _, v := range values(w) {
			out = append(out, fill(t, c, i, v))
		}
	}

	return out
}

func TestAllowlistEntriesNeverOverlap(t *testing.T) {
	t.Parallel()

	list := allowlist()

	for _, c := range list {
		args := fill(t, c, -1, "")

		matched := 0

		for _, other := range list {
			if other.matches(args) {
				matched++
			}
		}

		assert.Equal(t, 1, matched, "%s: %q matches more than one entry", c.name, args)
	}
}

func TestRemoteTagNamesOneTag(t *testing.T) {
	t.Parallel()

	assert.True(t, allowed(remoteTagArgs("v1.4.2")))
	assert.False(t, allowed([]string{lsRemote, remoteName, "refs/tags/v1.4.2", "refs/tags/v1.4.3^{}"}))
	assert.False(t, allowed([]string{lsRemote, remoteName, "refs/tags/v1.4.2^{}", "refs/tags/v1.4.2"}))
	assert.False(t, samePeeledTag([]string{"refs/tags/v1.4.2^{}"}))
}

func TestIsDir(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"/", "/srv/projects/example-repo", "/srv/projects/my repo", "/srv/a-b/c.d"} {
		assert.True(t, isDir(dir), dir)
	}

	for _, dir := range []string{
		"", ".", "relative/dir", "-C", injection, "/srv/a/../b", "/srv/a/", "/srv//a", "/srv/./a", "/srv/a\nb",
		"/srv/a\x00b", "/srv/a\tb", "/srv/..",
	} {
		assert.False(t, isDir(dir), "%q", dir)
	}
}

// assertReadOnlyForm checks that a command holds the word that makes its subcommand only
// read, such as config's --get; --get-regexp reads too.
func assertReadOnlyForm(t *testing.T, name, verb string, words []string, form string) {
	t.Helper()

	if form == "" || (verb == configCmd && slices.Contains(words, "--get-regexp")) {
		return
	}

	// A form's value, as status's --porcelain=v2, keeps it the same form.
	has := slices.ContainsFunc(words, func(w string) bool { return w == form || strings.HasPrefix(w, form+"=") })
	assert.True(t, has, "%s: %s without %s", name, verb, form)
}
