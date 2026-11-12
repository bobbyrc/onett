package channel

import "testing"

// RegisterForTest is Register for a test: the type is removed again when the test ends. The
// registry lives as long as the process, so a test that registered a type for good could run only
// once in it (go test -count=2 panicked with "registered twice").
func RegisterForTest(t testing.TB, typ string, r Registration) {
	t.Helper()
	Register(typ, r)
	t.Cleanup(func() {
		mu.Lock()
		delete(types, typ)
		mu.Unlock()
	})
}
