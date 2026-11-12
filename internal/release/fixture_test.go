package release

import (
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// sdkFixture shapes the origin and clone a rig starts from: the clone's folder, beside
// origin.git, the version tagged on its first commit, and go.mod's module path, committed
// with the second, when set.
type sdkFixture struct {
	clone  string
	base   string
	module string
}

// sdkCommits are a fixture's commits on master: first, tagged with its base version, and
// second, the head of the clone and of origin.
type sdkCommits struct {
	first  string
	second string
}

// sdkFixtures are the rigs' origins and clones, each built once for the test binary by
// buildSDK and copied into each test that starts from it.
var sdkFixtures = testutil.NewGitTemplates(buildSDK) //nolint:gochecknoglobals // built once for the whole test binary

// buildSDK builds f: a local bare origin, origin.git, and a clone made with git init, on
// master one commit past the first, tagged f.base, which pushed its branch and tags to
// origin and follows origin's master.
func buildSDK(b *testutil.GitBuild, f sdkFixture) sdkCommits {
	origin := filepath.Join(b.Root, "origin.git")

	b.Git(".", "-c", "init.defaultBranch=master", "init", "-q", "--bare", origin)
	b.Git(".", "-c", "init.defaultBranch=master", "init", "-q", f.clone)

	first := commitTo(b, f.clone, "one.txt")
	b.Git(f.clone, "tag", "-a", f.base, "-m", f.base)

	if f.module != "" {
		b.WriteFile(filepath.Join(f.clone, "go.mod"), "module "+f.module+"\n")
		b.Git(f.clone, "add", "go.mod")
	}

	second := commitTo(b, f.clone, "two.txt")
	b.Git(f.clone, "remote", "add", "origin", origin)
	b.Git(f.clone, "push", "-q", "-u", "origin", branchMaster, "--tags")

	return sdkCommits{first: first, second: second}
}

// commitTo writes name in dir and commits it, as commitIn does, returning the commit.
func commitTo(b *testutil.GitBuild, dir, name string) string {
	b.WriteFile(filepath.Join(dir, name), name+"\n")
	b.Git(dir, "add", name)
	b.Git(dir, "-c", "user.name=Example", "-c", "user.email=example@example.com", "commit", "-q", "-m", name)

	return b.Git(dir, "rev-parse", "HEAD")
}
