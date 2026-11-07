//go:build mage

package main

import (
	"fmt"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	// sqlcCommand is sqlc's main package. The targets build it with `go run` at the pinned
	// version, outside this module, so sqlc never enters go.mod or go.sum.
	sqlcCommand = "github.com/sqlc-dev/sqlc/cmd/sqlc"

	// sqlcVersionKey names the pinned sqlc version in the project env file.
	sqlcVersionKey = "SQLC_VERSION"

	// projectEnvFile holds this project's own CI and tool settings, the sqlc pin among them.
	projectEnvFile = ".github/env/90-project.env"
)

// DB groups the targets for the typed queries sqlc generates from each table owner's SQL
// and the schema the migrations build, as sqlc.yaml lays out.
type DB mg.Namespace

// Generate regenerates every table owner's typed queries.
//
//	magex db:generate
func (DB) Generate() error {
	return runSQLC("generate")
}

// Check fails when the committed queries differ from what sqlc generates, or when a query
// does not prepare against the schema the migrations build. CI runs the same check.
//
//	magex db:check
func (DB) Check() error {
	if err := runSQLC("diff"); err != nil {
		return fmt.Errorf("%w; run magex db:generate", err)
	}

	if err := runSQLC("vet"); err != nil {
		return err
	}

	fmt.Println("✅ the generated queries are up to date and every query prepares") //nolint:forbidigo // mage target output

	return nil
}

// runSQLC runs one sqlc command, from the repository root, at the pinned version.
func runSQLC(command string) error {
	version, err := pinnedVersion(projectEnvFile, sqlcVersionKey)
	if err != nil {
		return err
	}

	if err = sh.RunV("go", "run", sqlcCommand+"@"+version, command); err != nil {
		return fmt.Errorf("sqlc %s: %w", command, err)
	}

	return nil
}
