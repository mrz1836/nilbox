package release

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
)

// MergeSetting reads whether merging a pull request in the review sheet offers a
// release: the integrations.offerAfterMerge setting. Core adapts the settings.
type MergeSetting interface {
	OfferAfterMerge(ctx context.Context) (bool, error)
}

// DefaultBranches reads the default branch the app last stored for a repository, as the
// inbox read it from GitHub, without asking GitHub again: "" when it has none. Core adapts
// the inbox's store.
type DefaultBranches interface {
	DefaultBranch(ctx context.Context, name repo.Name) (string, error)
}

// AfterMerge says whether the toast that reports a merge in the review sheet offers to
// release the repository (§3.4.8). When Offer is set, its action opens Scripts → Release
// and the repository's sheet, with Repo, Dir, and Branch as the sheet's PrepareRequest.
type AfterMerge struct {
	// Offer reports that the toast carries Release <repo>….
	Offer bool `json:"offer"`

	// Repo is the repository, "owner/name", Dir its clone, with ~, and Branch the branch
	// the pull request merged into, which the sheet releases. Each is "" without Offer.
	Repo   string `json:"repo"`
	Dir    string `json:"dir"`
	Branch string `json:"branch"`
}

// OfferAfterMerge answers whether the toast that reports a merge in the review sheet
// offers to release owner/name, whose pull request merged into base (§5.11.9). It offers
// when the integrations.offerAfterMerge setting is on, magex is usable, a clone in the
// repository locations has the repository as its GitHub origin, and base is a branch Release offers
// (D10): the repository's default branch, or main or master, which origin has, since the
// merge went into it. It reads only local state and what the app already knows, and asks
// GitHub nothing: the default branch comes from the Release page's last list, or else
// from what the inbox stored, and the clone from the page's list, or else from the folders
// named as the repository is, never a scan of every clone. It fails only for a repository
// that isn't one, or for local state it can't read; anything missing is no offer.
func (s *Service) OfferAfterMerge(ctx context.Context, owner, name, base string) (AfterMerge, error) {
	full, err := repo.New(strings.TrimSpace(owner), strings.TrimSpace(name))
	if err != nil {
		return AfterMerge{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}

	base = strings.TrimSpace(base)
	if base == "" {
		return AfterMerge{}, nil
	}

	on, err := s.mergeOffers(ctx)
	if err != nil || !on {
		return AfterMerge{}, err
	}

	offered, err := s.offersBranch(ctx, full, base)
	if err != nil || !offered {
		return AfterMerge{}, err
	}

	dir, err := s.mergeClone(ctx, full)
	if err != nil || dir == "" {
		return AfterMerge{}, err
	}

	return AfterMerge{Offer: true, Repo: full.String(), Dir: paths.Tilde(dir, s.home), Branch: base}, nil
}

// mergeClone finds the clone of name for a merge's offer, or "" when it finds none,
// reading as little as it can, since a merge asks on every merge: the Release page's
// clones, when it listed the repository locations as set now, and else only the folders
// directly inside the clone root and each folder location named as the repository is,
// ignoring case, then each clone location whatever its name, each one's origin read with
// one git command. It never scans a folder, so a clone in a folder named otherwise is
// offered once the Release page has listed it.
func (s *Service) mergeClone(ctx context.Context, name repo.Name) (string, error) {
	roots, err := s.clones.Roots(ctx)
	if err != nil {
		return "", fmt.Errorf("read the repository locations: %w", err)
	}

	isName := func(_ string, origin repo.Name) bool { return origin.Key() == name.Key() }
	if dir, ok := s.listedClone(roots.Key(), isName); ok {
		return dir, nil
	}

	if s.origins == nil {
		return "", nil
	}

	return s.namedClone(ctx, roots, name)
}

// namedClone finds the clone of name, reading each candidate's origin, or returns "": the
// folders directly inside the clone root named as the repository is, ignoring case, then
// those inside each folder location, then each clone location whatever its name. A clone
// root that can't be read is an error; another location that can't be read is passed over.
func (s *Service) namedClone(ctx context.Context, roots localrepos.Roots, name repo.Name) (string, error) {
	candidates, err := namedCandidates(roots, name.Repo)
	if err != nil {
		return "", err
	}

	for _, dir := range candidates {
		if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
			continue
		}

		if origin, onHub, originErr := s.origins.Origin(ctx, dir); originErr == nil && onHub && origin.Key() == name.Key() {
			return dir, nil
		}
	}

	return "", nil
}

// namedCandidates lists the folders namedClone reads, in order: those directly inside the
// clone root and each folder location named repoName, ignoring case, then each clone
// location. Only a clone root that can't be read is an error.
func namedCandidates(roots localrepos.Roots, repoName string) ([]string, error) {
	var named, clones []string

	for _, place := range roots.Places() {
		if place.Kind == localrepos.KindClone {
			clones = append(clones, place.Path)

			continue
		}

		found, err := namedIn(place.Path, repoName)
		if err != nil && place.Primary {
			return nil, fmt.Errorf("read the clone root: %w", err)
		}

		named = append(named, found...)
	}

	return append(named, clones...), nil
}

// namedIn lists the entries directly inside folder named repoName, ignoring case.
func namedIn(folder, repoName string) ([]string, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}

	var out []string

	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), repoName) {
			out = append(out, filepath.Join(folder, entry.Name()))
		}
	}

	return out, nil
}

// mergeOffers reports whether a merge offers a release at all now: the setting is on, and
// magex is usable.
func (s *Service) mergeOffers(ctx context.Context) (bool, error) {
	if s.afterMerge == nil {
		return false, nil
	}

	on, err := s.afterMerge.OfferAfterMerge(ctx)
	if err != nil {
		return false, fmt.Errorf("read whether a merge offers a release: %w", err)
	}

	return on && s.tools != nil && s.tools.ReleaseTool(ctx).Usable(), nil
}

// offersBranch reports whether Release offers base for name (D10): main and master
// always, since a merge into one put it on origin, and any other branch only when it is
// the default branch, as the Release page last listed it or else as the inbox stored it.
func (s *Service) offersBranch(ctx context.Context, name repo.Name, base string) (bool, error) {
	if slices.Contains(otherBranches(), base) {
		return true, nil
	}

	def := s.listedDefault(name)

	if def == "" && s.branches != nil {
		got, err := s.branches.DefaultBranch(ctx, name)
		if err != nil {
			return false, fmt.Errorf("read the default branch of %s: %w", name, err)
		}

		def = got
	}

	return def != "" && def == base, nil
}

// listedDefault is the default branch of name as the Release page last listed it, or "".
func (s *Service) listedDefault(name repo.Name) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.has {
		return ""
	}

	for _, c := range s.last.Repos {
		if c.origin.Key() == name.Key() && c.DefaultBranch != "" {
			return c.DefaultBranch
		}
	}

	return ""
}
