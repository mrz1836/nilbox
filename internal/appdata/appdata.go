// Package appdata resolves where the application keeps its per-user data: the SQLite
// database, generated launch scripts, and logs.
//
// The location follows each operating system's convention. The resolver itself is a pure
// function of the OS name, the home directory, and the environment, so every branch is
// testable on any host even though only macOS is supported at run time today.
package appdata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mrz1836/nilbox/internal/brand"
)

// dirPerm is the permission for every directory the application creates: the data is
// private to the user.
const dirPerm = 0o700

var (
	// ErrUnsupportedOS is returned for an operating system with no known data location.
	ErrUnsupportedOS = errors.New("unsupported operating system")

	// ErrNoHomeDir is returned when the home directory is needed but empty.
	ErrNoHomeDir = errors.New("home directory is not set")

	// ErrNoAppDataEnv is returned on Windows when %AppData% is not set.
	ErrNoAppDataEnv = errors.New("APPDATA environment variable is not set")
)

// Paths are the application's data locations, all under one root directory.
type Paths struct {
	Root   string // the per-user data directory
	DB     string // the SQLite database file
	Launch string // generated launch scripts and prompt files
	Logs   string // log files
}

// NewPaths returns the data locations under root.
func NewPaths(root string) Paths {
	return Paths{
		Root:   root,
		DB:     filepath.Join(root, brand.Slug+".db"),
		Launch: filepath.Join(root, "launch"),
		Logs:   filepath.Join(root, "logs"),
	}
}

// Ensure creates every data directory that does not exist yet, readable only by the
// user. Existing directories are left as they are.
func (p Paths) Ensure() error {
	for _, dir := range []string{p.Root, p.Launch, p.Logs} {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
	}

	return nil
}

// Resolve returns the data locations for the current user on the current OS.
func Resolve() (Paths, error) {
	// A missing home directory is reported by Dir, and only on systems that need one.
	home, _ := os.UserHomeDir()

	root, err := Dir(runtime.GOOS, home, os.Getenv)
	if err != nil {
		return Paths{}, err
	}

	return NewPaths(root), nil
}

// Dir returns the per-user data directory for the given OS, home directory, and
// environment lookup:
//
//   - darwin: <home>/Library/Application Support/<DataDirName>
//   - linux: $XDG_DATA_HOME/<Slug>, else <home>/.local/share/<Slug>
//   - windows: %AppData%/<DataDirName>
//
// Any other OS returns ErrUnsupportedOS.
func Dir(goos, home string, getenv func(key string) string) (string, error) {
	switch goos {
	case "darwin":
		if home == "" {
			return "", ErrNoHomeDir
		}

		return filepath.Join(home, "Library", "Application Support", brand.DataDirName), nil
	case "linux":
		// The XDG spec says a relative value is invalid and must be ignored.
		if xdg := getenv("XDG_DATA_HOME"); xdg != "" && filepath.IsAbs(xdg) {
			return filepath.Join(xdg, brand.Slug), nil
		}

		if home == "" {
			return "", ErrNoHomeDir
		}

		return filepath.Join(home, ".local", "share", brand.Slug), nil
	case "windows":
		appData := getenv("APPDATA")
		if appData == "" {
			return "", ErrNoAppDataEnv
		}

		return filepath.Join(appData, brand.DataDirName), nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedOS, goos)
	}
}
