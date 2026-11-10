package integrations

import (
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/repo"
)

// MageX is mage-x, whose magex tags and pushes the user's releases.
func MageX() Definition {
	asset, checksums := releaseFiles("mage-x")

	return Definition{
		ID:           integrationprefs.MageX,
		Name:         "mage-x",
		Binary:       "magex",
		Repo:         repo.Name{Owner: "mrz1836", Repo: "mage-x"},
		Purpose:      "Release new versions of your repositories in two clicks.",
		VersionArgs:  []string{"--version"},
		ParseVersion: ParseMagexVersion,
		Asset:        asset,
		Checksums:    checksums,
		QuietEnv:     quietEnv(map[string]string{"MAGEX_NO_UPDATE_CHECK": "1"}),

		// The version the release fixtures were recorded with; no older one was compared.
		MinVersion: "1.29.5",

		Features: map[Feature]string{
			FeaturePushFails:       "1.30.0",
			FeatureExpect:          "1.30.0",
			FeatureBumpJSON:        "1.30.1",
			FeatureFastForwardPull: "1.30.2",
		},
	}
}
