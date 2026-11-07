//go:build !darwin

package launcher

import "github.com/mrz1836/nilbox/internal/execpath"

// DefaultRegistry returns an empty registry: no terminal driver exists for this OS yet,
// so every launch fails with ErrUnsupportedOS. A port adds this OS's drivers here.
func DefaultRegistry(_ execpath.Runner) Registry {
	return NewRegistry()
}
