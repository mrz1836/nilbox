package desktop

import (
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/update"
)

// DigestAlgo names the digest the desktop runtime's updater checks an update's zip
// against as it copies it: the manifest's SHA-512.
const DigestAlgo = "sha512"

// ErrNotInstallable is returned by NewArtifact for an update the runtime must not be
// handed: no zip, no release's version, no size, or no SHA-512.
var ErrNotInstallable = errors.New("the update can't be handed to the installer")

// Artifact is an update the desktop runtime's updater installs: a zip in the app's update
// cache, already checked against its signed manifest, which the runtime checks again,
// against Size and Digest, as it copies it beside the running bundle.
type Artifact struct {
	// Path is the zip, and Filename its name, which ends in .zip: the runtime extracts only
	// an archive it can tell by its name, and would put the zip itself in the app's place.
	Path     string
	Filename string

	// Version is the release it holds.
	Version string

	// Size is its size in bytes, and Digest its SHA-512, raw.
	Size   int64
	Digest []byte
}

// NewArtifact checks what the update asks the runtime to install, and returns it: the zip
// at zip, whose name ends in .zip, of version, a release's, of size bytes, with sha512Hex
// its SHA-512 in hex, exactly 64 bytes. Without a digest the runtime would install bytes it
// never checked, so anything short of all of that is ErrNotInstallable.
func NewArtifact(zip, version string, size int64, sha512Hex string) (Artifact, error) {
	name := filepath.Base(zip)

	switch {
	case zip == "" || !filepath.IsAbs(zip) || !strings.HasSuffix(strings.ToLower(name), ".zip") || name == ".zip":
		return Artifact{}, fmt.Errorf("%w: %q is not the path of a zip", ErrNotInstallable, zip)
	case !update.ValidVersion(version):
		return Artifact{}, fmt.Errorf("%w: %q is not a release's version", ErrNotInstallable, version)
	case size <= 0:
		return Artifact{}, fmt.Errorf("%w: a size of %d", ErrNotInstallable, size)
	}

	digest, err := hex.DecodeString(sha512Hex)
	if err != nil || len(digest) != sha512.Size {
		return Artifact{}, fmt.Errorf("%w: %q is not a SHA-512", ErrNotInstallable, sha512Hex)
	}

	return Artifact{Path: zip, Filename: name, Version: version, Size: size, Digest: digest}, nil
}
