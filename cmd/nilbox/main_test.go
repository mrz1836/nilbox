//go:build !desktop

package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
)

var errWrite = errors.New("write failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

func TestRunPrintsIdentityAndBuildMetadata(t *testing.T) {
	var buf bytes.Buffer

	require.NoError(t, run(&buf))

	out := buf.String()
	assert.Contains(t, out, brand.Name)
	assert.Contains(t, out, brand.Tagline)
	assert.Contains(t, out, version)
	assert.Contains(t, out, commit)
	assert.Contains(t, out, buildDate)
}

func TestRunReturnsWriteError(t *testing.T) {
	err := run(failingWriter{})

	require.ErrorIs(t, err, errWrite)
}

func TestExitStatus(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	assert.Zero(t, exitStatus(nil, &stderr))
	assert.Empty(t, stderr.String(), "success prints nothing")

	assert.Equal(t, 1, exitStatus(errWrite, &stderr))
	assert.Equal(t, errWrite.Error()+"\n", stderr.String())
}
