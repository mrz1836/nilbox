package gitread

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fuzzOID is an object ID for the fuzz seeds.
const fuzzOID = "0123456789abcdef0123456789abcdef01234567"

// FuzzParseStatus checks that any `git status --porcelain -z` output parses into changes
// that each hold a two-character code and a path, no more than its fields, and that one
// change written the way git writes it, from any code without a rename's or copy's letter
// and any path, parses back to that change.
func FuzzParseStatus(f *testing.F) {
	for _, seed := range []struct {
		out, code, path string
	}{
		{"", " M", "a.go"},
		{" M a.go\x00?? new dir/\x00", "??", "new dir/"},
		{"R  new.go\x00old.go\x00A  b.go\x00", "A ", "b.go"},
		{"C  copy.go", "MM", "x"},
		{"XY\x00ab\x00 M \x00", "!!", "with\nnewline"},
	} {
		f.Add([]byte(seed.out), seed.code, seed.path)
	}

	f.Fuzz(func(t *testing.T, out []byte, code, path string) {
		changes := parseStatus(out)
		assert.LessOrEqual(t, len(changes), strings.Count(string(out), "\x00")+1)

		for _, c := range changes {
			assert.Len(t, c.Code, 2)
			assert.NotEmpty(t, c.Path)
			assert.NotContains(t, c.Path, "\x00")
		}

		if len(code) != 2 || strings.ContainsAny(code, "RC\x00") || path == "" || strings.Contains(path, "\x00") {
			return
		}

		assert.Equal(t, []Change{{Code: code, Path: path}}, parseStatus([]byte(code+" "+path+"\x00")))
	})
}

// FuzzParseEntries checks that any `git status --porcelain=v2 -z` output parses into
// entries that each hold a path, no more than its fields, and that an ordinary change and
// a rename written the way git writes them, from any code, object, and paths, parse back
// to those entries.
func FuzzParseEntries(f *testing.F) {
	for _, seed := range []struct {
		out, code, path, from string
	}{
		{"", ".M", "a.go", "old.go"},
		{"1 .M N... 100644 100644 100644 " + fuzzOID + " " + fuzzOID + " a b.go\x00", "M.", "a b.go", "a.go"},
		{"2 R. N... 100644 100644 100644 " + fuzzOID + " " + fuzzOID + " R100 new.go\x00old.go\x00", "R.", "new", "old"},
		{"u UU N... 100644 100644 100644 100644 a b c conflict.go\x00? untracked\x00", "UU", "x", "y"},
		{"2 R. N... 1 1 1 a b R100 cut-short.go", "..", " ", " "},
		{"# branch.oid " + fuzzOID + "\x001 .M\x00?\x00", "AM", "dir/", "z"},
	} {
		f.Add([]byte(seed.out), seed.code, seed.path, seed.from)
	}

	f.Fuzz(func(t *testing.T, out []byte, code, path, from string) {
		entries := parseEntries(out)
		assert.LessOrEqual(t, len(entries), strings.Count(string(out), "\x00")+1)

		for _, e := range entries {
			assert.NotEmpty(t, e.Path)
			assert.NotContains(t, e.Path+e.From+e.Index+e.Code, "\x00")
		}

		if len(code) != 2 || strings.ContainsAny(code, " \x00") || path == "" ||
			strings.Contains(path, "\x00") || strings.Contains(from, "\x00") {
			return
		}

		ordinary := "1 " + code + " N... 100644 100644 100644 " + fuzzOID + " staged " + path
		renamed := "2 " + code + " N... 100644 100644 100644 " + fuzzOID + " staged R100 " + path

		assert.Equal(t, []Entry{
			{Code: code, Index: "staged", Path: path},
			{Code: code, Index: "staged", Path: path, From: from},
		}, parseEntries([]byte(ordinary+"\x00"+renamed+"\x00"+from+"\x00")))
	})
}

// FuzzParseSettings checks that any `git config --show-scope -z` output, with origins or
// without, either fails as unexpected output or parses into one setting for each group of
// fields, and that settings written the way git writes them, from any scope, origin, key,
// and value git can print, parse back to those settings.
func FuzzParseSettings(f *testing.F) {
	for _, seed := range []struct {
		out                       string
		withOrigin                bool
		scope, origin, key, value string
	}{
		{"", false, "local", "file:.git/config", "core.bare", "true"},
		{"local\x00core.hooksPath\n.husky\x00global\x00user.name\nAcme Org\x00", false, "global", "", "user.name", "a\nb"},
		{"local\x00file:.git/config\x00remote.origin.url\nhttps://example.com/x\x00", true, "command", "command line:", "k", ""},
		{"local\x00only-two\x00fields\x00", true, "worktree", "file:x", "include.path", "~/x"},
		{"\x00\x00\x00\x00", false, "", "", "", ""},
	} {
		f.Add([]byte(seed.out), seed.withOrigin, seed.scope, seed.origin, seed.key, seed.value)
	}

	f.Fuzz(func(t *testing.T, out []byte, withOrigin bool, scope, origin, key, value string) {
		checkParsedSettings(t, out, withOrigin)

		if strings.Contains(scope+origin+key+value, "\x00") || strings.Contains(key, "\n") {
			return
		}

		want := scopedSetting{scope: scope, key: key, value: value}
		line := scope + "\x00"

		if withOrigin {
			want.origin = origin
			line += origin + "\x00"
		}

		line += key + "\n" + value + "\x00"

		got, err := parseSettings([]byte(line+line), withOrigin)
		require.NoError(t, err)
		assert.Equal(t, []scopedSetting{want, want}, got)
	})
}

// checkParsedSettings checks what parseSettings makes of out: unexpected output, or one
// setting for each group of fields.
func checkParsedSettings(t *testing.T, out []byte, withOrigin bool) {
	t.Helper()

	each := 2
	if withOrigin {
		each = 3
	}

	settings, err := parseSettings(out, withOrigin)
	if err != nil {
		require.ErrorIs(t, err, errOutput)
		assert.Nil(t, settings)

		return
	}

	if text := strings.TrimSuffix(string(out), "\x00"); text != "" {
		assert.Len(t, settings, (strings.Count(text, "\x00")+1)/each)
	}
}

// FuzzParseRemoteRefs checks that any ls-remote output parses into branches and tags that
// each point at an object ID, and that a branch and an annotated tag written the way
// ls-remote writes them, from any names, parse back to that branch and tag, the tag's
// commit its peeled line's.
func FuzzParseRemoteRefs(f *testing.F) {
	const commit = "fedcba9876543210fedcba9876543210fedcba98"

	for _, seed := range []struct {
		out, branch, tag string
	}{
		{"", "main", "v1.0.0"},
		{fuzzOID + "\trefs/heads/main\n" + fuzzOID + "\trefs/tags/v1.4.2\n" + commit + "\trefs/tags/v1.4.2^{}\n", "x", "y"},
		{"not-an-id\trefs/heads/x\n" + fuzzOID + " refs/heads/y\n" + commit + "\trefs/tags/^{}\n", "feature/a b", "release/v1"},
		{commit + "\trefs/pull/7/head\n" + strings.Repeat("a", 64) + "\trefs/heads/sha256", "^{}", "v^{}"},
	} {
		f.Add([]byte(seed.out), seed.branch, seed.tag)
	}

	f.Fuzz(func(t *testing.T, out []byte, branch, tag string) {
		refs := parseRemoteRefs(out)

		for name, oid := range refs.Heads {
			assert.Regexp(t, objectIDPattern, oid, name)
		}

		for name, tagged := range refs.Tags {
			assert.Regexp(t, objectIDPattern, tagged.Object, name)
			assert.Regexp(t, objectIDPattern, tagged.Commit, name)
			assert.False(t, strings.HasSuffix(name, peelSuffix), "a peeled line names no tag of its own: %q", name)
		}

		if strings.ContainsAny(branch+tag, "\n") || strings.HasSuffix(tag, peelSuffix) {
			return
		}

		listed := fuzzOID + "\t" + headsPrefix + branch + "\n" +
			fuzzOID + "\t" + tagsPrefix + tag + "\n" + commit + "\t" + tagsPrefix + tag + peelSuffix + "\n"

		assert.Equal(t, RemoteRefs{
			Heads: map[string]string{branch: fuzzOID},
			Tags:  map[string]RemoteTag{tag: {Object: fuzzOID, Commit: commit}},
		}, parseRemoteRefs([]byte(listed)))
	})
}
