package textutil

// This file is the one importer of the standard library's uuid package, and it imports
// nothing else: gci, which magex format:fix runs, does not yet know uuid is a standard
// package and would set it apart from the others, which gofumpt then refuses.
import "uuid"

// NewUUID mints a random (version 4) UUID in its canonical form: 36 lowercase characters,
// hex and dashes.
func NewUUID() string {
	return uuid.New().String()
}
