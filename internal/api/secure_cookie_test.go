package api

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestSecureFlagFollowsTheSettingPerRequest(t *testing.T) {
	secure := false
	x := newHarnessWith(t, func(o *Options) {
		o.SecureCookiesFunc = func() bool { return secure }
	})
	x.newUserSession(t, "alice", domain.RoleOwner)

	assert.False(t, signIn(t, x).Secure)

	secure = true
	assert.True(t, signIn(t, x).Secure)
}
