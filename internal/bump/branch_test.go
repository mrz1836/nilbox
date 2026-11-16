package bump_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/bump"
)

func TestParseBranch(t *testing.T) {
	t.Parallel()

	dependabot := func(e bump.Ecosystem) bump.Branch { return bump.Branch{Tool: bump.ToolDependabot, Ecosystem: e} }

	tests := []struct {
		branch string
		want   bump.Branch
	}{
		{branch: "dependabot/go_modules/golang.org/x/net-0.31.0", want: dependabot(bump.EcosystemGo)},
		{branch: "dependabot/npm_and_yarn/frontend/typescript-6.0.3", want: dependabot(bump.EcosystemNPM)},
		{branch: "dependabot/github_actions/actions/checkout-5", want: dependabot(bump.EcosystemActions)},
		{branch: "dependabot/github_actions/github-actions-c0ffee1234", want: dependabot(bump.EcosystemActions)},
		{branch: "dependabot/docker/node-22-alpine", want: dependabot(bump.EcosystemDocker)},
		{branch: "dependabot/docker_compose/redis-7.4", want: dependabot(bump.EcosystemDockerCompose)},
		{branch: "dependabot/pip/requests-2.32.3", want: dependabot(bump.EcosystemPip)},
		{branch: "dependabot/bundler/rails-7.1.3", want: dependabot(bump.EcosystemBundler)},
		{branch: "dependabot/cargo/serde-1.0.200", want: dependabot(bump.EcosystemCargo)},
		{branch: "dependabot/submodules/vendor/lib-1a2b3c4", want: dependabot(bump.EcosystemSubmodules)},
		{branch: "dependabot/hex/phoenix-1.7.14", want: dependabot(bump.EcosystemMix)},
		{branch: "dependabot/go_modules/go-deps-4a1b2c3d4e", want: dependabot(bump.EcosystemGo)},
		{branch: "dependabot/npm_and_yarn/web/multi-9f8e7d6c5b", want: dependabot(bump.EcosystemNPM)},
		{branch: "dependabot-npm_and_yarn-lodash-4.17.21", want: dependabot(bump.EcosystemNPM)},
		{branch: "dependabot_go_modules_golang.org_x_net-0.31.0", want: dependabot(bump.EcosystemGo)},
		{branch: "dependabot_docker_compose_redis-7.4", want: dependabot(bump.EcosystemDockerCompose)},
		{branch: "dependabot/vcpkg/zlib-1.3.1", want: dependabot("vcpkg")},
		{branch: "  dependabot/pip/requests-2.32.3  ", want: dependabot(bump.EcosystemPip)},
		{branch: "renovate/lodash-4.x", want: bump.Branch{Tool: bump.ToolRenovate}},
		{branch: "renovate/major-react-monorepo", want: bump.Branch{Tool: bump.ToolRenovate}},
		{branch: "renovate/golang.org-x-net-0.x", want: bump.Branch{Tool: bump.ToolRenovate}},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			t.Parallel()

			got, ok := bump.ParseBranch(tt.branch)
			assert.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseBranchRefusesAnUnreadableBranch(t *testing.T) {
	t.Parallel()

	for _, branch := range []string{
		"", "main", "feature/login", "dependabot", "dependabot/", "dependabot/go_modules/", "dependabot/go_modules",
		"dependabotx/pip/requests-2", "dependabot-unknownthing-x-1", "dependabot/Not A Word/x", "renovate/", "renovate",
		"my-dependabot/pip/requests-2", "dependabot_vcpkg_zlib-1",
	} {
		got, ok := bump.ParseBranch(branch)
		assert.False(t, ok, branch)
		assert.Equal(t, bump.Branch{}, got, branch)
	}
}

func TestEcosystems(t *testing.T) {
	t.Parallel()

	seen := map[bump.Ecosystem]bool{}

	for _, e := range bump.Ecosystems() {
		assert.False(t, seen[e.ID], "%s is listed once", e.ID)
		seen[e.ID] = true

		assert.NotEmpty(t, e.Label, e.ID)
		assert.Equal(t, strings.ToLower(string(e.ID)), string(e.ID))
	}

	assert.True(t, seen[bump.EcosystemGo])
}

// FuzzParseBranch checks that any branch parses without a panic, and that a branch read
// as a bot's names that bot, an ecosystem only for Dependabot's, and nothing for one
// it can't read.
func FuzzParseBranch(f *testing.F) {
	for _, seed := range []string{
		"dependabot/go_modules/golang.org/x/net-0.31.0", "dependabot-npm_and_yarn-x-1", "dependabot_docker_compose_x",
		"renovate/x", "dependabot/", "dependabot", "", "\xff", "dependabot/a/b",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, branch string) {
		got, ok := bump.ParseBranch(branch)
		if !ok {
			assert.Equal(t, bump.Branch{}, got)

			return
		}

		assert.Contains(t, []bump.Tool{bump.ToolDependabot, bump.ToolRenovate}, got.Tool)

		if got.Tool == bump.ToolRenovate {
			assert.Equal(t, bump.EcosystemUnknown, got.Ecosystem)
		} else {
			assert.NotEqual(t, bump.EcosystemUnknown, got.Ecosystem)
		}
	})
}
