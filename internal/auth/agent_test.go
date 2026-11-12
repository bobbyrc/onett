package auth

import (
	"context"
	"errors"
	"regexp"
	"testing"
)

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestAgentTokenIsDerivedAndStable(t *testing.T) {
	got := AgentToken("owner-secret")
	if !hex64.MatchString(got) {
		t.Fatalf("AgentToken = %q, want 64 lowercase hex characters", got)
	}
	if got == "owner-secret" {
		t.Fatal("AgentToken returned the owner token itself")
	}
	if again := AgentToken("owner-secret"); again != got {
		t.Errorf("AgentToken changed between calls: %q then %q", got, again)
	}
	if other := AgentToken("other-secret"); other == got {
		t.Error("AgentToken is the same for different owner tokens")
	}
}

func TestSetupCodeDiffersFromAgentToken(t *testing.T) {
	code := SetupCode("owner-secret")
	if !hex64.MatchString(code) {
		t.Fatalf("SetupCode = %q, want 64 lowercase hex characters", code)
	}
	if code == AgentToken("owner-secret") {
		t.Fatal("SetupCode equals AgentToken; the two must be unrelated values")
	}
	if again := SetupCode("owner-secret"); again != code {
		t.Errorf("SetupCode changed between calls: %q then %q", code, again)
	}
}

func TestEmptyOwnerTokenDerivesNothing(t *testing.T) {
	if got := AgentToken(""); got != "" {
		t.Errorf("AgentToken(\"\") = %q, want empty", got)
	}
	if got := SetupCode(""); got != "" {
		t.Errorf("SetupCode(\"\") = %q, want empty", got)
	}
}

func assertRefused(t *testing.T, a PersonAction) {
	t.Helper()
	err := Refused(WithAgent(context.Background()), a)
	if err == nil {
		t.Fatalf("Refused(agent, %q) = nil, want ErrPersonOnly", a)
	}
	if !errors.Is(err, ErrPersonOnly) {
		t.Errorf("Refused(agent, %q) = %v, want an error wrapping ErrPersonOnly", a, err)
	}
	if err := Refused(context.Background(), a); err != nil {
		t.Errorf("Refused(person, %q) = %v, want nil", a, err)
	}
}

func TestAgentTokenCannotApproveMerge(t *testing.T) { assertRefused(t, ApproveMerge) }

func TestAgentTokenCannotResumeFactory(t *testing.T) { assertRefused(t, ResumeFactory) }

func TestAgentTokenCannotResumeProject(t *testing.T) { assertRefused(t, ResumeProject) }

func TestAgentTokenCannotChangePausedGoal(t *testing.T) { assertRefused(t, ChangePausedGoal) }

func TestAgentTokenCannotRunLoop(t *testing.T) { assertRefused(t, RunLoop) }

func TestAgentTokenCannotResetGoalBudget(t *testing.T) { assertRefused(t, ResetGoalBudget) }

func TestAgentTokenCannotWriteConversation(t *testing.T) { assertRefused(t, WriteConversation) }

func TestAgentTokenCannotRetryDenied(t *testing.T) { assertRefused(t, RetryDenied) }

func TestRefusedAllowsUnlistedAction(t *testing.T) {
	if err := Refused(WithAgent(context.Background()), PersonAction("not_listed")); err != nil {
		t.Errorf("Refused for an unlisted action = %v, want nil", err)
	}
}

func TestIsAgent(t *testing.T) {
	if IsAgent(context.Background()) {
		t.Error("IsAgent(background) = true, want false")
	}
	if !IsAgent(WithAgent(context.Background())) {
		t.Error("IsAgent(WithAgent) = false, want true")
	}
}
