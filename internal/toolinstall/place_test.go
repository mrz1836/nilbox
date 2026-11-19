package toolinstall

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestInstallSaysWhereItCantWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(t *testing.T, s *setup, req *Request)
		want   error
		words  string
	}{
		{
			name: "a downloads folder under a file",
			change: func(t *testing.T, s *setup, _ *Request) {
				t.Helper()

				file := filepath.Join(t.TempDir(), "a-file")
				require.NoError(t, os.WriteFile(file, nil, 0o600))

				s.in.Downloads = filepath.Join(file, "downloads")
			},
			words: "make the downloads folder",
		},
		{
			name: "a downloads folder it can't write",
			change: func(t *testing.T, s *setup, _ *Request) {
				t.Helper()
				testutil.SkipAsRoot(t)

				require.NoError(t, os.MkdirAll(s.downloads, 0o700))
				require.NoError(t, os.Chmod(s.downloads, 0o500))       //nolint:gosec // the test's own folder, made read-only
				t.Cleanup(func() { _ = os.Chmod(s.downloads, 0o700) }) //nolint:gosec // put back for t.TempDir's removal
			},
			words: "make the download folder",
		},
		{
			name: "a target folder under a file",
			change: func(t *testing.T, _ *setup, req *Request) {
				t.Helper()

				file := filepath.Join(t.TempDir(), "a-file")
				require.NoError(t, os.WriteFile(file, nil, 0o600))

				req.Dir = filepath.Join(file, "bin")
			},
			want: ErrWrite,
		},
		{
			name: "a folder where the program goes",
			change: func(t *testing.T, s *setup, _ *Request) {
				t.Helper()

				require.NoError(t, os.MkdirAll(filepath.Join(s.dir, testBinary), 0o700))
			},
			want:  ErrWrite,
			words: "isn't a file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newSetup(t, goodArchive(t))
			req := s.request("")
			tt.change(t, s, &req)

			_, err := s.in.Install(t.Context(), req)
			require.Error(t, err)

			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
			}

			require.ErrorContains(t, err, tt.words)
			assert.NoFileExists(t, filepath.Join(s.dir, testBinary), "nothing is placed")

			if entries, readErr := os.ReadDir(s.in.Downloads); readErr == nil {
				assert.Empty(t, entries, "and the download folder is removed")
			}
		})
	}
}

func TestALeftoverThatCantBeClearedStopsThePlaceAndKeepsTheCopy(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{"." + testSlug + "-new", "." + testSlug + "-old"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()

			s := newSetup(t, goodArchive(t))
			target := filepath.Join(s.dir, testBinary)

			require.NoError(t, os.MkdirAll(s.dir, 0o750))
			require.NoError(t, os.WriteFile(target, []byte("the copy already here"), 0o755)) //nolint:gosec // a program

			// A folder, not empty, in the leftover's place: an install's own leftover is a
			// file, so it is no leftover to remove.
			require.NoError(t, os.MkdirAll(filepath.Join(target+suffix, "inside"), 0o700))

			_, err := s.in.Install(t.Context(), s.request(""))
			require.ErrorIs(t, err, ErrWrite)

			got, readErr := os.ReadFile(target) //nolint:gosec // the test's own temporary folder
			require.NoError(t, readErr)
			assert.Equal(t, "the copy already here", string(got), "the copy in place is unchanged")
			assert.DirExists(t, target+suffix, "what isn't the install's own is left")

			for _, other := range []string{"." + testSlug + "-new", "." + testSlug + "-old"} {
				if other != suffix {
					assert.NoFileExists(t, target+other, "nothing of the install is left beside it")
				}
			}
		})
	}
}
