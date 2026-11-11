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

		// It refuses a Homebrew copy, one in go install's folder, and one it can't write.
		UpdateArgs: []string{"update"},

		// An older hook in a linked worktree checks the main checkout instead.
		MinVersion: "1.12.0",

		Features: map[Feature]string{
			FeatureVerifiedTools:   "1.14.0",
			FeatureInstallExitCode: "1.14.0",
			FeatureStatusJSON:      "1.14.1",
			FeatureRunJSON:         "1.14.1",

			// update arrived on go-selfupdate; before, upgrade was another command.
			FeatureSelfUpdate: "1.10.0",
		},
	}
}
