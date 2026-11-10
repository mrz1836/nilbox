package integrations

import (
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/repo"
)

// GoPreCommit is go-pre-commit, which installs and runs the pre-commit checks.
func GoPreCommit() Definition {
	asset, checksums := releaseFiles("go-pre-commit")

	return Definition{
		ID:           integrationprefs.GoPreCommit,
		Name:         "go-pre-commit",
		Binary:       "go-pre-commit",
		Repo:         repo.Name{Owner: "mrz1836", Repo: "go-pre-commit"},
		Purpose:      "Install, update, and run Git pre-commit hooks in your Go repositories.",
		VersionArgs:  []string{"--version"},
		ParseVersion: ParseGoPreCommitVersion,
		Asset:        asset,
		Checksums:    checksums,
		QuietEnv:     quietEnv(map[string]string{"GO_PRE_COMMIT_NO_UPDATE_CHECK": "1"}),

		// An older hook in a linked worktree checks the main checkout instead.
		MinVersion: "1.12.0",

		Features: map[Feature]string{
			FeatureVerifiedTools:   "1.14.0",
			FeatureInstallExitCode: "1.14.0",
			FeatureStatusJSON:      "1.14.1",
			FeatureRunJSON:         "1.14.1",
		},
	}
}
