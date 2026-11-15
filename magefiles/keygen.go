//go:build mage

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/update"
)

// signingKeyFile is the name app:keygen gives the private key it writes.
const signingKeyFile = "release.key"

var (
	// errInRepository is returned for a key folder inside the repository.
	errInRepository = errors.New("keep the private key outside the repository")

	// errNoKeyFolder is returned when app:keygen is given no folder.
	errNoKeyFolder = errors.New("name a folder outside the repository: magex app:keygen out=<folder>")
)

// Keygen makes the release key, the Ed25519 key pair whose private half signs every
// release's update manifest: it writes the private key to <out>/release.key (PKCS#8 PEM,
// readable by you alone) and prints the public key, base64, to pin as update.ReleaseKey.
// The folder may start with ~/. It refuses a folder inside the repository, however the
// path reaches it, and never overwrites a key.
//
//	magex app:keygen out=<a folder outside the repository>
func (App) Keygen() error {
	repo, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("find the repository: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find the home folder: %w", err)
	}

	return keygen(mageArg("out"), repo, home)
}

// keygen makes the release key in out, which must not be inside repo, as App.Keygen
// describes. A relative out is read from repo, the working folder, and ~/ from home.
func keygen(out, repo, home string) error {
	dir, err := keyFolder(out, repo, home)
	if err != nil {
		return err
	}

	in, err := insideFolder(dir, repo)
	if err != nil {
		return err
	}

	if in {
		return fmt.Errorf("%w: %s is inside %s", errInRepository, dir, repo)
	}

	if err = os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate the release key: %w", err)
	}

	body, err := update.EncodePrivateKey(private)
	if err != nil {
		return err
	}

	path := filepath.Join(dir, signingKeyFile)
	if err = writeNewFile(path, body, 0o600); err != nil {
		return err
	}

	say("Wrote the release's private key to " + path + ". Keep it secret and back it up: without it, no install " +
		"can verify an update again.")
	say("The public key, base64, to pin as update.ReleaseKey:")
	say(update.EncodeKey(public))

	return nil
}

// keyFolder returns the absolute, clean folder out names: ~/ expanded from home, and a
// relative path read from cwd.
func keyFolder(out, cwd, home string) (string, error) {
	if out == "" {
		return "", errNoKeyFolder
	}

	if out != "~" && !strings.HasPrefix(out, "~/") && !filepath.IsAbs(out) {
		out = filepath.Join(cwd, out)
	}

	dir, err := paths.ExpandHome(out, home)
	if err != nil {
		return "", fmt.Errorf("read the folder %s: %w", out, err)
	}

	return dir, nil
}

// insideFolder reports whether dir is root or inside it. It compares folders as files
// (os.SameFile), never as text, so a path that differs from root's only in case, or
// reaches it through a link or a firmlink, is caught: from the longest part of dir that
// exists, its links resolved, it asks each folder up to the file system's root whether it
// is root.
func insideFolder(dir, root string) (bool, error) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", root, err)
	}

	for folder := paths.Resolved(dir); ; folder = filepath.Dir(folder) {
		if info, statErr := os.Stat(folder); statErr == nil && os.SameFile(info, rootInfo) {
			return true, nil
		}

		if filepath.Dir(folder) == folder {
			return false, nil
		}
	}
}

// writeNewFile writes body to a new file at path with mode perm, refusing a file that is
// already there.
func writeNewFile(path string, body []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // a folder app:keygen was named
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	if _, err = f.Write(body); err != nil {
		_ = f.Close()

		return fmt.Errorf("write %s: %w", path, err)
	}

	if err = f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
