package worktrees

import (
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maxLockField is the most characters the agent's lock pattern allows in a session name
// or a start.
const maxLockField = 255

// FuzzParsePorcelain checks that any bytes either fail with ErrMalformedPorcelain or
// parse into entries that each have a path, with only the first marked as the main
// worktree, reasons only on locked or prunable entries, and that rendering the entries
// the way git writes them parses back to the same entries.
func FuzzParsePorcelain(f *testing.F) {
	for _, seed := range []string{
		"",
		"\x00",
		"worktree /src/example-repo\x00HEAD abc123\x00branch refs/heads/main\x00\x00" +
			"worktree /src/example-repo/.claude/worktrees/pr-7\x00HEAD def456\x00detached\x00" +
			"locked claude session pr-7 (pid 4242)\x00\x00",
		"worktree /src/example-repo.git\x00bare\x00\x00worktree /tmp/gone\x00HEAD 0\x00prunable gitdir file points to non-existent location\x00\x00",
		"worktree /src/with space\x00locked\x00branch refs/heads/refs/heads/x\x00future attribute\x00\x00",
		"worktree /src/example-repo\nHEAD abc\n\n",
		"HEAD abc\x00\x00",
		"worktree a\x00worktree b\x00\x00",
		"worktree \x00\x00",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		entries, err := ParsePorcelain(data)
		if err != nil {
			require.ErrorIs(t, err, ErrMalformedPorcelain)
			assert.Nil(t, entries)

			return
		}

		checkEntries(t, entries)

		again, err := ParsePorcelain(renderPorcelain(entries))
		require.NoError(t, err)

		// Output without entries parses to nil or to an empty list; callers only range
		// over it.
		if len(entries) == 0 {
			assert.Empty(t, again)

			return
		}

		assert.Equal(t, entries, again)
	})
}

// FuzzParseAgentLock checks that a lock reason the agent pattern accepts parses into a
// PID and start found where the pattern puts them: the reason names the agent, ends with
// "(pid <digits>)" or "(pid <digits> start <start>)" for exactly that PID and start, and
// latestStart gives the later of the start's readings in UTC and in the machine's zone.
func FuzzParseAgentLock(f *testing.F) {
	for _, seed := range []string{
		"",
		"claude session pr-7 (pid 4242)",
		"claude agent issue-12 (pid 99 start Fri Nov  6 17:14:26 2026)",
		"claude session pr-7 (pid 1 start x) (pid 2)",
		"claude session  (pid 0)",
		"claude session pr-7 (pid 99999999999)",
		"claude session pr-7\n(pid 5)",
		"claude session " + strings.Repeat("n", 300) + " (pid 5)",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, reason string) {
		lock, ok := parseAgentLock(reason)
		if !ok {
			assert.Equal(t, agentLock{}, lock)

			return
		}

		assert.GreaterOrEqual(t, lock.pid, 0)
		assert.True(t, strings.HasPrefix(reason, "claude agent ") || strings.HasPrefix(reason, "claude session "))
		checkLockTail(t, reason, lock)
		checkLatestStart(t, lock)
	})
}

// FuzzAgentLockRoundTrip checks that a lock reason written the way the agent writes it,
// from any session name, PID, and start the pattern allows, parses back to that PID and
// start. A start holding "(pid " is left out: with a short session name, the pattern
// reads the last "(pid" group, so such a start is ambiguous by design.
func FuzzAgentLockRoundTrip(f *testing.F) {
	f.Add("pr-7", uint32(4242), "", false)
	f.Add("issue-12", uint32(99), "Fri Nov  6 17:14:26 2026", true)
	f.Add("name (pid 3", uint32(0), "a) start b", false)
	f.Add("unicode été \xff", uint32(4294967295), ")", true)

	f.Fuzz(func(t *testing.T, name string, pid uint32, start string, agent bool) {
		if !lockFieldFits(name) || start != "" && !lockFieldFits(start) || strings.Contains(start, "(pid ") {
			t.Skip("the agent never writes such a name or start")
		}

		verb := "session"
		if agent {
			verb = "agent"
		}

		reason := "claude " + verb + " " + name + " (pid " + strconv.FormatUint(uint64(pid), 10)
		if start != "" {
			reason += " start " + start
		}

		lock, ok := parseAgentLock(reason + ")")
		require.True(t, ok, "reason %q", reason+")")
		assert.Equal(t, agentLock{pid: int(pid), start: start}, lock)
	})
}

// checkEntries checks the guarantees of parsed entries.
func checkEntries(t *testing.T, entries []Entry) {
	t.Helper()

	for i, e := range entries {
		assert.NotEmpty(t, e.Path, "entry %d", i)
		assert.Equal(t, i == 0, e.Main, "entry %d", i)

		if !e.Locked {
			assert.Empty(t, e.LockReason, "entry %d", i)
		}

		if !e.Prunable {
			assert.Empty(t, e.PrunableReason, "entry %d", i)
		}
	}
}

// renderPorcelain writes entries the way `git worktree list --porcelain -z` does.
func renderPorcelain(entries []Entry) []byte {
	var b strings.Builder

	for _, e := range entries {
		for _, attr := range porcelainAttrs(e) {
			b.WriteString(attr)
			b.WriteByte(0)
		}

		b.WriteByte(0)
	}

	return []byte(b.String())
}

// porcelainAttrs lists one entry's attributes, in git's order.
func porcelainAttrs(e Entry) []string {
	attrs := []string{worktreeCmd + " " + e.Path}

	if e.HEAD != "" {
		attrs = append(attrs, "HEAD "+e.HEAD)
	}

	if e.Branch != "" {
		attrs = append(attrs, "branch "+headsPrefix+e.Branch)
	}

	if e.Bare {
		attrs = append(attrs, "bare")
	}

	if e.Detached {
		attrs = append(attrs, "detached")
	}

	if e.Locked {
		attrs = append(attrs, porcelainFlag("locked", e.LockReason))
	}

	if e.Prunable {
		attrs = append(attrs, porcelainFlag("prunable", e.PrunableReason))
	}

	return attrs
}

// porcelainFlag writes a flag attribute, followed by its reason when it has one.
func porcelainFlag(key, reason string) string {
	if reason == "" {
		return key
	}

	return key + " " + reason
}

// checkLockTail checks that reason ends with the PID and start the lock holds.
func checkLockTail(t *testing.T, reason string, lock agentLock) {
	t.Helper()

	tail := ")"
	if lock.start != "" {
		tail = " start " + lock.start + ")"

		assert.True(t, lockFieldFits(lock.start), "start %q", lock.start)
	}

	head, found := strings.CutSuffix(reason, tail)
	require.True(t, found, "%q does not end with %q", reason, tail)

	at := strings.LastIndex(head, " (pid ")
	require.GreaterOrEqual(t, at, 0, "no PID in %q", reason)

	digits := head[at+len(" (pid "):]
	assert.LessOrEqual(t, len(digits), 10)

	pid, err := strconv.Atoi(digits)
	require.NoError(t, err)
	assert.Equal(t, pid, lock.pid)
}

// checkLatestStart checks that latestStart, for a start the agent's layout reads, is the
// later of its readings in UTC and in the machine's zone, here a fixed one behind UTC.
func checkLatestStart(t *testing.T, lock agentLock) {
	t.Helper()

	local := time.FixedZone("UTC-5", -5*60*60)

	latest, ok := lock.latestStart(local)

	asUTC, errUTC := time.ParseInLocation(lockStartLayout, lock.start, time.UTC)
	if lock.start == "" || errUTC != nil {
		assert.False(t, ok)

		return
	}

	asLocal, errLocal := time.ParseInLocation(lockStartLayout, lock.start, local)
	require.NoError(t, errLocal)
	require.True(t, ok)
	assert.False(t, latest.Before(asUTC) || latest.Before(asLocal), "%v is earlier than a reading of %q", latest, lock.start)
	assert.True(t, latest.Equal(asUTC) || latest.Equal(asLocal))
}

// lockFieldFits reports whether s fits the agent pattern's session name or start: 1 to
// 255 characters, none of them a newline. The pattern reads an invalid UTF-8 byte as one
// character, as utf8.RuneCountInString counts it.
func lockFieldFits(s string) bool {
	return s != "" && !strings.ContainsRune(s, '\n') && utf8.RuneCountInString(s) <= maxLockField
}
