//go:build mage

package main

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errStep stands in for a packaging step that fails.
var errStep = errors.New("the step failed")

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

//nolint:paralleltest // changes the process's environment and PATH
func TestWithKeychain(t *testing.T) {
	const keychains = "/Library/Keychains/My Keys.keychain-db /Library/Keychains/System.keychain"

	t.Run("puts the keychain first while fn runs, then the list back", func(t *testing.T) {
		_, log := releaseTools(t, "")

		var during string

		err := withKeychain(testKeychain, func() error {
			during = readFile(t, os.Getenv("KEYCHAIN_LIST"))

			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, "    \""+testKeychain+"\"\n"+originalKeychains, during)
		assert.Equal(t, originalKeychains, readFile(t, os.Getenv("KEYCHAIN_LIST")))
		assert.Equal(t, []string{
			"security list-keychains -d user",
			"security list-keychains -d user -s " + testKeychain + " " + keychains,
			"security list-keychains -d user -s " + keychains,
		}, logLines(t, log))
	})

	t.Run("puts the list back when fn fails", func(t *testing.T) {
		releaseTools(t, "")

		err := withKeychain(testKeychain, func() error { return errStep })

		require.ErrorIs(t, err, errStep)
		assert.Equal(t, originalKeychains, readFile(t, os.Getenv("KEYCHAIN_LIST")))
	})

	t.Run("a keychain already on the list moves first, once", func(t *testing.T) {
		releaseTools(t, "")

		var during string

		require.NoError(t, withKeychain("/Library/Keychains/System.keychain", func() error {
			during = readFile(t, os.Getenv("KEYCHAIN_LIST"))

			return nil
		}))

		assert.Equal(t, "    \"/Library/Keychains/System.keychain\"\n    \"/Library/Keychains/My Keys.keychain-db\"\n", during)
		assert.Equal(t, originalKeychains, readFile(t, os.Getenv("KEYCHAIN_LIST")))
	})

	t.Run("no keychain leaves the list alone", func(t *testing.T) {
		_, log := releaseTools(t, "")

		ran := false

		require.NoError(t, withKeychain("", func() error {
			ran = true

			return nil
		}))

		assert.True(t, ran)
		assert.NoFileExists(t, log)
	})

	t.Run("a list that can't be read runs nothing", func(t *testing.T) {
		tools, _ := releaseTools(t, "")
		fakeTool(t, tools, "security", "exit 1")

		err := withKeychain(testKeychain, func() error {
			t.Error("fn ran without its keychain on the list")

			return nil
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the keychain search list")
	})

	t.Run("a list security prints otherwise runs nothing", func(t *testing.T) {
		tools, _ := releaseTools(t, "")
		fakeTool(t, tools, "security", "echo not-quoted")

		require.ErrorIs(t, withKeychain(testKeychain, func() error { return nil }), errKeychainList)
	})

	t.Run("a list that can't be set runs nothing and is set back", func(t *testing.T) {
		tools, log := releaseTools(t, "")
		fakeTool(t, tools, "security", `printf 'security %s\n' "$*" >> "$TOOLS_LOG"
if [ "$#" -eq 3 ]; then /bin/cat "$KEYCHAIN_LIST"; exit 0; fi
exit 1`)

		err := withKeychain(testKeychain, func() error {
			t.Error("fn ran without its keychain on the list")

			return nil
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "add the keychain to the search list")
		assert.Equal(t, "security list-keychains -d user -s "+keychains, logLines(t, log)[2], "it tries to set the list back")
	})

	t.Run("a list that can't be set back says so", func(t *testing.T) {
		tools, _ := releaseTools(t, "")
		fakeTool(t, tools, "security", `if [ "$#" -eq 3 ]; then /bin/cat "$KEYCHAIN_LIST"; exit 0; fi
case "$*" in *"`+testKeychain+`"*) exit 0 ;; esac
exit 1`)

		err := withKeychain(testKeychain, func() error { return errStep })

		require.ErrorIs(t, err, errStep)
		assert.Contains(t, err.Error(), "put the keychain search list back")
	})
}

//nolint:paralleltest // signals reach the whole process
func TestOnInterrupt(t *testing.T) {
	t.Run("an interrupt runs fn, then goes on to whoever else hears it", func(t *testing.T) {
		// The test hears the interrupt too, as mage does, so delivering it again doesn't
		// end the test binary.
		heard := make(chan os.Signal, 2)
		signal.Notify(heard, os.Interrupt)
		t.Cleanup(func() { signal.Stop(heard) })

		ran := make(chan struct{})
		stop := onInterrupt(func() { close(ran) })
		t.Cleanup(stop)

		require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGINT))

		select {
		case <-ran:
		case <-time.After(5 * time.Second):
			t.Fatal("fn never ran")
		}

		for range 2 {
			select {
			case <-heard:
			case <-time.After(5 * time.Second):
				t.Fatal("the interrupt was not delivered again")
			}
		}
	})

	t.Run("after stop, fn never runs", func(t *testing.T) {
		ran := false
		stop := onInterrupt(func() { ran = true })

		stop()
		stop()

		assert.False(t, ran)
	})
}
