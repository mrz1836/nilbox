package integrations

import (
	"os"
	"time"
)

// fileStat is what tells a later look at a file whether it changed: its size, its
// modification time, and its inode, which a replaced file doesn't keep.
type fileStat struct {
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
	Inode   uint64    `json:"inode"`
}

// statOf reads path's fileStat, following links, or the zero fileStat when it can't.
func statOf(path string) fileStat {
	info, err := os.Stat(path)
	if err != nil {
		return fileStat{}
	}

	return fileStat{Size: info.Size(), ModTime: info.ModTime().UTC(), Inode: inodeOf(info)}
}
