package launcher

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/textutil"
)

const (
	// ghBin is the command the GitHub CLI runs as.
	ghBin = "gh"

	// maxErrDetail caps how much of the GitHub CLI's error output a failed clone's error
	// carries.
	maxErrDetail = 200
)

var (
	// ErrCloneTargetExists is returned when the directory a clone would be created in
	// already exists. Clone never writes into an existing directory.
	ErrCloneTargetExists = errors.New("it already exists")

	// ErrCloneUnavailable is returned when the Service was built without a GH runner.
	ErrCloneUnavailable = errors.New("cloning is not available")
)

// CloneTarget is a repository's local clone, or where Clone would create one.
type CloneTarget struct {
	// Repo is the repository, "owner/name".
	Repo string `json:"repo"`

	// Cloned reports whether the repository has a local clone, which Dir then names.
	Cloned bool `json:"cloned"`

	// Dir is the clone, or for a repository without one, where Clone would create it:
	// <clone root>/<name>. It is shown with ~ for home.
	Dir string `json:"dir"`

	// Taken reports that a repository without a clone cannot get one at Dir, because
	// something is already there. Clone refuses it.
	Taken bool `json:"taken"`
}

// CloneTarget reports whether an item's repository has a local clone and, when it has
// none, where Clone would create one and whether that path is free. It finds the clone
// as a launch does, so the result can be cached, and writes nothing else.
func (s *Service) CloneTarget(ctx context.Context, itemID int64) (CloneTarget, error) {
	t, _, err := s.cloneTarget(ctx, itemID)

	return t, err
}

// Clone creates the missing local clone of an item's repository at <clone root>/<name>
// by running `gh repo clone owner/name <dir>` as an argv, never through a shell, and
// caches the new clone's path, so the next launch starts in it. A repository that
// already has a clone is returned as it is, without running anything. Clone refuses
// when anything already exists at the target: it creates clones but never writes into
// or changes an existing directory. Clones run one at a time, so two requests for the
// same repository clone it once.
func (s *Service) Clone(ctx context.Context, itemID int64) (CloneTarget, error) {
	if s.gh == nil {
		return CloneTarget{}, ErrCloneUnavailable
	}

	s.cloneMu.Lock()
	defer s.cloneMu.Unlock()

	t, target, err := s.cloneTarget(ctx, itemID)
	if err != nil || t.Cloned {
		return t, err
	}

	if t.Taken {
		return CloneTarget{}, fmt.Errorf("clone %s into %s: %w", t.Repo, t.Dir, ErrCloneTargetExists)
	}

	dir := target.localPath

	if _, stderr, runErr := s.gh.Run(ctx, ghBin, "repo", "clone", t.Repo, dir); runErr != nil {
		if line := textutil.Clip(execpath.GitMessage(stderr), maxErrDetail); line != "" && ctx.Err() == nil {
			return CloneTarget{}, fmt.Errorf("clone %s: %s: %w", t.Repo, line, runErr)
		}

		return CloneTarget{}, fmt.Errorf("clone %s: %w", t.Repo, runErr)
	}

	s.cacheClonePath(ctx, target.id, dir)

	t.Cloned, t.Taken = true, false

	return t, nil
}

// cloneTarget finds an item's clone, or works out where Clone would create one. For a
// repository without a clone, the returned repoRef's localPath is that absolute path. A
// stored owner or name that breaks GitHub's rules is a repo.ErrInvalid, so no name that
// could escape the clone root or read as a flag ever names a directory or a gh argument.
func (s *Service) cloneTarget(ctx context.Context, itemID int64) (CloneTarget, repoRef, error) {
	prefs, err := s.settings.Get(ctx)
	if err != nil {
		return CloneTarget{}, repoRef{}, err
	}

	row, err := s.loadItem(ctx, itemID)
	if err != nil {
		return CloneTarget{}, repoRef{}, err
	}

	ref := row.repoRef()
	if _, err = repo.New(ref.name.Owner, ref.name.Repo); err != nil {
		return CloneTarget{}, repoRef{}, err
	}

	t := CloneTarget{Repo: ref.name.String()}

	clone, err := s.resolveClone(ctx, ref, prefs.CloneRoot)
	if err == nil {
		t.Cloned, t.Dir = true, paths.Tilde(clone.Dir, s.home)

		return t, repoRef{id: ref.id, name: ref.name, localPath: clone.Dir}, nil
	}

	if !errors.Is(err, localrepos.ErrCloneNotFound) {
		return CloneTarget{}, repoRef{}, err
	}

	root, err := paths.ExpandHome(prefs.CloneRoot, s.home)
	if err != nil {
		return CloneTarget{}, repoRef{}, fmt.Errorf("clone root: %w", err)
	}

	dir := filepath.Join(root, ref.name.Repo)
	t.Dir = paths.Tilde(dir, s.home)

	if t.Taken, err = exists(dir); err != nil {
		return CloneTarget{}, repoRef{}, err
	}

	return t, repoRef{id: ref.id, name: ref.name, localPath: dir}, nil
}

// exists reports whether anything, even a dangling symlink, is at path.
func exists(path string) (bool, error) {
	_, err := os.Lstat(path)

	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("check %s: %w", path, err)
	}
}
