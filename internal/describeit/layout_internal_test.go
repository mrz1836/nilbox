package describeit

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/brand"
)

// TestTheDescribeFolderKeepsItsLayout pins the describe folder's layout, which the tool
// mode and the plugin's installed copies name too: a change here is a change of protocol.
func TestTheDescribeFolderKeepsItsLayout(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := New(Config{Dir: dir})
	at := func(parts ...string) string { return filepath.Join(append([]string{dir}, parts...)...) }

	assert.Equal(t, at("context.json"), s.contextFile())
	assert.Equal(t, at("instructions.md"), s.instructionsFile())
	assert.Equal(t, at("work"), s.workDir())
	assert.Equal(t, at("bin", brand.Slug+"-mcp"), s.linkFile())
	assert.Equal(t, at("plugin"), s.pluginDir())
	assert.Equal(t, at("agent.json"), s.agentFile())
	assert.Equal(t, at("conversations", "c1"), s.conversationDir("c1"))
	assert.Equal(t, at("conversations", "c1", "conversation.json"), filepath.Join(s.conversationDir("c1"), conversationName))
	assert.Equal(t, at("proposals", "c1"), s.proposalsDir("c1"))
	assert.Equal(t, at("proposals", Outside, "outcomes.json"), filepath.Join(s.proposalsDir(Outside), outcomesName))
}
