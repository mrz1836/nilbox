package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/repo"
)

// releaseAssetsMax is the most files a latest-release query reads of each release. Both
// tools publish seven (six archives and the checksums).
const releaseAssetsMax = 30

// LatestRelease is a repository's latest release as GitHub reports it: the newest one
// that is neither a draft nor a prerelease.
type LatestRelease struct {
	// Tag is the release's tag, as in v1.29.5.
	Tag string

	// URL is the release's page on GitHub.
	URL string

	// PublishedAt is when the release was published.
	PublishedAt time.Time

	// Assets are the release's files.
	Assets []ReleaseAsset
}

// ReleaseAsset is one file of a release.
type ReleaseAsset struct {
	// Name is the file's name, as in mage-x_1.29.5_darwin_arm64.tar.gz.
	Name string

	// URL is where it downloads from.
	URL string

	// Size is its size in bytes.
	Size int64

	// Digest is GitHub's digest of it, as in sha256:<hex>, or "" when GitHub has none.
	Digest string
}

// FetchLatestReleases reads the latest release of each repository in one query, keyed by
// each repository's Key. A repository with no release, or one GitHub can't find, is left
// out; any other error fails the whole call. The query's cost (one point) is counted like
// every other's.
func (c *Client) FetchLatestReleases(ctx context.Context, tok Token, repos []repo.Name) (map[string]LatestRelease, error) {
	if len(repos) == 0 {
		return map[string]LatestRelease{}, nil
	}

	query, err := latestReleasesQuery(repos)
	if err != nil {
		return nil, err
	}

	resp, _, err := postGraphQL[releasesData](ctx, c, tok, "latest releases", graphQLRequest{Query: query}, unlessNotFound)
	if err != nil {
		return nil, Redact(err, tok)
	}

	out := make(map[string]LatestRelease, len(repos))

	for i, r := range repos {
		if release, ok := (*resp.Data)[releasesAlias(i)].release(); ok {
			out[r.Key()] = release
		}
	}

	return out, nil
}

// latestReleasesQuery asks for each repository's latest release under its own alias. The
// names are written as JSON strings, which GraphQL reads the same way, so no name can
// break out of its argument.
func latestReleasesQuery(repos []repo.Name) (string, error) {
	var b strings.Builder

	b.WriteString("query LatestReleases {\n")

	for i, r := range repos {
		owner, err := json.Marshal(r.Owner)
		if err != nil {
			return "", fmt.Errorf("encode the owner: %w", err)
		}

		name, err := json.Marshal(r.Repo)
		if err != nil {
			return "", fmt.Errorf("encode the name: %w", err)
		}

		fmt.Fprintf(&b, "  %s: repository(owner: %s, name: %s) { latestRelease { tagName url publishedAt "+
			"releaseAssets(first: %d) { nodes { name downloadUrl size digest } } } }\n",
			releasesAlias(i), owner, name, releaseAssetsMax)
	}

	// The query's own cost, so the API budget counts it.
	b.WriteString("  rateLimit { cost }\n}\n")

	return b.String(), nil
}

// releasesAlias is the alias of the i-th repository in a latest-release query.
func releasesAlias(i int) string {
	return "t" + strconv.Itoa(i)
}

// releasesData is the latest-release query's data object: one aliased repository each,
// null when GitHub could not find it.
type releasesData map[string]*releasesRepo

// releasesRepo is one aliased repository and its latest release, null when it has none.
type releasesRepo struct {
	LatestRelease *struct {
		TagName       string    `json:"tagName"`
		URL           string    `json:"url"`
		PublishedAt   time.Time `json:"publishedAt"`
		ReleaseAssets struct {
			Nodes []struct {
				Name        string `json:"name"`
				DownloadURL string `json:"downloadUrl"`
				Size        int64  `json:"size"`
				Digest      string `json:"digest"`
			} `json:"nodes"`
		} `json:"releaseAssets"`
	} `json:"latestRelease"`
}

// release converts one repository's answer; ok is false when it has no release.
func (r *releasesRepo) release() (LatestRelease, bool) {
	if r == nil || r.LatestRelease == nil || r.LatestRelease.TagName == "" {
		return LatestRelease{}, false
	}

	lr := r.LatestRelease
	out := LatestRelease{
		Tag: lr.TagName, URL: lr.URL, PublishedAt: lr.PublishedAt,
		Assets: make([]ReleaseAsset, 0, len(lr.ReleaseAssets.Nodes)),
	}

	for _, n := range lr.ReleaseAssets.Nodes {
		out.Assets = append(out.Assets, ReleaseAsset{Name: n.Name, URL: n.DownloadURL, Size: n.Size, Digest: n.Digest})
	}

	return out, true
}
