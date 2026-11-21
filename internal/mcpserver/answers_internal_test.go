package mcpserver

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// errNewerSnapshot is an error that is neither a path's nor a link's, with a space before
// it, as one read from a process might have.
var errNewerSnapshot = errors.New("  the snapshot is from a newer build")

func TestReasonSaysWhatFailedWithoutWhere(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"a path's error": {
			err: fmt.Errorf("read the snapshot: %w",
				&fs.PathError{Op: "open", Path: "/srv/data/snapshot.json", Err: fs.ErrPermission}),
			want: "open: permission denied",
		},
		"a link's error": {
			err: fmt.Errorf("write the proposal: %w",
				&os.LinkError{Op: "rename", Old: "/srv/data/p.tmp", New: "/srv/data/p.json", Err: fs.ErrExist}),
			want: "rename: file already exists",
		},
		"any other error": {err: errNewerSnapshot, want: "the snapshot is from a newer build"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, reason(tc.err))
		})
	}
}
