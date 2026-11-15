package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
)

const (
	// Format is the manifest format's version, which the first line of the signed message
	// names. Only a change an older build would misread raises it.
	Format = 1

	// Platform is the build every release ships today: one universal macOS bundle, for
	// Apple silicon and Intel alike.
	Platform = "darwin-universal"

	// ManifestName is the manifest's asset name for Platform: one manifest per operating
	// system, so another system's builds add a file of their own, never a format change.
	ManifestName = "manifest-darwin.json"

	// SumsName is the asset that lists the SHA-256 of the zip and the disk image, for
	// people checking a download by hand; the app never reads it.
	SumsName = "SHA256SUMS"

	// MaxManifestBytes is the largest manifest that is read at all: 16 KiB.
	MaxManifestBytes = 16 << 10

	// MaxFileBytes is the largest zip a manifest may name: 200 MiB.
	MaxFileBytes = 200 << 20
)

var (
	// ErrInvalid is returned for a manifest that isn't one: not JSON, larger than
	// MaxManifestBytes, or a field out of form.
	ErrInvalid = errors.New("not a valid update manifest")

	// ErrSignature is returned when a manifest's signature does not verify with the key.
	ErrSignature = errors.New("the update manifest's signature does not verify")

	// ErrKey is returned for a key that isn't an Ed25519 key in a form this package reads.
	ErrKey = errors.New("not an Ed25519 key")

	// ErrMismatch is returned when a file's size or SHA-512 differs from its manifest's.
	ErrMismatch = errors.New("the file does not match its update manifest")
)

// versionPattern matches a release's version as its tag names it, without the v:
// MAJOR.MINOR.PATCH, with a prerelease's suffix when it has one (0.2.0-rc.1), and no build
// metadata. As semver requires, no number (a prerelease's numeric identifiers included)
// has a leading zero.
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
	`(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$`)

// platformPattern matches a platform's name: lowercase words joined by hyphens.
var platformPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// digestPattern matches a SHA-512 written as lowercase hex.
var digestPattern = regexp.MustCompile(`^[0-9a-f]{128}$`)

// Manifest describes the zip one release's update installs. The signature covers every
// other field, so none of them can be changed, a version relabeled, or a file swapped,
// without the key.
type Manifest struct {
	// Version is the release's version, without a leading v, such as 0.3.0.
	Version string `json:"version"`

	// Platform is the build the zip holds, such as Platform.
	Platform string `json:"platform"`

	// File is the zip's asset name, exactly FileName(Platform, Version).
	File string `json:"file"`

	// Size is the zip's size in bytes.
	Size int64 `json:"size"`

	// SHA512 is the zip's SHA-512, as 128 lowercase hex characters.
	SHA512 string `json:"sha512"`

	// Signature is the Ed25519 signature of SignedMessage, base64.
	Signature string `json:"signature"`
}

// Assets are the names of the files a release attaches.
type Assets struct {
	// Zip is the zipped app bundle, which updates install.
	Zip string

	// DMG is the disk image first installs open. Its name holds no version, so a link to
	// the latest release's copy never changes.
	DMG string

	// Sums is SumsName.
	Sums string

	// Manifest is ManifestName, which the app reads before anything else.
	Manifest string
}

// AssetNames returns the names of the files the release of version attaches for Platform.
func AssetNames(version string) Assets {
	return Assets{
		Zip:      FileName(Platform, version),
		DMG:      brand.Name + ".dmg",
		Sums:     SumsName,
		Manifest: ManifestName,
	}
}

// FileName is the zip's asset name for platform and version, such as
// <Name>_0.3.0_darwin_universal.zip for Platform.
func FileName(platform, version string) string {
	return brand.Name + "_" + version + "_" + strings.ReplaceAll(platform, "-", "_") + ".zip"
}

// ValidVersion reports whether version is a release's version as its tag names it,
// without the v: 0.3.0, or a prerelease such as 0.3.0-rc.1.
func ValidVersion(version string) bool {
	return versionPattern.MatchString(version)
}

// SignedMessage is the text a manifest's signature covers: fixed lines, with no JSON to
// canonicalize. The first names the format, so a signature made for one format never
// verifies as another's.
func SignedMessage(m Manifest) []byte {
	return fmt.Appendf(nil, "%s update %d\nplatform %s\nversion %s\nfile %s\nsize %d\nsha512 %s\n",
		brand.Slug, Format, m.Platform, m.Version, m.File, m.Size, m.SHA512)
}

// Sign returns m signed with key. It refuses a manifest whose fields are out of form,
// so it never signs one Verify would refuse.
func Sign(m Manifest, key ed25519.PrivateKey) (Manifest, error) {
	if len(key) != ed25519.PrivateKeySize {
		return Manifest{}, fmt.Errorf("%w: a private key is %d bytes, not %d", ErrKey, ed25519.PrivateKeySize, len(key))
	}

	if err := checkForm(m); err != nil {
		return Manifest{}, err
	}

	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, SignedMessage(m)))

	return m, nil
}

// Verify checks m: every field in form, and the signature made by key's private half
// over SignedMessage(m). It refuses a key ParseKey would refuse (a point of small order
// among them). Whether the app takes the release, its platform and version against the
// running build's, is the caller's to decide.
func Verify(m Manifest, key ed25519.PublicKey) error {
	if err := checkPublicKey(key); err != nil {
		return err
	}

	if err := checkForm(m); err != nil {
		return err
	}

	sig, err := base64.StdEncoding.Strict().DecodeString(m.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: the signature is not %d bytes of base64", ErrInvalid, ed25519.SignatureSize)
	}

	if !ed25519.Verify(key, SignedMessage(m), sig) {
		return ErrSignature
	}

	return nil
}

// Parse reads a manifest from its JSON, refusing one larger than MaxManifestBytes, a
// field the format doesn't have, or anything after the object. It checks no field's
// value: Verify does.
func Parse(data []byte) (Manifest, error) {
	if len(data) > MaxManifestBytes {
		return Manifest{}, fmt.Errorf("%w: larger than %d bytes", ErrInvalid, MaxManifestBytes)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Manifest{}, fmt.Errorf("%w: more after the manifest", ErrInvalid)
	}

	return m, nil
}

// Encode returns m as the JSON a release attaches: indented, ending in a newline.
func Encode(m Manifest) ([]byte, error) {
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the update manifest: %w", err)
	}

	return append(body, '\n'), nil
}

// Digest reads r to its end and returns how many bytes it held and their SHA-512, as a
// manifest writes it.
func Digest(r io.Reader) (int64, string, error) {
	h := sha512.New()

	n, err := io.Copy(h, r)
	if err != nil {
		return 0, "", fmt.Errorf("read the file: %w", err)
	}

	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// CheckFile reads r, the file m names, and fails with ErrMismatch unless it holds exactly
// m.Size bytes whose SHA-512 is m.SHA512. It reads at most one byte past m.Size, so a file
// that runs on is refused without being read to its end. Call it only on a manifest Verify
// accepted: Verify is what holds m.Size to MaxFileBytes, and so bounds the read.
func CheckFile(m Manifest, r io.Reader) error {
	size, sum, err := Digest(io.LimitReader(r, m.Size+1))
	if err != nil {
		return err
	}

	switch {
	case size != m.Size:
		return fmt.Errorf("%w: %s holds a different number of bytes than the manifest's %d", ErrMismatch, m.File, m.Size)
	case sum != m.SHA512:
		return fmt.Errorf("%w: the SHA-512 of %s differs from the manifest's", ErrMismatch, m.File)
	}

	return nil
}

// checkForm fails with ErrInvalid, naming the field, unless each of m's signed fields is
// in form. The form keeps every field to one line, so no field can pass for another in
// the signed message.
func checkForm(m Manifest) error {
	var problem string

	switch {
	case !platformPattern.MatchString(m.Platform):
		problem = fmt.Sprintf("the platform %q is not a platform's name", m.Platform)
	case !ValidVersion(m.Version):
		problem = fmt.Sprintf("the version %q is not a release's version", m.Version)
	case m.File != FileName(m.Platform, m.Version):
		problem = fmt.Sprintf("the file %q is not %s", m.File, FileName(m.Platform, m.Version))
	case m.Size < 1 || m.Size > MaxFileBytes:
		problem = fmt.Sprintf("the size %d is not between 1 and %d bytes", m.Size, MaxFileBytes)
	case !digestPattern.MatchString(m.SHA512):
		problem = "the SHA-512 is not 128 lowercase hex characters"
	default:
		return nil
	}

	return fmt.Errorf("%w: %s", ErrInvalid, problem)
}
