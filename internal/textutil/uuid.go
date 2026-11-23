package textutil

// This file is the one importer of the standard library's uuid package, and it imports
// nothing else: gci, which magex format:fix runs, does not yet know uuid is a standard
// package and would set it apart from the others, which gofumpt then refuses.
import "uuid"

// uuidLength is a UUID's length in its canonical form, hex and dashes.
const uuidLength = 36

// NewUUID mints a random (version 4) UUID in its canonical form: 36 lowercase characters,
// hex and dashes.
func NewUUID() string {
	return uuid.New().String()
}

// CanonicalUUID returns s as a UUID in its canonical form, 36 lowercase characters, and
// whether it is one: s must be a UUID written with its dashes, in either case, and not the
// Nil UUID, which names nothing. Anything else is "" and false, so a caller that keeps only
// a UUID it can trust drops it.
func CanonicalUUID(s string) (string, bool) {
	if len(s) != uuidLength {
		return "", false
	}

	u, err := uuid.Parse(s)
	if err != nil || u == uuid.Nil() {
		return "", false
	}

	return u.String(), true
}
