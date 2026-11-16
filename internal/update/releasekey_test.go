package update_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/semver"
	"github.com/mrz1836/nilbox/internal/update"
)

// pinnedTrustRoot is the SHA-256 of ReleaseKey as pinned, the trust root of every
// installed copy's updates. Changing the key takes changing this too: two deliberate
// edits, never one.
const pinnedTrustRoot = "97f117b1b57ad8b1b7809e24e9cd06df27a8fec8cfcf070cd6fa3d918d5396b9"

func TestTheReleaseKeyIsPinned(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte(update.ReleaseKey))
	assert.Equal(t, pinnedTrustRoot, hex.EncodeToString(sum[:]),
		"ReleaseKey changed: pin its new SHA-256 here too, deliberately")
}

func TestThePinnedReleaseKeyParses(t *testing.T) {
	t.Parallel()

	if update.ReleaseKey == "" {
		// No key yet: a release build is off, and says why.
		r := newRig(t, func(c *update.Config) { c.Flavor = update.FlavorRelease })
		assert.False(t, r.engine.Enabled())
		assert.Contains(t, r.status(t).Off, "no release key pinned")

		return
	}

	assert.False(t, strings.HasPrefix(strings.TrimSpace(update.ReleaseKey), "-----"), "the raw key, not a PEM block")

	key, err := update.ParseKey([]byte(update.ReleaseKey))
	require.NoError(t, err, "the pinned key parses, and isn't one of the small-order points")
	assert.Len(t, key, ed25519.PublicKeySize)
	assert.True(t, newRig(t, func(c *update.Config) { c.Flavor = update.FlavorRelease }).engine.Enabled())
}

func TestTheManifestsVersionRuleAndSemverAgree(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"0.3.0", "10.20.30", "0.2.0-rc.1", "1.0.0-alpha", "1.0.0-x-y.0", "1.0.0-rc.01", "01.0.0", "1.0", "1.0.0-",
		"1.0.0-rc..1", "1.0.0-é", "v1.0.0", "1.0.0+build",
	} {
		_, ok := semver.Parse(version)
		valid := update.ValidVersion(version)

		if valid {
			assert.True(t, ok, "a manifest's version %q is semver's too", version)
		}

		if !ok && !strings.HasPrefix(version, "v") {
			assert.False(t, valid, "%q, which semver refuses, is no manifest's version", version)
		}
	}
}
