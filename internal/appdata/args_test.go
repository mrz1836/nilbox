package appdata_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
)

func TestFromArgsReadsTheDataDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join("/", "tmp", "sandbox")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no arguments", args: nil, want: ""},
		{name: "other flags", args: []string{"--debug", "--measure-startup"}, want: ""},
		{name: "the flag", args: []string{appdata.DataDirFlag + "=" + dir}, want: dir},
		{name: "among others", args: []string{"--debug", appdata.DataDirFlag + "=" + dir, "--other"}, want: dir},
		{name: "cleaned", args: []string{appdata.DataDirFlag + "=" + dir + "/./x/.."}, want: dir},
		{name: "the first wins", args: []string{appdata.DataDirFlag + "=" + dir, appdata.DataDirFlag + "=/elsewhere"}, want: dir},
		{name: "a prefix of another flag", args: []string{appdata.DataDirFlag + "s=" + dir}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := appdata.FromArgs(tt.args)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFromArgsRefusesWhatIsNotAnAbsolutePath(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{appdata.DataDirFlag},
		{appdata.DataDirFlag + "="},
		{appdata.DataDirFlag + "=relative/data"},
		{appdata.DataDirFlag + "=~/data"},
	} {
		_, err := appdata.FromArgs(args)
		require.ErrorIs(t, err, appdata.ErrDataDirFlag, "%v", args)
	}
}
