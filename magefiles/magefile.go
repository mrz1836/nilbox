//go:build mage

// Package main holds this repository's custom MAGE-X targets.
//
// MAGE-X (magex) supplies the standard targets (build, test, lint, format,
// deps, release) driven by .mage.yaml, so nothing here re-implements them.
// Targets that MAGE-X does not provide are added to this package, one
// namespace per file:
//
//   - brand: generate the frontend theme tokens and app metadata from
//     internal/brand
//   - frontend: install, build, and gate the desktop frontend
//   - app: generate bindings, lint, run, and bundle the desktop app
//   - measure: record startup, refresh, launch, and size numbers
//
// The desktop shell itself builds only with the desktop build tag; these
// targets compile on every OS and guard macOS-only steps at run time.
package main
