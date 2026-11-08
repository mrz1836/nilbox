package sensitive

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffURL(t *testing.T) {
	t.Parallel()

	const prURL = "https://github.com/acme-org/example-repo/pull/7"

	assert.Equal(t,
		prURL+"/files#diff-b803fcb7f17ed9235f1e5cb1fcd2f5d3b2838429d4368ae4c57ce4436577f03f",
		diffURL(prURL+"/", ".github/workflows/ci.yml"))
	assert.Empty(t, diffURL("http://example.com/pull/7", "x"))
	assert.Empty(t, diffURL(prURL, ""))
}
