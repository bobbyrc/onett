package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// The trigger methods of the fake controller do nothing; trigCtl below replaces them for the
// trigger tests.
func (f *fakeCtl) CreateTrigger(context.Context, domain.Trigger, string) (domain.Trigger, error) {
	return domain.Trigger{}, nil
}

func (f *fakeCtl) UpdateTrigger(context.Context, string, domain.TriggerPatch, string) (domain.Trigger, error) {
	return domain.Trigger{}, nil
}

func (f *fakeCtl) DeleteTrigger(context.Context, string, string) error { return nil }

func (f *fakeCtl) RunTrigger(context.Context, string, string) (domain.TriggerFiring, error) {
	return domain.TriggerFiring{}, nil
}

// trigCtl is a Controller whose trigger methods write to the store and record who called.
type trigCtl struct {
	*fakeCtl
	st     store.Store
	by     []string // the "by" of every mutating call, in order
	runErr error    // returned by RunTrigger when set
}

func (c *trigCtl) CreateTrigger(ctx context.Context, t domain.Trigger, by string) (domain.Trigger, error) {
	c.by = append(c.by, by)
	return c.st.CreateTrigger(ctx, t)
}

func (c *trigCtl) UpdateTrigger(ctx context.Context, id string, p domain.TriggerPatch, by string) (domain.Trigger, error) {
	c.by = append(c.by, by)
	return c.st.UpdateTrigger(ctx, id, func(t *domain.Trigger) error {
		if p.Name != nil {
			t.Name = *p.Name
		}
		if p.Prompt != nil {
			t.Prompt = *p.Prompt
		}
		if p.Enabled != nil {
			t.Enabled = *p.Enabled
		}
		if p.Cron != nil {
			t.Cron = *p.Cron
		}
		if p.TimeZone != nil {
			t.TimeZone = *p.TimeZone
		}
		if p.Label != nil {
			t.Label = *p.Label
		}
		return nil
	})
}

func (c *trigCtl) DeleteTrigger(ctx context.Context, id, by string) error {
	c.by = append(c.by, by)
	return c.st.DeleteTrigger(ctx, id)
}

func (c *trigCtl) RunTrigger(ctx context.Context, id, by string) (domain.TriggerFiring, error) {
	c.by = append(c.by, by)
	if c.runErr != nil {
		return domain.TriggerFiring{}, c.runErr
	}
	if _, err := c.st.GetTrigger(ctx, id); err != nil {
		return domain.TriggerFiring{}, err
	}
	return c.st.CreateTriggerFiring(ctx, domain.TriggerFiring{
		TriggerID: id, Cause: domain.FiringCauseManual, By: by, Outcome: domain.FiringStarted,
	})
}

// newTriggerHarness is newHarness with a trigCtl as the controller.
func newTriggerHarness(t *testing.T) (*harness, *trigCtl) {
	t.Helper()
	x := newHarness(t)
	tc := &trigCtl{fakeCtl: x.ctl, st: x.st}
	x.h = NewServer(Options{
		Store:      x.st,
		Bus:        x.bus,
		Controller: tc,
		Token:      testToken,
		Version:    "test-version",
		Web:        fstest.MapFS{},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return x, tc
}

// seedTrigger stores a schedule trigger created at created.
func seedTrigger(t *testing.T, st store.Store, name string, created time.Time) domain.Trigger {
	t.Helper()
	tr, err := st.CreateTrigger(context.Background(), domain.Trigger{
		Name: name, Kind: domain.TriggerSchedule, Project: "demo", Prompt: "Look.",
		Enabled: true, Cron: "0 5 * * *", CreatedAt: created, UpdatedAt: created,
	})
	require.NoError(t, err)
	return tr
}

// seedFiring stores a firing of trigger id at at.
func seedFiring(t *testing.T, st store.Store, id, outcome string, at time.Time) domain.TriggerFiring {
	t.Helper()
	f, err := st.CreateTriggerFiring(context.Background(), domain.TriggerFiring{
		TriggerID: id, At: at, Cause: domain.FiringCauseSchedule, Outcome: outcome,
	})
	require.NoError(t, err)
	return f
}

const createTriggerBody = `{"name":"New","kind":"schedule","project":"demo","prompt":"p","enabled":true,"cron":"0 6 * * *"}`

type trigRoute struct{ method, path, body string }

func trigMutations(id string) []trigRoute {
	return []trigRoute{
		{http.MethodPost, "/api/v1/triggers", createTriggerBody},
		{http.MethodPatch, "/api/v1/triggers/" + id, `{"enabled":false}`},
		{http.MethodPost, "/api/v1/triggers/" + id + "/run", ""},
		{http.MethodDelete, "/api/v1/triggers/" + id, ""},
	}
}

func TestTriggerMutationsRefuseTheAPITokenAndAcceptAnOwnerSession(t *testing.T) {
	x, tc := newTriggerHarness(t)
	id := seedTrigger(t, x.st, "Morning", time.Now()).ID
	ops := x.newUserSession(t, "olga", domain.RoleOperator)
	viewer := x.newUserSession(t, "vic", domain.RoleViewer)
	owner := x.newUserSession(t, "alice", domain.RoleOwner)
	routes := trigMutations(id)

	for _, r := range routes {
		rec := x.authReq(t, r.method, r.path, r.body, withToken(testToken))
		assert.Equal(t, http.StatusForbidden, rec.Code, "token %s %s: %s", r.method, r.path, rec.Body.String())
		rec = x.authReq(t, r.method, r.path, r.body, withCookie(ops))
		assert.Equal(t, http.StatusForbidden, rec.Code, "operator %s %s: %s", r.method, r.path, rec.Body.String())
		rec = x.authReq(t, r.method, r.path, r.body, withCookie(viewer))
		assert.Equal(t, http.StatusForbidden, rec.Code, "viewer %s %s: %s", r.method, r.path, rec.Body.String())
		rec = x.authReq(t, r.method, r.path, r.body)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "anonymous %s %s: %s", r.method, r.path, rec.Body.String())
	}
	assert.Empty(t, tc.by, "the controller must not be called for a refused request")

	wantOwner := []int{http.StatusCreated, http.StatusOK, http.StatusOK, http.StatusNoContent}
	for i, r := range routes {
		rec := x.authReq(t, r.method, r.path, r.body, withCookie(owner))
		assert.Equal(t, wantOwner[i], rec.Code, "owner %s %s: %s", r.method, r.path, rec.Body.String())
	}
	assert.Equal(t, []string{"owner:alice", "owner:alice", "owner:alice", "owner:alice"}, tc.by)
}

func TestTriggerReadsAllowTheTokenAndAViewer(t *testing.T) {
	x, _ := newTriggerHarness(t)
	id := seedTrigger(t, x.st, "Morning", time.Now()).ID
	viewer := x.newUserSession(t, "vic", domain.RoleViewer)

	for _, path := range []string{"/api/v1/triggers", "/api/v1/triggers/" + id, "/api/v1/triggers/" + id + "/firings"} {
		rec := x.authReq(t, http.MethodGet, path, "", withToken(testToken))
		assert.Equal(t, http.StatusOK, rec.Code, "token GET %s", path)
		rec = x.authReq(t, http.MethodGet, path, "", withCookie(viewer))
		assert.Equal(t, http.StatusOK, rec.Code, "viewer GET %s", path)
	}
}

func TestCreateTriggerPassesOwnerFieldsAndReturns201(t *testing.T) {
	x, tc := newTriggerHarness(t)
	owner := x.newUserSession(t, "alice", domain.RoleOwner)
	body := `{"name":"Morning issues","kind":"schedule","project":"demo","prompt":"Look.","enabled":true,"cron":"0 5 * * *","time_zone":"America/Chicago"}`

	rec := x.authReq(t, http.MethodPost, "/api/v1/triggers", body, withCookie(owner))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var out TriggerView
	decode(t, rec, &out)
	assert.NotEmpty(t, out.ID)
	assert.Equal(t, "Morning issues", out.Name)
	assert.Equal(t, domain.TriggerSchedule, out.Kind)
	assert.Equal(t, "demo", out.Project)
	assert.Equal(t, "Look.", out.Prompt)
	assert.True(t, out.Enabled)
	assert.Equal(t, "0 5 * * *", out.Cron)
	assert.Equal(t, "America/Chicago", out.TimeZone)
	assert.Nil(t, out.LastFiring)
	assert.Contains(t, rec.Body.String(), `"time_zone"`)
	assert.Contains(t, rec.Body.String(), `"created_at"`)
	assert.NotContains(t, rec.Body.String(), `"last_firing"`)
	assert.Equal(t, []string{"owner:alice"}, tc.by)
}

func TestTriggerRoutesRejectUnknownFields(t *testing.T) {
	x, tc := newTriggerHarness(t)
	id := seedTrigger(t, x.st, "Morning", time.Now()).ID
	owner := x.newUserSession(t, "alice", domain.RoleOwner)

	rec := x.authReq(t, http.MethodPost, "/api/v1/triggers",
		`{"name":"x","kind":"schedule","project":"demo","prompt":"p","cron":"0 5 * * *","nope":1}`, withCookie(owner))
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	rec = x.authReq(t, http.MethodPatch, "/api/v1/triggers/"+id, `{"kind":"github_issue"}`, withCookie(owner))
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	rec = x.authReq(t, http.MethodPatch, "/api/v1/triggers/"+id, `{"project":"other"}`, withCookie(owner))
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	assert.Empty(t, tc.by, "a body with unknown fields must not reach the controller")
}

func TestListTriggersCarriesLastFiring(t *testing.T) {
	x, _ := newTriggerHarness(t)
	viewer := x.newUserSession(t, "vic", domain.RoleViewer)

	rec := x.authReq(t, http.MethodGet, "/api/v1/triggers", "", withCookie(viewer))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "[]", strings.TrimSpace(rec.Body.String()))

	base := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	a := seedTrigger(t, x.st, "First", base)
	b := seedTrigger(t, x.st, "Second", base.Add(time.Hour))
	seedFiring(t, x.st, a.ID, domain.FiringSkipped, base.Add(time.Minute))
	seedFiring(t, x.st, a.ID, domain.FiringStarted, base.Add(2*time.Minute))

	rec = x.authReq(t, http.MethodGet, "/api/v1/triggers", "", withCookie(viewer))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out []TriggerView
	decode(t, rec, &out)
	require.Len(t, out, 2)
	assert.Equal(t, a.ID, out[0].ID)
	assert.Equal(t, b.ID, out[1].ID)
	require.NotNil(t, out[0].LastFiring)
	assert.Equal(t, domain.FiringStarted, out[0].LastFiring.Outcome)
	assert.Nil(t, out[1].LastFiring)
}

func TestTriggerFiringsNewestFirstWithLimit(t *testing.T) {
	x, _ := newTriggerHarness(t)
	viewer := x.newUserSession(t, "vic", domain.RoleViewer)
	base := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	tr := seedTrigger(t, x.st, "Morning", base)
	empty := seedTrigger(t, x.st, "Quiet", base)
	f1 := seedFiring(t, x.st, tr.ID, domain.FiringStarted, base.Add(1*time.Minute))
	f2 := seedFiring(t, x.st, tr.ID, domain.FiringSkipped, base.Add(2*time.Minute))
	f3 := seedFiring(t, x.st, tr.ID, domain.FiringStarted, base.Add(3*time.Minute))
	path := "/api/v1/triggers/" + tr.ID + "/firings"

	rec := x.authReq(t, http.MethodGet, path, "", withCookie(viewer))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out []domain.TriggerFiring
	decode(t, rec, &out)
	require.Len(t, out, 3)
	assert.Equal(t, []string{f3.ID, f2.ID, f1.ID}, []string{out[0].ID, out[1].ID, out[2].ID})

	rec = x.authReq(t, http.MethodGet, path+"?limit=2", "", withCookie(viewer))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	out = nil
	decode(t, rec, &out)
	require.Len(t, out, 2)
	assert.Equal(t, []string{f3.ID, f2.ID}, []string{out[0].ID, out[1].ID})

	rec = x.authReq(t, http.MethodGet, path+"?limit=x", "", withCookie(viewer))
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	rec = x.authReq(t, http.MethodGet, "/api/v1/triggers/"+empty.ID+"/firings", "", withCookie(viewer))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "[]", strings.TrimSpace(rec.Body.String()))
}

func TestTriggerRoutesUnknownIDIs404(t *testing.T) {
	x, _ := newTriggerHarness(t)
	owner := x.newUserSession(t, "alice", domain.RoleOwner)
	cases := []trigRoute{
		{http.MethodGet, "/api/v1/triggers/nope", ""},
		{http.MethodPatch, "/api/v1/triggers/nope", `{"enabled":true}`},
		{http.MethodDelete, "/api/v1/triggers/nope", ""},
		{http.MethodPost, "/api/v1/triggers/nope/run", ""},
		{http.MethodGet, "/api/v1/triggers/nope/firings", ""},
	}
	for _, c := range cases {
		rec := x.authReq(t, c.method, c.path, c.body, withCookie(owner))
		assert.Equal(t, http.StatusNotFound, rec.Code, "%s %s: %s", c.method, c.path, rec.Body.String())
	}
}

func TestRunTriggerReturnsTheFiringAndMapsConflict(t *testing.T) {
	x, tc := newTriggerHarness(t)
	id := seedTrigger(t, x.st, "Morning", time.Now()).ID
	owner := x.newUserSession(t, "alice", domain.RoleOwner)

	rec := x.authReq(t, http.MethodPost, "/api/v1/triggers/"+id+"/run", "", withCookie(owner))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var f domain.TriggerFiring
	decode(t, rec, &f)
	assert.Equal(t, domain.FiringCauseManual, f.Cause)
	assert.Equal(t, "owner:alice", f.By)

	tc.runErr = fmt.Errorf("%w: run now is for schedule triggers", ErrConflict)
	rec = x.authReq(t, http.MethodPost, "/api/v1/triggers/"+id+"/run", "", withCookie(owner))
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

func TestPatchTriggerAppliesOnlyGivenFields(t *testing.T) {
	x, _ := newTriggerHarness(t)
	tr := seedTrigger(t, x.st, "Morning", time.Now())
	owner := x.newUserSession(t, "alice", domain.RoleOwner)

	rec := x.authReq(t, http.MethodPatch, "/api/v1/triggers/"+tr.ID, `{"enabled":false}`, withCookie(owner))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out TriggerView
	decode(t, rec, &out)
	assert.False(t, out.Enabled)
	assert.Equal(t, tr.Name, out.Name)
	assert.Equal(t, tr.Prompt, out.Prompt)
}
