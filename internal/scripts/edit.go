package scripts

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// originParallel is how many clones' origins RepoNames reads at a time.
const originParallel = 4

// Save saves one of the user's own scripts as the editor filled it in: a new one, at the
// end of the order, when its ID is 0, else in place of the one with its ID. Its name and
// command are trimmed and must be there, on one line; its scope, place, and icon must be
// ones there are; and a script for chosen repositories needs one, each read as owner/name
// and kept once, in any case. It returns the script as saved.
func (s *Service) Save(ctx context.Context, script UserScript) (UserScript, error) {
	u, err := checked(script)
	if err != nil {
		return UserScript{}, err
	}

	if u.ID == 0 {
		u.ID, err = s.yours.create(ctx, u)
	} else {
		err = s.yours.update(ctx, u)
	}

	if err != nil {
		return UserScript{}, err
	}

	s.logger.InfoContext(ctx, "a script of the user's was saved", "script", u.ID, "scope", string(u.Scope))

	return u, nil
}

// Delete deletes the user's script id. The runs it made stay in the history.
func (s *Service) Delete(ctx context.Context, id int64) error {
	if err := s.yours.remove(ctx, id); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "a script of the user's was deleted", "script", id)

	return nil
}

// Duplicate saves a copy of the user's script id, called "<name> copy", right after it in
// the order, and returns the copy.
func (s *Service) Duplicate(ctx context.Context, id int64) (UserScript, error) {
	made, err := s.yours.duplicate(ctx, id)
	if err != nil {
		return UserScript{}, err
	}

	s.logger.InfoContext(ctx, "a script of the user's was duplicated", "script", id, "copy", made.ID)

	return made, nil
}

// Move moves the user's script id one place up or down their order, as the clone at dir,
// with ~, lists them: past the nearest script that way that shows there, so the move
// shows on the page it was made on, or, with dir "", past its neighbor. A script already
// first or last there stays where it is.
func (s *Service) Move(ctx context.Context, id int64, way Direction, dir string) error {
	if way != MoveUp && way != MoveDown {
		return fmt.Errorf("%w: the direction %q", ErrInvalidRequest, way)
	}

	shown := func(UserScript) bool { return true }

	if dir != "" {
		c, err := s.cloneAt(ctx, dir)
		if err != nil {
			return err
		}

		goMod := goModule(c.dir)
		shown = func(u UserScript) bool { return u.includes(c.name, goMod) }
	}

	return s.yours.move(ctx, id, way, shown)
}

// RepoNames returns the GitHub repositories of the clones in the repository locations,
// owner/name, each once, sorted: what a script for chosen repositories can choose from.
// Each clone's origin is read with git, a few at a time, and kept until the clone's
// .git/config changes, so the editor opens at once after the first time; a clone the
// listing no longer holds is forgotten. Once the caller has gone (ctx is done, as when the
// window cancels the call), no more origins are read.
func (s *Service) RepoNames(ctx context.Context) ([]string, error) {
	l, err := s.folders().List(ctx)
	if err != nil {
		return nil, err
	}

	names := make([]repo.Name, len(l.Dirs))
	parallel(ctx, l.Dirs, func(i int, dir string) { names[i] = s.originOf(ctx, dir) })

	if err = ctx.Err(); err != nil {
		return nil, fmt.Errorf("list the clones' repositories: %w", err)
	}

	s.forgetOrigins(l.Dirs)

	seen := map[string]bool{}
	out := []string{}

	for _, n := range names {
		if n.Valid() && !seen[n.Key()] {
			seen[n.Key()] = true

			out = append(out, n.String())
		}
	}

	slices.SortFunc(out, byFold)

	return out, nil
}

// keptOrigin is a clone's origin as RepoNames last read it, and how its .git/config
// looked then.
type keptOrigin struct {
	look string
	name repo.Name
}

// originOf returns the GitHub repository the clone at dir's origin is, or the zero Name:
// as last read while its .git/config looks the same, else read again. One that can't be
// read is the zero Name, and read again next time.
func (s *Service) originOf(ctx context.Context, dir string) repo.Name {
	look := configLook(dir)

	if look != "" {
		s.mu.Lock()
		kept, ok := s.origins[dir]
		s.mu.Unlock()

		if ok && kept.look == look {
			return kept.name
		}
	}

	name, onHub, err := s.clones.Origin(ctx, dir)
	if err != nil {
		return repo.Name{}
	}

	if !onHub {
		name = repo.Name{}
	}

	if look != "" {
		s.mu.Lock()
		s.origins[dir] = keptOrigin{look: look, name: name}
		s.mu.Unlock()
	}

	return name
}

// forgetOrigins drops the origins kept for clones that aren't among dirs, the clones the
// latest listing holds.
func (s *Service) forgetOrigins(dirs []string) {
	listed := map[string]bool{}
	for _, d := range dirs {
		listed[d] = true
	}

	s.mu.Lock()
	maps.DeleteFunc(s.origins, func(dir string, _ keptOrigin) bool { return !listed[dir] })
	s.mu.Unlock()
}

// configLook is what the clone at dir's .git/config looks like on disk, its size and
// modification time, or "" when it isn't there, as in a clone whose .git is a file.
func configLook(dir string) string {
	info, err := os.Stat(filepath.Join(dir, ".git", "config"))
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}

	return strconv.FormatInt(info.Size(), 10) + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

// parallel calls fn for each folder of dirs, a few at a time, and returns once every call
// it made has returned. Once ctx is done it starts no more.
func parallel(ctx context.Context, dirs []string, fn func(i int, dir string)) {
	var wg sync.WaitGroup

	slots := make(chan struct{}, originParallel)

	for i, dir := range dirs {
		if ctx.Err() != nil {
			break
		}

		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()

			return
		}

		wg.Go(func() {
			defer func() { <-slots }()

			fn(i, dir)
		})
	}

	wg.Wait()
}

// yoursIn returns the user's scripts that show in the clone c, worked out, and whether the
// user has any at all.
func (s *Service) yoursIn(ctx context.Context, c clone) (out []script, found bool, err error) {
	all, err := s.yours.all(ctx)
	if err != nil {
		return nil, false, uierr.New("Couldn't read your scripts.", err)
	}

	goMod := goModule(c.dir)

	for _, u := range all {
		if u.includes(c.name, goMod) {
			out = append(out, yoursScript(u))
		}
	}

	return out, len(all) > 0, nil
}

// yoursMissing says why the user's script called id, as a ref names it, isn't among the
// clone c's: it was deleted, or doesn't show there.
func (s *Service) yoursMissing(ctx context.Context, c clone, id string) error {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: the script %q", ErrInvalidRequest, ref(SourceYours, id))
	}

	u, ok, err := s.yours.get(ctx, n)

	switch {
	case err != nil:
		return err
	case !ok:
		return goneError(n)
	}

	return uierr.New(fmt.Sprintf("%q doesn't show in %s. Edit it to choose the repositories it shows in.", u.Name,
		c.display()), fmt.Errorf("%w: %s in %s", ErrNotFound, ref(SourceYours, id), c.display()))
}
