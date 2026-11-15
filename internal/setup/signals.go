package setup

import (
	"context"
	"os"
	"path/filepath"
)

// Signals counts the user's clones that hold each sign the Tools step looks for, so it
// shows only when the tools it offers fit, and says why it is suggested.
type Signals struct {
	// Clones is how many clones were looked at.
	Clones int `json:"clones"`

	// GoModules counts the clones with a go.mod at their root.
	GoModules int `json:"goModules"`

	// MageX counts the clones set up for mage-x: its env file, its .mage.yaml, or a
	// magefiles folder.
	MageX int `json:"mageX"`

	// GoPreCommit counts the clones set up for go-pre-commit: its env file, or the older
	// .github/.env.base.
	GoPreCommit int `json:"goPreCommit"`

	// Fortress counts the clones whose CI is go-fortress's: its main workflow.
	Fortress int `json:"fortress"`
}

// The paths, inside a clone, of each sign Signals counts.
func goModuleMarkers() []string { return []string{"go.mod"} }

func mageXMarkers() []string {
	return []string{filepath.Join(".github", "env", "10-mage-x.env"), ".mage.yaml", "magefiles"}
}

func goPreCommitMarkers() []string {
	return []string{filepath.Join(".github", "env", "10-pre-commit.env"), filepath.Join(".github", ".env.base")}
}

func fortressMarkers() []string {
	return []string{filepath.Join(".github", "workflows", "fortress.yml"), filepath.Join(".github", "workflows", "fortress.yaml")}
}

// Signals counts the user's clones, in every repository location, that hold each sign the
// Tools step looks for. It checks a few fixed paths in each clone with lstat alone: no git,
// no file read, no folder listed, so it is quick and touches nothing a clone controls. A
// location that doesn't answer in time is skipped, as every listing skips it.
func (s *Service) Signals(ctx context.Context) (Signals, error) {
	if s.clones == nil {
		return Signals{}, nil
	}

	dirs, err := s.clones.Clones(ctx)
	if err != nil {
		return Signals{}, err
	}

	var out Signals

	for _, dir := range dirs {
		if ctx.Err() != nil {
			return Signals{}, ctx.Err()
		}

		out.Clones++
		out.GoModules += count(dir, goModuleMarkers())
		out.MageX += count(dir, mageXMarkers())
		out.GoPreCommit += count(dir, goPreCommitMarkers())
		out.Fortress += count(dir, fortressMarkers())
	}

	return out, nil
}

// count is 1 when the clone at dir holds any of markers, else 0.
func count(dir string, markers []string) int {
	for _, m := range markers {
		if _, err := os.Lstat(filepath.Join(dir, m)); err == nil {
			return 1
		}
	}

	return 0
}
