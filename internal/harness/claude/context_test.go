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

// The result line of a real run carries modelUsage with the window the CLI runs the model at.
func TestResultReportsContextWindow(t *testing.T) {
	_, res := replay(t, "testdata/simple.jsonl")
	assert.Equal(t, int64(1_000_000), res.ContextWindow)
}

func TestContextWindowPicksSessionModel(t *testing.T) {
	p := &parser{}
	p.parseLine(`{"type":"system","subtype":"init","model":"claude-opus-5-5[1m]"}`)
	p.parseLine(`{"type":"result","is_error":false,"result":"ok","modelUsage":{` +
		`"claude-haiku-5-5":{"contextWindow":200000,"costUSD":0.5},` +
		`"claude-opus-5-5":{"contextWindow":1000000,"costUSD":0.1}}}`)
	assert.Equal(t, int64(1_000_000), p.result(0, nil).ContextWindow, "the init model, not the costliest")
}

func TestContextWindowUnknownWithoutModelUsage(t *testing.T) {
	p := &parser{}
	p.parseLine(`{"type":"result","is_error":false,"result":"ok","usage":{"input_tokens":1}}`)
	assert.Zero(t, p.result(0, nil).ContextWindow)
}

// Each assistant message with usage yields a log-less event carrying the prompt size, so a
// caller can follow the context during a turn; a sub-agent's message does not.
func TestAssistantUsageEmitsContextEvent(t *testing.T) {
	p := &parser{}
	evs := p.parseLine(`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":5,"cache_read_input_tokens":1000}}}`)
	if assert.Len(t, evs, 2) {
		assert.Equal(t, int64(1005), evs[0].ContextTokens)
		assert.Empty(t, evs[0].Kind)
		assert.Equal(t, "hi", evs[1].Text)
	}
	sub := p.parseLine(`{"type":"assistant","parent_tool_use_id":"t","message":{"content":[],"usage":{"input_tokens":9}}}`)
	assert.Empty(t, sub)
}
