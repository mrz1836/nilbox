package repoguard_test

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	// docLink matches the target of a markdown link or image.
	docLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

	// docHref matches the target of an HTML link, as README's contents and badges use.
	docHref = regexp.MustCompile(`href="([^"]+)"`)

	// codeSpan matches inline code, whose text is never a link.
	codeSpan = regexp.MustCompile("`[^`]*`")
)

// docLinkAt is one link of a doc and the line it is on.
type docLinkAt struct {
	line   int
	target string
}

// isDoc reports whether a repository file is one the docs guards read: the project guide,
// the README, or a markdown file under docs/.
func isDoc(rel string) bool {
	return rel == "CLAUDE.md" || rel == "README.md" ||
		(strings.HasPrefix(rel, "docs/") && strings.HasSuffix(rel, ".md"))
}

// docFiles returns the docs the guards read, with their text, skipping any deleted in the
// working tree.
func docFiles(t *testing.T) map[string]string {
	t.Helper()

	docs := map[string]string{}

	for _, rel := range repoFiles(t) {
		if !isDoc(rel) {
			continue
		}

		if text, ok := readText(t, rel); ok {
			docs[rel] = text
		}
	}

	return docs
}

// docLinks returns every link of a markdown text outside its fenced code blocks and its
// inline code.
func docLinks(text string) []docLinkAt {
	var links []docLinkAt

	var fence markdownFence

	for i, line := range strings.Split(text, "\n") {
		if fence.inside(line) {
			continue
		}

		line = codeSpan.ReplaceAllString(line, "``")

		for _, re := range []*regexp.Regexp{docLink, docHref} {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				links = append(links, docLinkAt{line: i + 1, target: m[1]})
			}
		}
	}

	return links
}

// headingAnchors returns the anchors GitHub gives a markdown text's headings, a repeated
// one with its -1, -2, ... suffix.
func headingAnchors(text string) map[string]bool {
	anchors := map[string]bool{}
	seen := map[string]int{}

	for _, heading := range markdownHeadings(text) {
		anchor := headingAnchor(heading)
		if n := seen[anchor]; n > 0 {
			anchors[fmt.Sprintf("%s-%d", anchor, n)] = true
		} else {
			anchors[anchor] = true
		}

		seen[anchor]++
	}

	return anchors
}

// repoTree is the repository's file list as sets of files and of the folders holding them.
type repoTree struct {
	files   map[string]bool
	folders map[string]bool
}

func newRepoTree(files []string) repoTree {
	tree := repoTree{files: map[string]bool{}, folders: map[string]bool{}}

	for _, rel := range files {
		tree.files[rel] = true

		for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
			tree.folders[dir] = true
		}
	}

	return tree
}

// linkPath resolves a link's path, relative to the doc from, to a repository path, or
// returns what is wrong with it.
func linkPath(from, rel string) (string, string) {
	switch {
	case rel == "":
		return from, ""
	case strings.HasPrefix(rel, "/"):
		return "", "is written from the repository's root; write it relative to the file"
	}

	rel = path.Clean(path.Join(path.Dir(from), rel))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", "leaves the repository"
	}

	return rel, ""
}

// linkedFile is the file a resolved link opens: the file itself, or a folder's README.md
// ("" for a folder with none), or what is wrong with it.
func linkedFile(rel string, tree repoTree) (string, string) {
	if tree.files[rel] {
		return rel, ""
	}

	if !tree.folders[rel] {
		return "", "names no file or folder"
	}

	readme := rel + "/README.md"

	switch {
	case tree.files[readme]:
		return readme, ""
	case strings.HasPrefix(rel, "docs/"):
		return "", "names a docs folder with no README.md"
	}

	return "", ""
}

// resolveDocLink checks one link of the doc from, returning what is wrong with it, or ""
// when it resolves. A link to a web page is not checked, and of a synced file under
// .github/, which every sync overwrites, only that it exists.
func resolveDocLink(from, target string, tree repoTree, docs map[string]string) string {
	if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return ""
	}

	rel, anchor, _ := strings.Cut(target, "#")

	rel, problem := linkPath(from, rel)
	if problem != "" {
		return problem
	}

	file, problem := linkedFile(rel, tree)
	if problem != "" || anchor == "" || strings.HasPrefix(file, ".github/") {
		return problem
	}

	if text, ok := docs[file]; ok && !headingAnchors(text)[anchor] {
		return fmt.Sprintf("names #%s, which no heading of %s has", anchor, file)
	}

	return ""
}

// markdownTexts returns the text of every markdown file outside .github/, the files a doc
// may link to an anchor of.
func markdownTexts(t *testing.T, tree repoTree) map[string]string {
	t.Helper()

	texts := map[string]string{}

	for rel := range tree.files {
		if !strings.HasSuffix(rel, ".md") || strings.HasPrefix(rel, ".github/") {
			continue
		}

		if text, ok := readText(t, rel); ok {
			texts[rel] = text
		}
	}

	return texts
}

// repeatedAnchors returns each anchor more than one heading of a markdown text has.
func repeatedAnchors(text string) []string {
	seen := map[string]int{}

	var repeated []string

	for _, heading := range markdownHeadings(text) {
		anchor := headingAnchor(heading)
		if seen[anchor]++; seen[anchor] == 2 {
			repeated = append(repeated, anchor)
		}
	}

	return repeated
}

// TestDocsLinksResolve checks every link of the docs (the project guide, the README, and
// everything under docs/): each relative link names a file, or a folder (a docs folder with
// its README.md), and each #anchor into a doc names one of its headings. It also checks that
// no two headings of a doc share an anchor, so every heading can be linked to.
func TestDocsLinksResolve(t *testing.T) {
	t.Parallel()

	tree := newRepoTree(repoFiles(t))
	texts := markdownTexts(t, tree)

	for rel, text := range docFiles(t) {
		for _, link := range docLinks(text) {
			problem := resolveDocLink(rel, link.target, tree, texts)
			assert.Empty(t, problem, "%s:%d: the link %s %s", rel, link.line, link.target, problem)
		}

		for _, anchor := range repeatedAnchors(text) {
			assert.Fail(t, "repeated heading", "%s has more than one heading with the anchor #%s: give each its own words", rel, anchor)
		}
	}
}

// docsMaxBytes caps each doc, so that a reader, a person or an agent, reads the one topic they
// came for rather than a book.
const docsMaxBytes = 48 << 10

// docsIndex is the one index of every doc.
const docsIndex = "docs/README.md"

// docsUnder returns the docs under docs/, with their text.
func docsUnder(t *testing.T) map[string]string {
	t.Helper()

	docs := map[string]string{}

	for rel, text := range docFiles(t) {
		if strings.HasPrefix(rel, "docs/") {
			docs[rel] = text
		}
	}

	return docs
}

// linkedDocs returns the repository paths a doc links to, a folder's link as its README.md.
func linkedDocs(from, text string, tree repoTree) map[string]bool {
	linked := map[string]bool{}

	for _, link := range docLinks(text) {
		rel, _, _ := strings.Cut(link.target, "#")
		if rel == "" || strings.Contains(rel, "://") {
			continue
		}

		target, problem := linkPath(from, rel)
		if problem != "" {
			continue
		}

		if tree.folders[target] {
			target += "/README.md"
		}

		linked[target] = true
	}

	return linked
}

// TestDocsAreIndexed checks that docs/README.md links to every doc, so it is the one place
// to start, and that a folder's README.md links to every topic in the folder.
func TestDocsAreIndexed(t *testing.T) {
	t.Parallel()

	tree := newRepoTree(repoFiles(t))
	docs := docsUnder(t)

	index, ok := docs[docsIndex]
	if !assert.True(t, ok, "%s is missing", docsIndex) {
		return
	}

	indexed := linkedDocs(docsIndex, index, tree)
	readmes := map[string]map[string]bool{}

	for rel := range docs {
		if rel == docsIndex {
			continue
		}

		assert.True(t, indexed[rel], "%s links to no %s: add it to its section", docsIndex, rel)

		dir := path.Dir(rel)
		readme := dir + "/README.md"

		if dir == "docs" || rel == readme || docs[readme] == "" {
			continue
		}

		if readmes[readme] == nil {
			readmes[readme] = linkedDocs(readme, docs[readme], tree)
		}

		assert.True(t, readmes[readme][rel], "%s links to no %s: add it to its In this folder list", readme, rel)
	}
}

// TestDocsOpenWithTheirNavLine checks that every doc under docs/ but the index opens with its
// title, then a nav line whose breadcrumb leads back to the index.
func TestDocsOpenWithTheirNavLine(t *testing.T) {
	t.Parallel()

	for rel, text := range docsUnder(t) {
		lines := strings.SplitN(text, "\n", 4)
		if !assert.GreaterOrEqual(t, len(lines), 3, "%s is too short for a title and a nav line", rel) {
			continue
		}

		assert.True(t, strings.HasPrefix(lines[0], "# "), "%s:1 is not the file's title (# …)", rel)

		if rel == docsIndex {
			continue
		}

		crumb := "[Docs](" + strings.Repeat("../", strings.Count(strings.TrimPrefix(rel, "docs/"), "/")) + "README.md)"

		assert.Empty(t, lines[1], "%s:2 is not the blank line under the title", rel)
		assert.True(t, strings.HasPrefix(lines[2], crumb),
			"%s:3 is not the nav line: it starts with %s, the breadcrumb back to %s", rel, crumb, docsIndex)
	}
}

// TestDocsStaySmall caps every doc under docs/ at docsMaxBytes.
func TestDocsStaySmall(t *testing.T) {
	t.Parallel()

	for rel, text := range docsUnder(t) {
		assert.LessOrEqual(t, len(text), docsMaxBytes,
			"%s is %d bytes, over %d: make it a folder, with a README.md for its own notes and one file "+
				"per topic, each listed in the README.md and in %s", rel, len(text), docsMaxBytes, docsIndex)
	}
}
