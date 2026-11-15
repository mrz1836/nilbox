//go:build darwin

package localrepos

import "golang.org/x/sys/unix"

// mountPoints lists the folders disks are mounted on, from the kernel's mount table. It
// reads the table with MNT_NOWAIT, so a share that doesn't answer is never asked. It
// returns nil when the table can't be read.
func mountPoints() []string {
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil || n <= 0 {
		return nil
	}

	stats := make([]unix.Statfs_t, n)

	n, err = unix.Getfsstat(stats, unix.MNT_NOWAIT)
	if err != nil {
		return nil
	}

	out := make([]string, 0, n)

	for i := range stats[:min(n, len(stats))] {
		out = append(out, unix.ByteSliceToString(stats[i].Mntonname[:]))
	}

	return out
}
