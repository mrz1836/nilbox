//go:build mage

package main

import (
	"github.com/magefile/mage/mg"

	"github.com/mrz1836/nilbox/internal/brandgen"
)

// Brand groups the targets that work with files generated from internal/brand.
type Brand mg.Namespace

// Generate writes the frontend theme tokens, the frontend brand constants, and the macOS
// Info.plist from internal/brand. It is idempotent: a second run changes nothing.
//
//	magex brand:generate
func (Brand) Generate() error {
	return brandgen.Write(".")
}
