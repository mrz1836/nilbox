package release

import (
	"context"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/shellenv"
)

// SignFormat is how git signs a release's tag.
type SignFormat string

// The ways git signs a tag.
const (
	// SignNone is a tag git doesn't sign.
	SignNone SignFormat = ""

	// SignSSH signs with an ssh key: the ssh agent's, or a key file.
	SignSSH SignFormat = "ssh"

	// SignGPG signs with gpg, git's default format (openpgp).
	SignGPG SignFormat = "openpgp"

	// SignX509 signs with an X.509 certificate, through gpgsm.
	SignX509 SignFormat = "x509"
)

const (
	// toolTimeout bounds one ssh-add or ssh-keygen run.
	toolTimeout = 5 * time.Second

	// literalKeyPrefix starts a user.signingkey that is the key itself, and sshKeyPrefix
	// the older form git also reads as one.
	literalKeyPrefix = "key::"
	sshKeyPrefix     = "ssh-"

	// The suffixes ssh-keygen strips from a public key's file to find its private key.
	pubSuffix     = ".pub"
	certPubSuffix = "-cert.pub"

	// pinentryOption is gpg-agent.conf's option naming the program that asks for a
	// passphrase, and gpgAgentConf that file.
	pinentryOption = "pinentry-program"
	gpgAgentConf   = "gpg-agent.conf"
)

// SigningFacts says whether git signs a release's tag here, how, and whether it can do it
// without asking in a terminal.
type SigningFacts struct {
	Format SignFormat

	// AgentHasKey reports, for ssh, that the ssh agent holds the signing key, and OpenKey
	// that the key's file opens without a passphrase.
	AgentHasKey bool
	OpenKey     bool

	// WindowPinentry reports, for gpg, that gpg-agent's pinentry asks in a window.
	WindowPinentry bool
}

// signing reads how git signs a tag in the clone at dir and whether it can here, with
// env, the environment ssh-add and ssh-keygen run with: the login shell's, whose
// SSH_AUTH_SOCK names the agent.
func (g *Gatherer) signing(ctx context.Context, dir string, env []string) (SigningFacts, error) {
	s, err := g.git.Signing(ctx, dir)
	if err != nil || !s.Signed() {
		return SigningFacts{}, err
	}

	switch SignFormat(s.Format) {
	case SignSSH:
		return g.sshSigning(ctx, s.Key, env), nil
	case SignX509:
		return SigningFacts{Format: SignX509}, nil
	case SignNone, SignGPG:
	}

	// openpgp, git's default, or a format git itself would refuse
	return SigningFacts{Format: SignGPG, WindowPinentry: windowPinentry(g.gnupgHome(env))}, nil
}

// sshSigning works out whether ssh can sign with key, user.signingkey as written, without
// asking: the agent lists it (`ssh-add -L`), or its private key file opens without a
// passphrase (ssh-keygen -y, given an empty -P, exits 0; its output is thrown away).
func (g *Gatherer) sshSigning(ctx context.Context, key string, env []string) SigningFacts {
	out := SigningFacts{Format: SignSSH}
	public, private := g.sshKey(key)

	if public != "" && g.agentHas(ctx, env, public) {
		out.AgentHasKey = true

		return out
	}

	if private != "" {
		out.OpenKey = g.opensWithoutPassphrase(ctx, env, private)
	}

	return out
}

// sshKey reads user.signingkey: the public key, as "<type> <key>", when it is the key
// itself or a file that holds it is beside it, and the private key's file, when it names
// one. Neither is "" for an unset key.
func (g *Gatherer) sshKey(key string) (public, private string) {
	key = strings.TrimSpace(key)

	switch {
	case key == "":
		return "", ""
	case strings.HasPrefix(key, literalKeyPrefix):
		return publicKeyID(strings.TrimPrefix(key, literalKeyPrefix)), ""
	case strings.HasPrefix(key, sshKeyPrefix):
		return publicKeyID(key), ""
	}

	path, err := paths.ExpandHome(key, g.home)
	if err != nil || !filepath.IsAbs(path) {
		return "", ""
	}

	for _, suffix := range []string{certPubSuffix, pubSuffix} {
		if base, ok := strings.CutSuffix(path, suffix); ok {
			return readPublicKey(path), base
		}
	}

	return readPublicKey(path + pubSuffix), path
}

// agentHas reports whether the ssh agent env names lists the public key.
func (g *Gatherer) agentHas(ctx context.Context, env []string, public string) bool {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()

	listed, _, err := g.runner(env).Run(ctx, "ssh-add", "-L")
	if err != nil {
		return false // no agent, or one without keys
	}

	for line := range strings.Lines(string(listed)) {
		if publicKeyID(line) == public {
			return true
		}
	}

	return false
}

// opensWithoutPassphrase reports whether the private key file at path opens with an
// empty passphrase. ssh-keygen never asks here: -P gives it the passphrase to try.
func (g *Gatherer) opensWithoutPassphrase(ctx context.Context, env []string, path string) bool {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()

	_, _, err := g.runner(env).Run(ctx, "ssh-keygen", "-y", "-P", "", "-f", path)

	return err == nil
}

// gnupgHome is gpg's home folder: GNUPGHOME in env, else ~/.gnupg.
func (g *Gatherer) gnupgHome(env []string) string {
	if home := shellenv.Lookup(env, "GNUPGHOME"); filepath.IsAbs(home) {
		return home
	}

	return filepath.Join(g.home, ".gnupg")
}

// publicKeyID returns a public key's type and key, "<type> <key>", from a line that holds
// it with or without a comment, or "".
func publicKeyID(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}

	return fields[0] + " " + fields[1]
}

// readPublicKey returns the public key in the file at path, as publicKeyID, or "".
func readPublicKey(path string) string {
	data, err := readHead(path, goModLimit)
	if err != nil {
		return ""
	}

	line, _, _ := strings.Cut(string(data), "\n")

	return publicKeyID(line)
}

// windowPinentry reports whether the gpg-agent.conf in the gpg home folder home names a
// pinentry that asks in a window. Without one, gpg-agent runs its default, which on a Mac
// without GPG Suite asks in a terminal.
func windowPinentry(home string) bool {
	data, err := readHead(filepath.Join(home, gpgAgentConf), goModLimit)
	if err != nil {
		return false
	}

	program := ""

	for line := range strings.Lines(string(data)) {
		name, value := cutOption(line)
		if name == pinentryOption {
			program = value // the last one wins, as in gpg-agent
		}
	}

	return windowProgram(filepath.Base(program))
}

// cutOption splits a gpg-agent.conf line into its option and its value, at the first
// space or tab: the value, a program's path, may hold spaces of its own.
func cutOption(line string) (name, value string) {
	line = strings.TrimSpace(line)

	i := strings.IndexFunc(line, unicode.IsSpace)
	if i < 0 {
		return line, ""
	}

	return line[:i], strings.TrimSpace(line[i:])
}

// windowProgram reports a pinentry, by its file's name, that asks in a window: the Mac's,
// GNOME's, Qt's, GTK's, FLTK's, EFL's, X2Go's, Touch ID's, or Windows'. pinentry-curses,
// pinentry-tty, and plain pinentry ask in a terminal.
func windowProgram(name string) bool {
	name = strings.ToLower(name)

	for _, kind := range []string{"mac", "gnome", "qt", "gtk", "fltk", "efl", "x2go", "touchid", "w32", "wsl"} {
		if strings.Contains(name, "pinentry-"+kind) || strings.Contains(name, "pinentry_"+kind) {
			return true
		}
	}

	return false
}
