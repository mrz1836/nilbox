package update

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/semver"
)

var (
	// ErrPlatform is returned for a manifest of another platform's build.
	ErrPlatform = errors.New("the update is for another platform")

	// ErrPrerelease is returned for a manifest of a prerelease: the app updates to
	// releases alone.
	ErrPrerelease = errors.New("the update is a prerelease")
)

// accept checks m as the app takes a release: every field in form and the signature made
// with key's private half (Verify), the build for this platform, and a release's version,
// never a prerelease's. Whether it is newer than the running build is the caller's to ask.
// The form Verify holds every field to already keeps the file's name exactly FileName and
// its size at most MaxFileBytes.
func accept(m Manifest, key ed25519.PublicKey) error {
	if err := Verify(m, key); err != nil {
		return err
	}

	if m.Platform != Platform {
		return fmt.Errorf("%w: %s", ErrPlatform, m.Platform)
	}

	v, ok := semver.Parse(m.Version)
	if !ok {
		return fmt.Errorf("%w: the version %q is not a release's version", ErrInvalid, m.Version)
	}

	if v.Prerelease() {
		return fmt.Errorf("%w: %s", ErrPrerelease, m.Version)
	}

	return nil
}

// newer reports whether version a is newer than b; false when either isn't a version.
func newer(a, b string) bool {
	c, ok := semver.Compare(a, b)

	return ok && c > 0
}

// runtimeKey reads the key the running app trusts: the raw 32 bytes in base64, as
// ReleaseKey holds it, and nothing else, not a PEM block of any kind.
func runtimeKey(text string) (ed25519.PublicKey, error) {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "-----") {
		return nil, fmt.Errorf("%w: not %d bytes of base64", ErrKey, ed25519.PublicKeySize)
	}

	return ParseKey([]byte(text))
}
