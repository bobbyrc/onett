package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/client"
)

func TestCostTodayShowsSubscriptionEstimate(t *testing.T) {
	assert.Equal(t, "$0.00 of $25.00 daily budget (+$38.47 estimated on subscriptions, not billed)",
		costToday(client.Stats{DailyBudgetUSD: 25, SubscriptionCostTodayUSD: 38.47}))
	assert.Equal(t, "$0.00 of $25.00 daily budget", costToday(client.Stats{DailyBudgetUSD: 25}))
}
