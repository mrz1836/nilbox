//go:build mage

package main

import (
	"errors"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// errStep stands in for a packaging step that fails.
var errStep = errors.New("the step failed")

// keychainList is the user's keychain search list as a fake security keeps it: starting
// at originalKeychains, printed by `list-keychains -d user`, one quoted path a line, and
// set by `list-keychains -d user -s`.
type keychainList struct {
	mu   sync.Mutex
	list []string
}

// newKeychainList returns a search list holding originalKeychains.
func newKeychainList() *keychainList {
	return &keychainList{list: []string{"/Library/Keychains/My Keys.keychain-db", "/Library/Keychains/System.keychain"}}
}

// printed is the list as security prints it.
func (k *keychainList) printed() string {
	k.mu.Lock()
	defer k.mu.Unlock()

	var b strings.Builder

	for _, path := range k.list {
		b.WriteString("    \"" + path + "\"\n")
	}

	return b.String()
}

// answer is the fake security's answer: it prints the list, or sets it, and fails any
// other command.
func (k *keychainList) answer(args []string) testutil.FakeRun {
	switch {
	case slices.Equal(args, []string{"list-keychains", "-d", "user"}):
		return testutil.FakeRun{Stdout: k.printed()}
	case len(args) >= 4 && slices.Equal(args[:4], []string{"list-keychains", "-d", "user", "-s"}):
		k.mu.Lock()
		k.list = slices.Clone(args[4:])
		k.mu.Unlock()

		return testutil.FakeRun{}
	default:
		return testutil.FakeRun{Exit: 1}
	}
}

// keychainRig returns a rig with a fake security that keeps a fresh search list, and the
// list.
func keychainRig(t *testing.T) (*rig, *keychainList) {
	t.Helper()

	g := newRig(t)
	k := newKeychainList()
	g.tool("security", k.answer)

	return g, k
}

func TestParseKeychainList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		out     string
		want    []string
		wantErr bool
	}{
		{name: "paths with spaces, one a line", out: originalKeychains, want: []string{
			"/Library/Keychains/My Keys.keychain-db", "/Library/Keychains/System.keychain",
		}},
		{name: "no trailing newline", out: `    "/Library/Keychains/System.keychain"`, want: []string{"/Library/Keychains/System.keychain"}},
		{name: "an empty list", out: "", want: []string{}},
		{name: "an unquoted line", out: "    /Library/Keychains/System.keychain\n", wantErr: true},
		{name: "an empty quoted path", out: "    \"\"\n", wantErr: true},
		{name: "a lone quote", out: "    \"\n", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseKeychainList(tt.out)

			if tt.wantErr {
				require.ErrorIs(t, err, errKeychainList)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWithKeychain(t *testing.T) {
	t.Parallel()

	const keychains = "/Library/Keychains/My Keys.keychain-db /Library/Keychains/System.keychain"

	t.Run("puts the keychain first while fn runs, then the list back", func(t *testing.T) {
		t.Parallel()

		g, k := keychainRig(t)

		var during string

		err := g.runner().withKeychain(testKeychain, func() error {
			during = k.printed()

			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, "    \""+testKeychain+"\"\n"+originalKeychains, during)
		assert.Equal(t, originalKeychains, k.printed())
		assert.Equal(t, []string{
			"security list-keychains -d user",
			"security list-keychains -d user -s " + testKeychain + " " + keychains,
			"security list-keychains -d user -s " + keychains,
		}, g.lines())
	})

	t.Run("puts the list back when fn fails", func(t *testing.T) {
		t.Parallel()

		g, k := keychainRig(t)

		err := g.runner().withKeychain(testKeychain, func() error { return errStep })

		require.ErrorIs(t, err, errStep)
		assert.Equal(t, originalKeychains, k.printed())
	})

	t.Run("a keychain already on the list moves first, once", func(t *testing.T) {
		t.Parallel()

		g, k := keychainRig(t)

		var during string

		require.NoError(t, g.runner().withKeychain("/Library/Keychains/System.keychain", func() error {
			during = k.printed()

			return nil
		}))

		assert.Equal(t, "    \"/Library/Keychains/System.keychain\"\n    \"/Library/Keychains/My Keys.keychain-db\"\n", during)
		assert.Equal(t, originalKeychains, k.printed())
	})

	t.Run("no keychain leaves the list alone", func(t *testing.T) {
		t.Parallel()

		g, _ := keychainRig(t)

		ran := false

		require.NoError(t, g.runner().withKeychain("", func() error {
			ran = true

			return nil
		}))

		assert.True(t, ran)
		assert.Empty(t, g.lines())
	})

	t.Run("a list that can't be read runs nothing", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool("security", exits(1))

		err := g.runner().withKeychain(testKeychain, func() error {
			t.Error("fn ran without its keychain on the list")

			return nil
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the keychain search list")
	})

	t.Run("a list security prints otherwise runs nothing", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.fixed("security", testutil.FakeRun{Stdout: "not-quoted\n"})

		require.ErrorIs(t, g.runner().withKeychain(testKeychain, func() error { return nil }), errKeychainList)
	})

	t.Run("a list that can't be set runs nothing and is set back", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		k := newKeychainList()

		g.tool("security", func(args []string) testutil.FakeRun {
			if len(args) == 3 {
				return k.answer(args)
			}

			return testutil.FakeRun{Exit: 1}
		})

		err := g.runner().withKeychain(testKeychain, func() error {
			t.Error("fn ran without its keychain on the list")

			return nil
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "add the keychain to the search list")
		assert.Equal(t, "security list-keychains -d user -s "+keychains, g.lines()[2], "it tries to set the list back")
	})

	t.Run("a list that can't be set back says so", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		k := newKeychainList()

		g.tool("security", func(args []string) testutil.FakeRun {
			if len(args) == 3 || slices.Contains(args, testKeychain) {
				return k.answer(args)
			}

			return testutil.FakeRun{Exit: 1}
		})

		err := g.runner().withKeychain(testKeychain, func() error { return errStep })

		require.ErrorIs(t, err, errStep)
		assert.Contains(t, err.Error(), "put the keychain search list back")
	})
}

// signalWait is how long the interrupt tests wait for a signal to arrive.
const signalWait = 5 * time.Second

// hear makes the test hear sig as mage would, so a signal delivered again doesn't end the
// test binary, and returns the channel it arrives on.
func hear(t *testing.T, sig os.Signal) chan os.Signal {
	t.Helper()

	heard := make(chan os.Signal, 2)
	signal.Notify(heard, sig)
	t.Cleanup(func() { signal.Stop(heard) })

	return heard
}

// await fails the test unless ch receives within signalWait.
func await[T any](t *testing.T, ch <-chan T, what string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(signalWait):
		t.Fatal(what)
	}
}

//nolint:paralleltest // signals reach the whole process
func TestOnInterrupt(t *testing.T) {
	t.Run("Control-C runs fn and is left to mage, never sent again", func(t *testing.T) {
		heard := hear(t, os.Interrupt)

		ran := make(chan struct{})
		stop := onInterrupt(func() { close(ran) })
		t.Cleanup(stop)

		require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGINT))

		await(t, ran, "fn never ran")
		await(t, heard, "mage never heard the interrupt")

		select {
		case <-heard:
			t.Fatal("the interrupt was sent again, which mage takes as a second Control-C")
		case <-time.After(200 * time.Millisecond):
		}
	})

	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String()+" runs fn, then is delivered again", func(t *testing.T) {
			heard := hear(t, sig)

			ran := make(chan struct{})
			stop := onInterrupt(func() { close(ran) })
			t.Cleanup(stop)

			require.NoError(t, syscall.Kill(os.Getpid(), sig))

			await(t, ran, "fn never ran")
			await(t, heard, "the signal never arrived")
			await(t, heard, "the signal was not delivered again")
		})
	}

	t.Run("after stop, fn never runs", func(t *testing.T) {
		ran := false
		stop := onInterrupt(func() { ran = true })

		stop()
		stop()

		assert.False(t, ran)
	})
}
