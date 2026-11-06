package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The result line sums usage over every request of the turn (tool_use.jsonl: two requests that
// each re-read the cached prompt), so the context size has to come from the last assistant message.
func TestResultReportsLastRequestContext(t *testing.T) {
	_, res := replay(t, "testdata/tool_use.jsonl")
	assert.Equal(t, int64(2+108+58569), res.ContextTokens, "the second request's prompt, not the sum")
	assert.Greater(t, res.InputTokens, res.ContextTokens, "InputTokens sums both requests")
}

func TestContextIgnoresSubAgentMessages(t *testing.T) {
	p := &parser{}
	p.parseLine(`{"type":"assistant","message":{"content":[],"usage":{"input_tokens":5,"cache_read_input_tokens":1000}}}`)
	p.parseLine(`{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"content":[],"usage":{"input_tokens":9,"cache_read_input_tokens":90000}}}`)
	p.parseLine(`{"type":"result","is_error":false,"result":"ok","usage":{"input_tokens":14,"cache_read_input_tokens":91000}}`)
	assert.Equal(t, int64(1005), p.result(0, nil).ContextTokens)
}

func TestContextUnknownWithoutAssistantUsage(t *testing.T) {
	p := &parser{}
	p.parseLine(`{"type":"result","is_error":false,"result":"ok","usage":{"input_tokens":1}}`)
	assert.Zero(t, p.result(0, nil).ContextTokens)
}
