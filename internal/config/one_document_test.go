package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Parse decodes only the first YAML document. A second "---" section would be dropped without a
// word, so a file with one is refused.
func TestSecondYAMLDocumentIsRefused(t *testing.T) {
	docs := []string{
		"server:\n  addr: 127.0.0.1:0\n---\nlimits:\n  max_attempts: 9\n",
		"loops:\n  self_update:\n    max_defer: 1h\n---\nlimits:\n  max_attempts: 9\n",
		"server:\n  addr: 127.0.0.1:0\n---\n",
	}
	for _, doc := range docs {
		_, err := Parse([]byte(doc), "")
		require.Error(t, err, doc)
		assert.ErrorContains(t, err, "more than one YAML document", doc)
	}
}

// A single document, with or without its markers, still loads.
func TestSingleYAMLDocumentStillLoads(t *testing.T) {
	docs := []string{
		"---\nserver:\n  addr: 127.0.0.1:0\n",
		"server:\n  addr: 127.0.0.1:0\n...\n",
		"",
		"# only a comment\n",
	}
	for _, doc := range docs {
		_, err := Parse([]byte(doc), "")
		assert.NoError(t, err, doc)
	}
}
