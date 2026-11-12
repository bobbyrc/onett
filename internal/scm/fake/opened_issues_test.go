package fake

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFakeListOpenedIssuesReturnsOpenIssuesAfterSinceOldestFirst(t *testing.T) {
	f := New()
	base := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	one := f.AddIssue("one", "")
	two := f.AddIssue("two", "")
	three := f.AddIssue("three", "")
	f.Issues[one.Number].CreatedAt = base.Add(1 * time.Minute)
	f.Issues[two.Number].CreatedAt = base.Add(3 * time.Minute)
	f.Issues[three.Number].CreatedAt = base.Add(2 * time.Minute)
	f.Issues[three.Number].AuthorAssociation = "OWNER"
	require.NoError(t, f.CloseIssue(ctx, two.Number, "done"))

	got, err := f.ListOpenedIssues(ctx, base)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, one.Number, got[0].Number)
	assert.Equal(t, three.Number, got[1].Number)
	assert.Equal(t, "OWNER", got[1].AuthorAssociation)

	got, err = f.ListOpenedIssues(ctx, base.Add(90*time.Second))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, three.Number, got[0].Number)
}

func TestFakeListOpenedIssuesReturnsCopies(t *testing.T) {
	f := New()
	is := f.AddIssue("original", "")
	f.Issues[is.Number].CreatedAt = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)

	got, err := f.ListOpenedIssues(ctx, time.Time{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	got[0].Title = "changed"
	assert.Equal(t, "original", f.Issues[is.Number].Title)
}
