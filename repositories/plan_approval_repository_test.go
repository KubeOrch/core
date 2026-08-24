package repositories

import (
	"testing"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestPlanCursorRoundTripAndWorkspaceBinding(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	planID := primitive.NewObjectID()
	createdAt := time.Date(2026, time.August, 24, 10, 30, 0, 123000000, time.UTC)
	cursor := encodeDomainCursor(planCursorKind, workspaceID, [8]byte{}, createdAt, planID)

	decodedCreatedAt, decodedPlanID, err := decodeDomainCursor(cursor, planCursorKind, workspaceID, [8]byte{})

	require.NoError(t, err)
	assert.Equal(t, createdAt, decodedCreatedAt)
	assert.Equal(t, planID, decodedPlanID)
	_, _, err = decodeDomainCursor(cursor, planCursorKind, primitive.NewObjectID(), [8]byte{})
	assert.ErrorIs(t, err, ErrInvalidCursor)
}

func TestListPlansRejectsInvalidPageSizeBeforeQueryingMongoDB(t *testing.T) {
	repository := &PlanApprovalRepository{}
	for _, limit := range []int{-1, 0, 101} {
		plans, cursor, err := repository.ListPlans(t.Context(), primitive.NewObjectID(), limit, "")
		assert.ErrorIs(t, err, ErrInvalidPlanPageSize)
		assert.Nil(t, plans)
		assert.Empty(t, cursor)
	}
}

func TestPlanTransitionFiltersEnforceWorkspaceAndExpectedState(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	planID := primitive.NewObjectID()

	assert.Equal(t, bson.M{
		"_id":              planID,
		"workspace_id":     workspaceID,
		"status":           models.PlanStatusProposed,
		"approval_request": bson.M{"$exists": false},
		"decision":         bson.M{"$exists": false},
	}, approvalRequestTransitionFilter(workspaceID, planID))
	assert.Equal(t, bson.M{
		"_id":          planID,
		"workspace_id": workspaceID,
		"status":       models.PlanStatusApprovalRequested,
		"decision":     bson.M{"$exists": false},
	}, decisionTransitionFilter(workspaceID, planID))
}

func TestPlanTransitionReplayIdentityIsActorKeyAndPayloadBound(t *testing.T) {
	actorID := primitive.NewObjectID()
	approval := models.PlanApprovalRequest{RequestedBy: actorID, IdempotencyKey: "approval-key", RequestHash: "approval-hash"}
	decision := models.PlanDecision{DecidedBy: actorID, IdempotencyKey: "decision-key", RequestHash: "decision-hash"}

	assert.True(t, sameApprovalRequest(approval, approval))
	changedApproval := approval
	changedApproval.RequestedBy = primitive.NewObjectID()
	assert.False(t, sameApprovalRequest(approval, changedApproval))
	changedApproval = approval
	changedApproval.RequestHash = "different"
	assert.False(t, sameApprovalRequest(approval, changedApproval))

	assert.True(t, samePlanDecision(decision, decision))
	changedDecision := decision
	changedDecision.IdempotencyKey = "different"
	assert.False(t, samePlanDecision(decision, changedDecision))
}
