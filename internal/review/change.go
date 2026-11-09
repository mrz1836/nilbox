package review

import (
	"path"
	"slices"
	"strings"
)

// maxAreas caps the folders a change names.
const maxAreas = 3

// changeOf judges what kind of change files make, and the folders it changes most. A
// dependency update's title decides first; otherwise every file must be docs, tests, or CI
// for the change to be that kind, and anything else is code.
func changeOf(files []File, isBump bool) Change {
	c := Change{Kind: kindOf(files, isBump), Areas: areasOf(files)}
	c.Label = changeLabel(c.Kind)

	return c
}

// kindOf is the kind of change files make.
func kindOf(files []File, isBump bool) ChangeKind {
	switch {
	case isBump:
		return ChangeDependencies
	case len(files) == 0:
		return ChangeCode
	case allFiles(files, isDependencyFile):
		return ChangeDependencies
	case allFiles(files, isDocsFile):
		return ChangeDocs
	case allFiles(files, isTestFile):
		return ChangeTests
	case allFiles(files, isCIFile):
		return ChangeCI
	default:
		return ChangeCode
	}
}

// changeLabel names a kind of change.
func changeLabel(kind ChangeKind) string {
	switch kind {
	case ChangeDependencies:
		return "Dependency update"
	case ChangeDocs:
		return "Docs only"
	case ChangeTests:
		return "Tests only"
	case ChangeCI:
		return "CI and workflows"
	case ChangeCode:
		return "Code"
	default:
		return "Code"
	}
}

// allFiles reports whether every file's path passes is.
func allFiles(files []File, is func(p string) bool) bool {
	return !slices.ContainsFunc(files, func(f File) bool { return !is(strings.ToLower(f.Path)) })
}

// isDependencyFile reports a manifest or lock file, p in lower case.
func isDependencyFile(p string) bool {
	switch path.Base(p) {
	case "go.mod", "go.sum", "go.work", "go.work.sum", "package.json", "package-lock.json", "pnpm-lock.yaml",
		"yarn.lock", "bun.lock", "bun.lockb", "npm-shrinkwrap.json", "cargo.toml", "cargo.lock", "gemfile",
		"gemfile.lock", "composer.json", "composer.lock", "pyproject.toml", "poetry.lock", "uv.lock", "pipfile",
		"pipfile.lock", "requirements.txt", "pnpm-workspace.yaml":
		return true
	default:
		return strings.HasPrefix(p, "vendor/") || strings.Contains(p, "/vendor/")
	}
}

// isDocsFile reports documentation, p in lower case.
func isDocsFile(p string) bool {
	switch {
	case strings.HasPrefix(p, "docs/") || strings.Contains(p, "/docs/"):
		return true
	case strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".mdx") || strings.HasSuffix(p, ".rst") ||
		strings.HasSuffix(p, ".txt") || strings.HasSuffix(p, ".adoc"):
		return true
	default:
		base := path.Base(p)

		return base == "license" || base == "notice" || base == "authors"
	}
}

// isTestFile reports a test or its data, p in lower case.
func isTestFile(p string) bool {
	base := path.Base(p)

	switch {
	case strings.HasSuffix(base, "_test.go"), strings.Contains(base, ".test."), strings.Contains(base, ".spec."),
		strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"):
		return true
	default:
		for _, dir := range []string{"test/", "tests/", "__tests__/", "testdata/", "spec/"} {
			if strings.HasPrefix(p, dir) || strings.Contains(p, "/"+dir) {
				return true
			}
		}

		return false
	}
}

// isCIFile reports a CI or workflow file, p in lower case.
func isCIFile(p string) bool {
	return strings.HasPrefix(p, ".github/workflows/") || strings.HasPrefix(p, ".github/actions/") ||
		p == ".gitlab-ci.yml" || strings.HasPrefix(p, ".circleci/") || strings.HasPrefix(p, ".buildkite/")
}

// areasOf lists the top folders files change, by lines changed, most first, at most
// maxAreas; "." stands for the root.
func areasOf(files []File) []string {
	lines := map[string]int{}

	var order []string

	for _, f := range files {
		area := "."
		if dir, _, found := strings.Cut(f.Path, "/"); found {
			area = dir
		}

		if _, seen := lines[area]; !seen {
			order = append(order, area)
		}

		lines[area] += max(f.Additions+f.Deletions, 1)
	}

	slices.SortStableFunc(order, func(a, b string) int { return lines[b] - lines[a] })

	if len(order) > maxAreas {
		order = order[:maxAreas]
	}

	if order == nil {
		return []string{}
	}

	return order
}
