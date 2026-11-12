package testutil

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplateIsAWholeDatabaseFile(t *testing.T) {
	t.Parallel()

	body, err := template()
	require.NoError(t, err)

	// The file's header: https://sqlite.org/fileformat.html#the_database_header
	require.Greater(t, len(body), 100, "the file holds at least a header")
	assert.Equal(t, "SQLite format 3\x00", string(body[:16]))
	assert.Equal(t, []byte{2, 2}, body[18:20], "the file is in write-ahead-logging mode, as the store leaves it")

	pageSize := int(binary.BigEndian.Uint16(body[16:18]))
	pages := int(binary.BigEndian.Uint32(body[28:32]))
	assert.Equal(t, len(body), pageSize*pages, "every page is in the file itself, none left in a write-ahead log")

	again, err := template()
	require.NoError(t, err)
	assert.Same(t, &body[0], &again[0], "the template is built once")
}
