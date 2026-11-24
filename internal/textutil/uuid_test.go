package textutil_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// v4 is a version 4 UUID with the RFC 9562 variant, in its canonical form.
var v4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewUUID(t *testing.T) {
	t.Parallel()

	a, b := textutil.NewUUID(), textutil.NewUUID()

	assert.Regexp(t, v4, a)
	assert.Regexp(t, v4, b)
	assert.NotEqual(t, a, b)
}

func TestCanonicalUUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, in, want string
	}{
		{name: "canonical", in: "6f9619ff-8b86-4d01-b42d-00cf4fc964ff", want: "6f9619ff-8b86-4d01-b42d-00cf4fc964ff"},
		{name: "capitals", in: "6F9619FF-8B86-4D01-B42D-00CF4FC964FF", want: "6f9619ff-8b86-4d01-b42d-00cf4fc964ff"},
		{name: "another version", in: "f81d4fae-7dec-11d0-a765-00a0c91e6bf6", want: "f81d4fae-7dec-11d0-a765-00a0c91e6bf6"},
		{name: "the Nil UUID", in: "00000000-0000-0000-0000-000000000000"},
		{name: "no dashes", in: "6f9619ff8b864d01b42d00cf4fc964ff"},
		{name: "braces", in: "{6f9619ff-8b86-4d01-b42d-00cf4fc964ff}"},
		{name: "a URN", in: "urn:uuid:6f9619ff-8b86-4d01-b42d-00cf4fc964ff"},
		{name: "space around it", in: " 6f9619ff-8b86-4d01-b42d-00cf4fc964f"},
		{name: "not hex", in: "6f9619ff-8b86-4d01-b42d-00cf4fc964fg"},
		{name: "dashes misplaced", in: "6f9619ff8-b86-4d01-b42d-00cf4fc964ff"},
		{name: "empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := textutil.CanonicalUUID(tt.in)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want != "", ok)
		})
	}

	minted := textutil.NewUUID()
	got, ok := textutil.CanonicalUUID(minted)
	assert.True(t, ok)
	assert.Equal(t, minted, got, "a minted UUID is canonical")
}
