package desktop_test

import (
	"crypto/sha512"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/desktop"
)

func TestAnArtifactCarriesTheZipAndItsDigest(t *testing.T) {
	t.Parallel()

	sum := sha512.Sum512([]byte("the zip"))
	zip := "/data/updates/0.3.0/" + brand.Name + "_0.3.0_darwin_universal.zip"

	got, err := desktop.NewArtifact(zip, nextVersion, 7, hex.EncodeToString(sum[:]))
	require.NoError(t, err)

	assert.Equal(t, desktop.Artifact{
		Path: zip, Filename: brand.Name + "_0.3.0_darwin_universal.zip", Version: nextVersion, Size: 7, Digest: sum[:],
	}, got)
	assert.Equal(t, "sha512", desktop.DigestAlgo)
}

func TestAnArtifactWithoutEverythingIsNeverHandedOver(t *testing.T) {
	t.Parallel()

	sum := sha512.Sum512([]byte("the zip"))
	digest := hex.EncodeToString(sum[:])
	zip := "/data/updates/0.3.0/" + brand.Name + "_0.3.0_darwin_universal.zip"

	tests := []struct {
		name    string
		zip     string
		version string
		size    int64
		sha512  string
	}{
		{name: "no digest", zip: zip, version: nextVersion, size: 7, sha512: ""},
		{name: "a digest that isn't hex", zip: zip, version: nextVersion, size: 7, sha512: strings.Repeat("z", 128)},
		{name: "a SHA-256 in its place", zip: zip, version: nextVersion, size: 7, sha512: strings.Repeat("ab", 32)},
		{name: "a digest too long", zip: zip, version: nextVersion, size: 7, sha512: digest + "00"},
		{name: "no zip", zip: "", version: nextVersion, size: 7, sha512: digest},
		{name: "a file that isn't a zip", zip: "/data/updates/0.3.0/" + brand.Name + ".app", version: nextVersion, size: 7, sha512: digest},
		{name: "a name that is only the suffix", zip: "/data/updates/0.3.0/.zip", version: nextVersion, size: 7, sha512: digest},
		{name: "a relative path", zip: "updates/x.zip", version: nextVersion, size: 7, sha512: digest},
		{name: "a tag, not a version", zip: zip, version: "v0.3.0", size: 7, sha512: digest},
		{name: "no version", zip: zip, version: "", size: 7, sha512: digest},
		{name: "no size", zip: zip, version: nextVersion, size: 0, sha512: digest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := desktop.NewArtifact(tt.zip, tt.version, tt.size, tt.sha512)
			require.ErrorIs(t, err, desktop.ErrNotInstallable)
			assert.Equal(t, desktop.Artifact{}, got)
		})
	}
}
