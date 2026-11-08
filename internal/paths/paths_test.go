package paths_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
)

func TestExpandHome(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator)+"home", "octo-user")
	abs := filepath.Join(string(filepath.Separator)+"srv", "clones")

	tests := []struct {
		name    string
		path    string
		home    string
		want    string
		wantErr error
	}{
		{name: "tilde path", path: "~/projects", home: home, want: filepath.Join(home, "projects")},
		{name: "tilde path with trailing slash", path: " ~/projects/ ", home: home, want: filepath.Join(home, "projects")},
		{name: "tilde alone", path: "~", home: home, want: home},
		{name: "absolute", path: abs + string(filepath.Separator), home: home, want: abs},
		{name: "absolute without home", path: abs, want: abs},
		{name: "empty", path: "  ", home: home, wantErr: paths.ErrEmpty},
		{name: "tilde without home", path: "~/projects", wantErr: paths.ErrNoHome},
		{name: "relative", path: "projects", home: home, wantErr: paths.ErrRelative},
		{name: "other user's tilde", path: "~octo-user/projects", home: home, wantErr: paths.ErrRelative},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := paths.ExpandHome(tt.path, tt.home)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTilde(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator)+"home", "octo-user")
	other := filepath.Join(string(filepath.Separator)+"srv", "clones")

	tests := []struct {
		name string
		path string
		home string
		want string
	}{
		{name: "under home", path: filepath.Join(home, "projects", "example-repo"), home: home, want: "~/projects/example-repo"},
		{name: "home itself", path: home, home: home, want: "~"},
		{name: "outside home", path: other, home: home, want: other},
		{name: "sibling sharing a prefix", path: home + "-other", home: home, want: home + "-other"},
		{name: "home unknown", path: other, want: other},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, paths.Tilde(tt.path, tt.home))
		})
	}
}
