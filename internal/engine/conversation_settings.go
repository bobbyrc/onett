package engine

import (
	"context"
	"fmt"
	"slices"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

// settingsAgent finds the agent profile whose defaults a conversation's settings override: the
// orchestrator for a chat, the root task's role for a task agent's conversation.
func (e *Engine) settingsAgent(ctx context.Context, conv domain.Conversation) (config.Agent, error) {
	project, role := conv.Project, "orchestrator"
	if conv.TaskID != "" {
		t, err := e.d.Store.GetTask(ctx, conv.TaskID)
		if err != nil {
			return config.Agent{}, err
		}
		project, role = t.Project, t.Role
	}
	a, ok := e.Config().AgentsFor(project)[role]
	if !ok {
		return config.Agent{}, fmt.Errorf("%w: unknown agent %q", api.ErrInvalid, role)
	}
	return a, nil
}

// settingsChoices is what the owner may pick for a conversation run by agent: the harness type's
// own list, plus the models named in the agents config for that harness.
func (e *Engine) settingsChoices(agent config.Agent) harness.Choices {
	cfg := e.Config()
	c := harness.ChoicesFor(cfg.Harness[agent.Harness].Type)
	seen := map[string]bool{}
	for _, m := range c.Models {
		seen[m.Value] = true
	}
	names := make([]string, 0, len(cfg.Agents))
	for n := range cfg.Agents {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		a := cfg.Agents[n]
		if a.Harness == agent.Harness && a.Model != "" && !seen[a.Model] {
			seen[a.Model] = true
			c.Models = append(c.Models, harness.Choice{Value: a.Model, Label: a.Model})
		}
	}
	if agent.Model != "" && !seen[agent.Model] {
		c.Models = append(c.Models, harness.Choice{Value: agent.Model, Label: agent.Model})
	}
	return c
}

// ConversationSettings returns a conversation's settings with the role's defaults and the choices
// its harness accepts.
func (e *Engine) ConversationSettings(ctx context.Context, id string) (api.SettingsView, error) {
	conv, err := e.d.Store.GetConversation(ctx, id)
	if err != nil {
		return api.SettingsView{}, err
	}
	return e.settingsView(ctx, conv)
}

func (e *Engine) settingsView(ctx context.Context, conv domain.Conversation) (api.SettingsView, error) {
	agent, err := e.settingsAgent(ctx, conv)
	if err != nil {
		return api.SettingsView{}, err
	}
	return api.SettingsView{
		Settings: conv.Settings,
		Defaults: domain.ConversationSettings{Model: agent.Model},
		Harness:  agent.Harness,
		Choices:  e.settingsChoices(agent),
	}, nil
}

// UpdateConversationSettings replaces a conversation's settings. Each non-empty value must be one
// of the harness's choices; a dangerous permission mode that is not already set needs
// confirmDangerous. The change applies from the conversation's next turn.
func (e *Engine) UpdateConversationSettings(ctx context.Context, id string, in domain.ConversationSettings, confirmDangerous bool) (api.SettingsView, error) {
	conv, err := e.d.Store.GetConversation(ctx, id)
	if err != nil {
		return api.SettingsView{}, err
	}
	view, err := e.settingsView(ctx, conv)
	if err != nil {
		return api.SettingsView{}, err
	}
	has := func(list []harness.Choice, v string) (harness.Choice, bool) {
		i := slices.IndexFunc(list, func(c harness.Choice) bool { return c.Value == v })
		if i < 0 {
			return harness.Choice{}, false
		}
		return list[i], true
	}
	for _, f := range []struct {
		name, value string
		list        []harness.Choice
	}{
		{"model", in.Model, view.Choices.Models},
		{"effort", in.Effort, view.Choices.Efforts},
		{"permission_mode", in.PermissionMode, view.Choices.PermissionModes},
	} {
		if f.value == "" {
			continue
		}
		if _, ok := has(f.list, f.value); !ok {
			return api.SettingsView{}, fmt.Errorf("%w: %s %q is not a choice for harness %s", api.ErrInvalid, f.name, f.value, view.Harness)
		}
	}
	if c, _ := has(view.Choices.PermissionModes, in.PermissionMode); c.Dangerous &&
		in.PermissionMode != conv.Settings.PermissionMode && !confirmDangerous {
		return api.SettingsView{}, fmt.Errorf("%w: permission mode %q skips safety checks; send confirm_dangerous to choose it", api.ErrInvalid, in.PermissionMode)
	}
	updated, err := e.d.Store.UpdateConversation(ctx, id, func(c *domain.Conversation) error {
		c.Settings = in
		return nil
	})
	if err != nil {
		return api.SettingsView{}, err
	}
	if in != conv.Settings {
		e.d.Bus.Emit(ctx, domain.EvConversationUpdated, updated.Project, updated.TaskID, "", "conversation settings changed", updated)
	}
	view.Settings = updated.Settings
	return view, nil
}

// effectiveSettings is what a turn of agent runs with: the conversation's choice, or the role's
// default where it made none.
func effectiveSettings(agent config.Agent, s domain.ConversationSettings) domain.ConversationSettings {
	if s.Model == "" {
		s.Model = agent.Model
	}
	return s
}

// taskSettings returns the settings stored for the conversation of a task agent, or
// none when it has no conversation yet.
func (e *Engine) taskSettings(ctx context.Context, rootID string) domain.ConversationSettings {
	conv, err := e.d.Store.GetConversationByTask(ctx, rootID)
	if err != nil {
		return domain.ConversationSettings{}
	}
	return conv.Settings
}
