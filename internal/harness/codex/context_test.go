package codex

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextTokensIsLastTurnInput(t *testing.T) {
	p := newParser()
	p.parseLine(`{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":5}}`)
	p.parseLine(`{"type":"turn.completed","usage":{"input_tokens":150,"cached_input_tokens":90,"output_tokens":5}}`)
	res := p.result(0, nil)
	assert.Equal(t, int64(150), res.ContextTokens)
	assert.Equal(t, int64(250), res.InputTokens)
}
