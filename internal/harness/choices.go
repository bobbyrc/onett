package harness

import "sync"

// Choice is one value the owner can pick for a model, effort or permission mode.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
	// Hint is a short explanation shown next to the label.
	Hint string `json:"hint,omitempty"`
	// Dangerous marks a choice that skips safety checks; the UI asks for confirmation.
	Dangerous bool `json:"dangerous,omitempty"`
}

// Choices are the values a harness type accepts for the per-conversation settings. Models lists
// only aliases the adapter knows; the engine adds the models named in the agents config.
type Choices struct {
	Models          []Choice `json:"models"`
	Efforts         []Choice `json:"efforts"`
	PermissionModes []Choice `json:"permission_modes"`
}

var (
	choicesMu sync.RWMutex
	choices   = map[string]Choices{}
)

// RegisterChoices records the settings choices of a harness type. Adapters call it from init, so
// each adapter's list is built in one place.
func RegisterChoices(typ string, c Choices) {
	choicesMu.Lock()
	defer choicesMu.Unlock()
	choices[typ] = c
}

// ChoicesFor returns the choices of a harness type; the zero value, with no choices at all, for a
// type that registered none (the exec harness).
func ChoicesFor(typ string) Choices {
	choicesMu.RLock()
	defer choicesMu.RUnlock()
	c := choices[typ]
	return Choices{
		Models:          append([]Choice{}, c.Models...),
		Efforts:         append([]Choice{}, c.Efforts...),
		PermissionModes: append([]Choice{}, c.PermissionModes...),
	}
}
