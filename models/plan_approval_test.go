package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlanContractEnumsRejectUnknownValues(t *testing.T) {
	assert.True(t, PlanSourceAI.IsValid())
	assert.False(t, PlanSource("plugin").IsValid())
	assert.True(t, PlanCheckStatusWarning.IsValid())
	assert.False(t, PlanCheckStatus("successful").IsValid())
	assert.True(t, PlanCostStatusUnavailable.IsValid())
	assert.False(t, PlanCostStatus("pending").IsValid())
	assert.True(t, PlanDecisionApprove.IsValid())
	assert.False(t, PlanDecisionType("approved").IsValid())
}
