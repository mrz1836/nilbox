package bump_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/bump"
)

func TestCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		from, to string
		want     bump.Level
	}{
		{name: "major", from: "5.9.3", to: "6.0.3", want: bump.LevelMajor},
		{name: "minor", from: "0.30.0", to: "0.31.0", want: bump.LevelMinor},
		{name: "patch", from: "4.17.20", to: "4.17.21", want: bump.LevelPatch},
		{name: "a fourth part", from: "1.2.3.4", to: "1.2.3.5", want: bump.LevelPatch},
		{name: "one part each", from: "3", to: "4", want: bump.LevelMajor},
		{name: "a leading v on one side", from: "v1.2.0", to: "1.3.0", want: bump.LevelMinor},
		{name: "a capital V", from: "V1.2.0", to: "V2.0.0", want: bump.LevelMajor},
		{name: "a caret", from: "^1.2.0", to: "^1.2.5", want: bump.LevelPatch},
		{name: "a tilde", from: "~1.2.0", to: "~1.3.0", want: bump.LevelMinor},
		{name: "a pessimistic requirement", from: "~> 6.0", to: "~> 7.0", want: bump.LevelMajor},
		{name: "a lower bound", from: ">= 1.50, < 2.0", to: ">= 1.60, < 2.0", want: bump.LevelMinor},
		{name: "an equals sign", from: "=1.0.0", to: "= 1.0.1", want: bump.LevelPatch},
		{name: "surrounding space", from: " 1.0.0 ", to: "\t2.0.0", want: bump.LevelMajor},
		{name: "a downgrade", from: "2.0.0", to: "1.9.0", want: bump.LevelMajor},
		{name: "a minor downgrade", from: "1.3.0", to: "1.2.9", want: bump.LevelMinor},
		{name: "a prerelease", from: "1.2.3-beta", to: "1.2.3", want: bump.LevelPatch},
		{name: "a prerelease ends the run", from: "3-beta.1", to: "3-beta.2", want: bump.LevelPatch},
		{name: "a prerelease part then a major", from: "3-beta", to: "4", want: bump.LevelMajor},
		{name: "another part", from: "1.2", to: "1.2.0", want: bump.LevelPatch},
		{name: "a Docker tag's suffix", from: "18-alpine", to: "20-alpine", want: bump.LevelMajor},
		{name: "leading zeros", from: "2024.01.5", to: "2024.1.6", want: bump.LevelPatch},
		{name: "a long part", from: "1.123456789012345678901234567890", to: "1.123456789012345678901234567891", want: bump.LevelMinor},
		{name: "identical", from: "1.2.3", to: "1.2.3", want: bump.LevelUnknown},
		{name: "the same after the v", from: "v1.2.3", to: "1.2.3", want: bump.LevelUnknown},
		{name: "digests", from: "abc1234", to: "def5678", want: bump.LevelUnknown},
		{name: "digests that start with a digit", from: "1a2b3c4", to: "5D6E7F8", want: bump.LevelUnknown},
		{name: "a version to a digest", from: "1.2.3", to: "9f8e7d6c5b4a", want: bump.LevelUnknown},
		{name: "a short tag is no digest", from: "1a", to: "2a", want: bump.LevelMajor},
		{name: "a long number is no digest", from: "20240101", to: "20240102", want: bump.LevelMajor},
		{name: "a long tag is no digest", from: "18-alpine3.20", to: "20-alpine3.20", want: bump.LevelMajor},
		{name: "latest", from: "1.0.0", to: "latest", want: bump.LevelUnknown},
		{name: "empty", from: "", to: "1.0.0", want: bump.LevelUnknown},
		{name: "both empty", from: "", to: "", want: bump.LevelUnknown},
		{name: "a dot first", from: ".1", to: ".2", want: bump.LevelUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, bump.Compare(tt.from, tt.to))
		})
	}
}

func TestFromCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		msg    string
		want   bump.Level
		wantOK bool
	}{
		{
			name: "one patch",
			msg: "Bump lodash from 4.17.20 to 4.17.21\n\nBumps lodash.\n\n---\nupdated-dependencies:\n" +
				"- dependency-name: lodash\n  dependency-version: 4.17.21\n  dependency-type: direct:production\n" +
				"  update-type: version-update:semver-patch\n...\n\nSigned-off-by: dependabot[bot]\n",
			want: bump.LevelPatch, wantOK: true,
		},
		{
			name: "a group's patch and major",
			msg: "Bump the go-deps group with 2 updates\n\n---\nupdated-dependencies:\n" +
				"- dependency-name: golang.org/x/net\n  update-type: version-update:semver-patch\n" +
				"  dependency-group: go-deps\n" +
				"- dependency-name: github.com/acme-org/example-repo\n  update-type: version-update:semver-major\n" +
				"  dependency-group: go-deps\n...\n",
			want: bump.LevelMajor, wantOK: true,
		},
		{
			name: "minor over patch",
			msg: "  update-type: version-update:semver-patch\n  update-type: version-update:semver-minor\n" +
				"  update-type: version-update:semver-patch\n",
			want: bump.LevelMinor, wantOK: true,
		},
		{
			name: "a security update and Windows line endings",
			msg:  "---\r\nupdated-dependencies:\r\n- dependency-name: lodash\r\n  update-type: security-update:semver-minor\r\n",
			want: bump.LevelMinor, wantOK: true,
		},
		{
			name: "the key first in the list item",
			msg:  "- update-type: version-update:semver-major\n  dependency-name: react\n",
			want: bump.LevelMajor, wantOK: true,
		},
		{
			name: "any case",
			msg:  "  Update-Type: Version-Update:SemVer-Minor\n",
			want: bump.LevelMinor, wantOK: true,
		},
		{name: "no trailer", msg: "Fix the login bug\n\nIt was broken.", want: bump.LevelUnknown},
		{name: "empty", msg: "", want: bump.LevelUnknown},
		{name: "an unknown level", msg: "  update-type: version-update:semver-huge\n", want: bump.LevelUnknown},
		{name: "a longer word", msg: "  update-type: version-update:semver-majority\n", want: bump.LevelUnknown},
		{name: "not at a line's start", msg: "see update-type: version-update:semver-major\n", want: bump.LevelUnknown},
		{name: "no kind", msg: "  update-type: semver-major\n", want: bump.LevelUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := bump.FromCommit(tt.msg)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}
