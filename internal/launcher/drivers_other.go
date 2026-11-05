//go:build !darwin

package launcher

// DefaultRegistry returns an empty registry: no terminal driver exists for this OS yet,
// so every launch fails with ErrUnsupportedOS. A port adds this OS's drivers here.
func DefaultRegistry(_ Executor) Registry {
	return NewRegistry()
}
