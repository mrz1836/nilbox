package mcpserver_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/mcpserver"
)

func TestTheToolModesArgumentsNameItsVersionAndItsConversation(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "--mcp-server-v1=/data/describe", mcpserver.Arg("/data/describe"))
	assert.Equal(t, "--conversation=00000000-0000-4000-8000-000000000001",
		mcpserver.ConversationArg("00000000-0000-4000-8000-000000000001"))
}

func TestAnyVersionOfTheToolModeIsACall(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{mcpserver.Arg("/data/describe")},
		{mcpserver.Arg("/data/describe"), mcpserver.ConversationArg("c1")},
		{"--mcp-server-v2=/data/describe"},
		{"--data-dir=/data", "--mcp-server"},
	} {
		assert.True(t, mcpserver.IsCall(args), "%q", args)
	}

	for _, args := range [][]string{nil, {}, {"--data-dir=/data"}, {"--conversation=c1"}, {"--fence-hook-v1=/run"}} {
		assert.False(t, mcpserver.IsCall(args), "%q", args)
	}
}
