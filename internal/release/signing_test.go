package release

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// signingKey is the public key the signing tests sign with, and otherKey another.
const (
	signingKey = "ssh-ed25519 AAAAsigning"
	otherKey   = "ssh-ed25519 AAAAother"
)

// signingRig is a fresh clone with the signing config given, a home folder, and a Gatherer
// whose ssh-add and ssh-keygen are fake.
type signingRig struct {
	g     *Gatherer
	clone string
	home  string
	ssh   *sshFake
	env   []string
}

// newSigningRig makes a clone whose config sets each key to its value, or skips the test
// without git.
func newSigningRig(t *testing.T, ssh *sshFake, config map[string]string) signingRig {
	t.Helper()

	gitEnv := testutil.Git(t)
	r := signingRig{clone: filepath.Join(realTempDir(t), "sdk"), home: realTempDir(t), ssh: ssh}
	r.env = []string{"SSH_AUTH_SOCK=/tmp/agent.sock"}

	testutil.InitRepo(t, r.clone)

	for key, value := range config {
		testutil.RunGit(t, r.clone, "config", key, value)
	}

	r.g = NewGatherer(GatherConfig{
		Git:    gitread.New(gitread.Config{Env: func() []string { return gitEnv }}),
		Runner: ssh.runner, Home: r.home,
	})

	return r
}

// signing reads the rig's clone's signing facts.
func (r signingRig) signing(t *testing.T) SigningFacts {
	t.Helper()

	got, err := r.g.signing(t.Context(), r.clone, r.env)
	require.NoError(t, err)

	return got
}

// sshConfig signs tags with ssh, with key as user.signingkey.
func sshConfig(key string) map[string]string {
	return map[string]string{"tag.gpgSign": "true", "gpg.format": "ssh", "user.signingkey": key}
}

func TestSigningUnsigned(t *testing.T) {
	t.Parallel()

	r := newSigningRig(t, &sshFake{agent: signingKey + "\n"}, map[string]string{"user.signingkey": "~/k.pub"})

	assert.Equal(t, SigningFacts{}, r.signing(t))
	assert.Empty(t, r.ssh.ran())
}

func TestSigningWithSSH(t *testing.T) {
	t.Parallel()

	t.Run("the agent has the key", func(t *testing.T) {
		t.Parallel()

		r := newSigningRig(t, &sshFake{agent: otherKey + " other\n" + signingKey + " me@example.com\n"},
			sshConfig("~/keys/signing.pub"))
		writeIn(t, r.home, "keys/signing.pub", signingKey+" me@example.com\n", 0o600)

		assert.Equal(t, SigningFacts{Format: SignSSH, AgentHasKey: true}, r.signing(t))
		assert.Equal(t, [][]string{{"ssh-add", "-L"}}, r.ssh.ran())
		assert.Equal(t, [][]string{r.env}, r.ssh.envs, "the shell's environment names the agent")
	})

	t.Run("a key file without a passphrase", func(t *testing.T) {
		t.Parallel()

		r := newSigningRig(t, &sshFake{agent: otherKey + "\n", opens: true}, sshConfig("~/keys/signing.pub"))
		writeIn(t, r.home, "keys/signing.pub", signingKey+"\n", 0o600)

		assert.Equal(t, SigningFacts{Format: SignSSH, OpenKey: true}, r.signing(t))
		assert.Equal(t, [][]string{
			{"ssh-add", "-L"}, {"ssh-keygen", "-y", "-P", "", "-f", filepath.Join(r.home, "keys", "signing")},
		}, r.ssh.ran(), "the private key beside the public one")
	})

	t.Run("a key that needs its passphrase", func(t *testing.T) {
		t.Parallel()

		r := newSigningRig(t, &sshFake{}, sshConfig("~/keys/signing.pub"))
		writeIn(t, r.home, "keys/signing.pub", signingKey+"\n", 0o600)

		assert.Equal(t, SigningFacts{Format: SignSSH}, r.signing(t))
	})

	t.Run("a private key with its public one beside it", func(t *testing.T) {
		t.Parallel()

		r := newSigningRig(t, &sshFake{agent: signingKey + "\n"}, sshConfig("~/keys/signing"))
		writeIn(t, r.home, "keys/signing.pub", signingKey+"\n", 0o600)

		assert.Equal(t, SigningFacts{Format: SignSSH, AgentHasKey: true}, r.signing(t))
	})

	t.Run("a private key alone", func(t *testing.T) {
		t.Parallel()

		r := newSigningRig(t, &sshFake{agent: signingKey + "\n", opens: true}, sshConfig("~/keys/alone"))

		assert.Equal(t, SigningFacts{Format: SignSSH, OpenKey: true}, r.signing(t))
		assert.Equal(t, [][]string{{"ssh-keygen", "-y", "-P", "", "-f", filepath.Join(r.home, "keys", "alone")}},
			r.ssh.ran(), "without its public key, the agent can't be asked about it")
	})

	t.Run("a certificate", func(t *testing.T) {
		t.Parallel()

		r := newSigningRig(t, &sshFake{opens: true}, sshConfig("~/keys/signing-cert.pub"))

		assert.Equal(t, SigningFacts{Format: SignSSH, OpenKey: true}, r.signing(t))
		assert.Equal(t, "-f "+filepath.Join(r.home, "keys", "signing"), lastTwo(r.ssh.ran()))
	})

	t.Run("the key itself", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{"key::" + signingKey, signingKey + " me@example.com"} {
			r := newSigningRig(t, &sshFake{agent: signingKey + "\n"}, sshConfig(key))
			assert.Equal(t, SigningFacts{Format: SignSSH, AgentHasKey: true}, r.signing(t), "%s", key)

			r = newSigningRig(t, &sshFake{opens: true}, sshConfig(key))
			assert.Equal(t, SigningFacts{Format: SignSSH}, r.signing(t), "no file to open: %s", key)
			assert.Equal(t, [][]string{{"ssh-add", "-L"}}, r.ssh.ran())
		}
	})

	t.Run("no key", func(t *testing.T) {
		t.Parallel()

		r := newSigningRig(t, &sshFake{agent: signingKey + "\n"},
			map[string]string{"tag.forceSignAnnotated": "true", "gpg.format": "ssh"})

		assert.Equal(t, SigningFacts{Format: SignSSH}, r.signing(t))
		assert.Empty(t, r.ssh.ran())
	})

	t.Run("a relative path", func(t *testing.T) {
		t.Parallel()

		r := newSigningRig(t, &sshFake{opens: true}, sshConfig("keys/signing"))

		assert.Equal(t, SigningFacts{Format: SignSSH}, r.signing(t))
		assert.Empty(t, r.ssh.ran())
	})
}

// lastTwo joins the last run's last two arguments.
func lastTwo(runs [][]string) string {
	if len(runs) == 0 {
		return ""
	}

	last := runs[len(runs)-1]
	if len(last) < 2 {
		return ""
	}

	return last[len(last)-2] + " " + last[len(last)-1]
}

func TestSigningWithGPG(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		conf   string
		window bool
	}{
		{name: "the Mac's pinentry", conf: "pinentry-program /opt/homebrew/bin/pinentry-mac\n", window: true},
		{name: "a terminal's pinentry", conf: "pinentry-program /opt/homebrew/bin/pinentry-curses\n"},
		{
			name: "the last one wins", window: true,
			conf: "# keep\npinentry-program /usr/bin/pinentry-tty\npinentry-program /usr/bin/pinentry-gnome3\n",
		},
		{name: "none named", conf: "default-cache-ttl 600\n"},
		{name: "a tab before the program", conf: "pinentry-program\t/opt/homebrew/bin/pinentry-mac\n", window: true},
		{
			name: "a program in a folder with a space", window: true,
			conf: "  pinentry-program \t/Applications/GPG Keychain.app/Contents/MacOS/pinentry-mac  \n",
		},
		{name: "the option alone", conf: "pinentry-program\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := newSigningRig(t, &sshFake{}, map[string]string{"tag.gpgSign": "true"})
			writeIn(t, r.home, ".gnupg/gpg-agent.conf", tc.conf, 0o600)

			assert.Equal(t, SigningFacts{Format: SignGPG, WindowPinentry: tc.window}, r.signing(t))
			assert.Empty(t, r.ssh.ran())
		})
	}

	t.Run("GNUPGHOME", func(t *testing.T) {
		t.Parallel()

		r := newSigningRig(t, &sshFake{}, map[string]string{"tag.gpgSign": "true", "gpg.format": "openpgp"})
		gnupg := realTempDir(t)
		writeIn(t, gnupg, "gpg-agent.conf", "pinentry-program /usr/local/bin/pinentry-mac\n", 0o600)
		r.env = append(r.env, "GNUPGHOME="+gnupg)

		assert.Equal(t, SigningFacts{Format: SignGPG, WindowPinentry: true}, r.signing(t))
	})

	t.Run("no gpg-agent.conf", func(t *testing.T) {
		t.Parallel()

		r := newSigningRig(t, &sshFake{}, map[string]string{"tag.gpgSign": "true"})

		assert.Equal(t, SigningFacts{Format: SignGPG}, r.signing(t))
	})
}

func TestSigningWithX509(t *testing.T) {
	t.Parallel()

	r := newSigningRig(t, &sshFake{}, map[string]string{"tag.gpgSign": "true", "gpg.format": "x509"})

	assert.Equal(t, SigningFacts{Format: SignX509}, r.signing(t))
}

func TestSigningFailsWithGit(t *testing.T) {
	t.Parallel()

	r := newSigningRig(t, &sshFake{}, map[string]string{"tag.gpgSign": "maybe"})

	_, err := r.g.signing(t.Context(), r.clone, r.env)
	require.Error(t, err, "a switch git can't read")
}

func TestWindowProgram(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"pinentry-mac": true, "pinentry-gnome3": true, "pinentry-qt": true, "pinentry-gtk-2": true,
		"pinentry-touchid": true, "pinentry-curses": false, "pinentry-tty": false, "pinentry": false, "": false,
	} {
		assert.Equal(t, want, windowProgram(name), "%q", name)
	}
}

func TestPublicKeyID(t *testing.T) {
	t.Parallel()

	assert.Equal(t, signingKey, publicKeyID(signingKey+" me@example.com\n"))
	assert.Equal(t, signingKey, publicKeyID("  "+signingKey))
	assert.Empty(t, publicKeyID("ssh-ed25519"))
	assert.Empty(t, publicKeyID(""))
}
