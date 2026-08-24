package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type stubPlanApprovalStore struct {
	createPlan           func(context.Context, *models.Plan) error
	getPlanByCreationKey func(context.Context, primitive.ObjectID, primitive.ObjectID, string) (*models.Plan, error)
	listPlans            func(context.Context, primitive.ObjectID, int, string) ([]models.Plan, string, error)
	getPlan              func(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Plan, error)
	getApplication       func(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Application, error)
	getEnvironment       func(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Environment, error)
	requestApproval      func(context.Context, primitive.ObjectID, primitive.ObjectID, models.PlanApprovalRequest, time.Time) (*models.Plan, bool, error)
	recordDecision       func(context.Context, primitive.ObjectID, primitive.ObjectID, models.PlanDecision, models.PlanStatus, time.Time) (*models.Plan, bool, error)
}

func (s stubPlanApprovalStore) CreatePlan(ctx context.Context, plan *models.Plan) error {
	return s.createPlan(ctx, plan)
}

func (s stubPlanApprovalStore) GetPlanByCreationKey(ctx context.Context, workspaceID, actorID primitive.ObjectID, key string) (*models.Plan, error) {
	return s.getPlanByCreationKey(ctx, workspaceID, actorID, key)
}

func (s stubPlanApprovalStore) ListPlans(ctx context.Context, workspaceID primitive.ObjectID, limit int, cursor string) ([]models.Plan, string, error) {
	return s.listPlans(ctx, workspaceID, limit, cursor)
}

func (s stubPlanApprovalStore) GetPlan(ctx context.Context, workspaceID, planID primitive.ObjectID) (*models.Plan, error) {
	return s.getPlan(ctx, workspaceID, planID)
}

func (s stubPlanApprovalStore) GetApplication(ctx context.Context, workspaceID, applicationID primitive.ObjectID) (*models.Application, error) {
	return s.getApplication(ctx, workspaceID, applicationID)
}

func (s stubPlanApprovalStore) GetEnvironment(ctx context.Context, workspaceID, environmentID primitive.ObjectID) (*models.Environment, error) {
	return s.getEnvironment(ctx, workspaceID, environmentID)
}

func (s stubPlanApprovalStore) RequestApproval(ctx context.Context, workspaceID, planID primitive.ObjectID, request models.PlanApprovalRequest, updatedAt time.Time) (*models.Plan, bool, error) {
	return s.requestApproval(ctx, workspaceID, planID, request, updatedAt)
}

func (s stubPlanApprovalStore) RecordDecision(ctx context.Context, workspaceID, planID primitive.ObjectID, decision models.PlanDecision, status models.PlanStatus, updatedAt time.Time) (*models.Plan, bool, error) {
	return s.recordDecision(ctx, workspaceID, planID, decision, status, updatedAt)
}

func TestCreatePlanPersistsImmutableScopedProposal(t *testing.T) {
	fixture := newPlanServiceFixture(t)
	captured := (*models.Plan)(nil)
	fixture.store.createPlan = func(_ context.Context, plan *models.Plan) error {
		captured = plan
		return nil
	}

	request := validCreatePlanRequest(fixture.applicationID, fixture.environmentID)
	request.Policy.SelfApprovalForbidden = false
	response, replayed, err := fixture.service.CreatePlan(
		context.Background(), fixture.actorID, fixture.workspaceID, fixture.applicationID, fixture.environmentID,
		request, "create-plan-1",
	)

	require.NoError(t, err)
	assert.False(t, replayed)
	require.NotNil(t, captured)
	assert.Equal(t, models.PlanStatusProposed, captured.Status)
	assert.Equal(t, fixture.workspaceID, captured.WorkspaceID)
	assert.Equal(t, fixture.applicationID, captured.ApplicationID)
	assert.Equal(t, fixture.environmentID, captured.EnvironmentID)
	assert.Equal(t, "plan-correlation", captured.AuditCorrelationID)
	assert.True(t, captured.Policy.SelfApprovalForbidden)
	assert.NotEmpty(t, captured.CreationHash)
	assert.Nil(t, captured.ApprovalRequest)
	assert.Nil(t, captured.Decision)
	assert.Equal(t, captured.ID.Hex(), response.ID)
	assert.Equal(t, "plan-correlation", response.AuditCorrelationID)
	assert.True(t, response.Policy.SelfApprovalForbidden)
	assert.Equal(t, fixture.now, response.CreatedAt)
}

func TestCreatePlanRejectsCrossEnvironmentReference(t *testing.T) {
	fixture := newPlanServiceFixture(t)
	created := false
	fixture.store.createPlan = func(context.Context, *models.Plan) error {
		created = true
		return nil
	}
	fixture.store.getApplication = func(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Application, error) {
		return &models.Application{ID: fixture.applicationID, WorkspaceID: fixture.workspaceID, EnvironmentID: primitive.NewObjectID()}, nil
	}

	_, _, err := fixture.service.CreatePlan(
		context.Background(), fixture.actorID, fixture.workspaceID, fixture.applicationID, fixture.environmentID,
		validCreatePlanRequest(fixture.applicationID, fixture.environmentID), "create-plan-1",
	)

	assert.ErrorIs(t, err, ErrPlanReferenceNotFound)
	assert.False(t, created)
}

func TestCreatePlanReplaysStableAuditCorrelationAndDetectsConflict(t *testing.T) {
	fixture := newPlanServiceFixture(t)
	request := validCreatePlanRequest(fixture.applicationID, fixture.environmentID)
	requestHash, err := hashPlanPayload(request)
	require.NoError(t, err)
	existing := fixture.plan()
	existing.CreationKey = "create-plan-1"
	existing.CreationHash = requestHash
	existing.AuditCorrelationID = "original-correlation"
	existing.Status = models.PlanStatusApproved
	existing.ApprovalRequest = &models.PlanApprovalRequest{RequestedAt: fixture.now.Add(time.Minute)}
	existing.Decision = &models.PlanDecision{Decision: models.PlanDecisionApprove}
	existing.UpdatedAt = fixture.now.Add(2 * time.Minute)
	fixture.store.createPlan = func(context.Context, *models.Plan) error {
		t.Fatal("known creation replay must not attempt another insert")
		return nil
	}
	fixture.store.getPlanByCreationKey = func(context.Context, primitive.ObjectID, primitive.ObjectID, string) (*models.Plan, error) {
		return existing, nil
	}

	response, replayed, err := fixture.service.CreatePlan(
		context.Background(), fixture.actorID, fixture.workspaceID, fixture.applicationID, fixture.environmentID,
		request, "create-plan-1",
	)

	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, "original-correlation", response.AuditCorrelationID)
	assert.Equal(t, models.PlanStatusProposed, response.Status)
	assert.Nil(t, response.ApprovalRequest)
	assert.Nil(t, response.Decision)
	assert.Equal(t, response.CreatedAt, response.UpdatedAt)
	existing.CreationHash = "different"
	_, _, err = fixture.service.CreatePlan(
		context.Background(), fixture.actorID, fixture.workspaceID, fixture.applicationID, fixture.environmentID,
		request, "create-plan-1",
	)
	assert.ErrorIs(t, err, ErrDomainIdempotencyConflict)
}

func TestRequestApprovalReplayReturnsOriginalTransitionSnapshot(t *testing.T) {
	fixture := newPlanServiceFixture(t)
	requestHash, err := hashPlanPayload(struct {
		Reason string `json:"reason"`
	}{Reason: "review evidence"})
	require.NoError(t, err)
	plan := fixture.plan()
	plan.Status = models.PlanStatusApproved
	plan.ApprovalRequest = &models.PlanApprovalRequest{
		RequestedBy: fixture.actorID, Reason: "review evidence", AuditCorrelationID: "original-approval",
		IdempotencyKey: "approval-request-1", RequestHash: requestHash, RequestedAt: fixture.now.Add(time.Minute),
	}
	plan.Decision = &models.PlanDecision{Decision: models.PlanDecisionApprove, DecidedAt: fixture.now.Add(2 * time.Minute)}
	plan.UpdatedAt = fixture.now.Add(2 * time.Minute)
	fixture.store.requestApproval = func(context.Context, primitive.ObjectID, primitive.ObjectID, models.PlanApprovalRequest, time.Time) (*models.Plan, bool, error) {
		return plan, true, nil
	}

	response, replayed, err := fixture.service.RequestApproval(
		context.Background(), fixture.actorID, fixture.workspaceID, fixture.planID,
		models.CreateApprovalRequest{Reason: "review evidence"}, "approval-request-1",
	)

	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, models.PlanStatusApprovalRequested, response.Status)
	require.NotNil(t, response.ApprovalRequest)
	assert.Equal(t, "original-approval", response.ApprovalRequest.AuditCorrelationID)
	assert.Nil(t, response.Decision)
	assert.Equal(t, response.ApprovalRequest.RequestedAt, response.UpdatedAt)
}

func TestCreatePlanValidatesEvidenceAndResultReferences(t *testing.T) {
	fixture := newPlanServiceFixture(t)
	fixture.store.createPlan = func(context.Context, *models.Plan) error { return nil }

	request := validCreatePlanRequest(fixture.applicationID, fixture.environmentID)
	request.Policy.Reference = "https://user:token@example.com/policy"
	_, _, err := fixture.service.CreatePlan(
		context.Background(), fixture.actorID, fixture.workspaceID, fixture.applicationID, fixture.environmentID,
		request, "create-plan-1",
	)
	assert.ErrorIs(t, err, ErrInvalidPlanData)

	request = validCreatePlanRequest(fixture.applicationID, fixture.environmentID)
	request.Validation.Status = "successful"
	_, _, err = fixture.service.CreatePlan(
		context.Background(), fixture.actorID, fixture.workspaceID, fixture.applicationID, fixture.environmentID,
		request, "create-plan-1",
	)
	assert.ErrorIs(t, err, ErrInvalidPlanData)
}

func TestRequestApprovalPersistsStableTransitionIdentity(t *testing.T) {
	fixture := newPlanServiceFixture(t)
	fixture.store.requestApproval = func(_ context.Context, workspaceID, planID primitive.ObjectID, request models.PlanApprovalRequest, updatedAt time.Time) (*models.Plan, bool, error) {
		assert.Equal(t, fixture.workspaceID, workspaceID)
		assert.Equal(t, fixture.planID, planID)
		assert.Equal(t, fixture.actorID, request.RequestedBy)
		assert.Equal(t, "review production diff", request.Reason)
		assert.Equal(t, "approval-correlation", request.AuditCorrelationID)
		assert.Equal(t, "request-approval-1", request.IdempotencyKey)
		assert.NotEmpty(t, request.RequestHash)
		assert.Equal(t, fixture.now, updatedAt)
		plan := fixture.plan()
		plan.Status = models.PlanStatusApprovalRequested
		plan.ApprovalRequest = &request
		return plan, false, nil
	}
	fixture.service.newCorrelationID = func() string { return "approval-correlation" }

	response, replayed, err := fixture.service.RequestApproval(
		context.Background(), fixture.actorID, fixture.workspaceID, fixture.planID,
		models.CreateApprovalRequest{Reason: "  review production diff  "}, "request-approval-1",
	)

	require.NoError(t, err)
	assert.False(t, replayed)
	require.NotNil(t, response.ApprovalRequest)
	assert.Equal(t, "approval-correlation", response.ApprovalRequest.AuditCorrelationID)
	assert.Equal(t, models.PlanStatusApprovalRequested, response.Status)
}

func TestDecidePlanEnforcesRoleApprovalRequestAndSelfApprovalPolicy(t *testing.T) {
	fixture := newPlanServiceFixture(t)
	approvalPlan := fixture.plan()
	approvalPlan.Status = models.PlanStatusApprovalRequested
	approvalPlan.ApprovalRequest = &models.PlanApprovalRequest{RequestedBy: fixture.actorID}
	fixture.store.getPlan = func(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Plan, error) {
		return approvalPlan, nil
	}
	recorded := false
	fixture.store.recordDecision = func(context.Context, primitive.ObjectID, primitive.ObjectID, models.PlanDecision, models.PlanStatus, time.Time) (*models.Plan, bool, error) {
		recorded = true
		return approvalPlan, false, nil
	}

	_, _, err := fixture.service.DecidePlan(
		context.Background(), fixture.actorID, models.MembershipRoleMember, fixture.workspaceID, fixture.planID,
		models.CreatePlanDecisionRequest{Decision: models.PlanDecisionApprove, Reason: "looks good"}, "decision-1",
	)
	assert.ErrorIs(t, err, ErrPlanDecisionForbidden)
	assert.False(t, recorded)

	approvalPlan.Status = models.PlanStatusProposed
	approvalPlan.ApprovalRequest = nil
	_, _, err = fixture.service.DecidePlan(
		context.Background(), fixture.actorID, models.MembershipRoleOwner, fixture.workspaceID, fixture.planID,
		models.CreatePlanDecisionRequest{Decision: models.PlanDecisionApprove, Reason: "looks good"}, "decision-1",
	)
	assert.ErrorIs(t, err, ErrPlanApprovalRequired)
	assert.False(t, recorded)

	approvalPlan.Status = models.PlanStatusApprovalRequested
	approvalPlan.ApprovalRequest = &models.PlanApprovalRequest{RequestedBy: fixture.actorID}
	approvalPlan.Policy.SelfApprovalForbidden = false
	_, _, err = fixture.service.DecidePlan(
		context.Background(), fixture.actorID, models.MembershipRoleOwner, fixture.workspaceID, fixture.planID,
		models.CreatePlanDecisionRequest{Decision: models.PlanDecisionApprove, Reason: "looks good"}, "decision-1",
	)
	assert.ErrorIs(t, err, ErrPlanSelfApprovalForbidden)
	assert.False(t, recorded)
}

func TestDecidePlanRecordsTerminalDecisionWithoutApply(t *testing.T) {
	fixture := newPlanServiceFixture(t)
	approverID := primitive.NewObjectID()
	plan := fixture.plan()
	plan.Status = models.PlanStatusApprovalRequested
	plan.ApprovalRequest = &models.PlanApprovalRequest{RequestedBy: fixture.actorID}
	plan.Policy.SelfApprovalForbidden = true
	fixture.store.getPlan = func(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Plan, error) {
		return plan, nil
	}
	fixture.store.recordDecision = func(_ context.Context, workspaceID, planID primitive.ObjectID, decision models.PlanDecision, status models.PlanStatus, updatedAt time.Time) (*models.Plan, bool, error) {
		assert.Equal(t, fixture.workspaceID, workspaceID)
		assert.Equal(t, fixture.planID, planID)
		assert.Equal(t, approverID, decision.DecidedBy)
		assert.Equal(t, models.PlanDecisionApprove, decision.Decision)
		assert.Equal(t, "decision-correlation", decision.AuditCorrelationID)
		assert.Equal(t, models.PlanStatusApproved, status)
		assert.Equal(t, fixture.now, updatedAt)
		plan.Status = status
		plan.Decision = &decision
		return plan, false, nil
	}
	fixture.service.newCorrelationID = func() string { return "decision-correlation" }

	response, replayed, err := fixture.service.DecidePlan(
		context.Background(), approverID, models.MembershipRoleAdmin, fixture.workspaceID, fixture.planID,
		models.CreatePlanDecisionRequest{Decision: models.PlanDecisionApprove, Reason: "  policy evidence verified  "}, "decision-1",
	)

	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, models.PlanStatusApproved, response.Status)
	require.NotNil(t, response.Decision)
	assert.Equal(t, "decision-correlation", response.Decision.AuditCorrelationID)
	assert.Equal(t, "policy evidence verified", response.Decision.Reason)
}

func TestDecidePlanReplaysExactTerminalDecisionAndRejectsReplacement(t *testing.T) {
	fixture := newPlanServiceFixture(t)
	reason := "policy evidence verified"
	hash, err := hashPlanPayload(struct {
		Decision models.PlanDecisionType `json:"decision"`
		Reason   string                  `json:"reason"`
	}{Decision: models.PlanDecisionApprove, Reason: reason})
	require.NoError(t, err)
	plan := fixture.plan()
	plan.Status = models.PlanStatusApproved
	plan.Decision = &models.PlanDecision{
		Decision: models.PlanDecisionApprove, Reason: reason, DecidedBy: fixture.actorID,
		IdempotencyKey: "decision-1", RequestHash: hash, AuditCorrelationID: "original-decision",
	}
	fixture.store.getPlan = func(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Plan, error) {
		return plan, nil
	}
	fixture.store.recordDecision = func(context.Context, primitive.ObjectID, primitive.ObjectID, models.PlanDecision, models.PlanStatus, time.Time) (*models.Plan, bool, error) {
		t.Fatal("terminal replay must not call the mutation store")
		return nil, false, nil
	}

	response, replayed, err := fixture.service.DecidePlan(
		context.Background(), fixture.actorID, models.MembershipRoleOwner, fixture.workspaceID, fixture.planID,
		models.CreatePlanDecisionRequest{Decision: models.PlanDecisionApprove, Reason: reason}, "decision-1",
	)
	require.NoError(t, err)
	assert.True(t, replayed)
	require.NotNil(t, response.Decision)
	assert.Equal(t, "original-decision", response.Decision.AuditCorrelationID)

	_, _, err = fixture.service.DecidePlan(
		context.Background(), fixture.actorID, models.MembershipRoleOwner, fixture.workspaceID, fixture.planID,
		models.CreatePlanDecisionRequest{Decision: models.PlanDecisionReject, Reason: "changed mind"}, "decision-2",
	)
	assert.ErrorIs(t, err, ErrPlanDecisionTerminal)
}

func TestListPlansMapsWorkspaceBoundCursorErrors(t *testing.T) {
	fixture := newPlanServiceFixture(t)
	fixture.store.listPlans = func(context.Context, primitive.ObjectID, int, string) ([]models.Plan, string, error) {
		return nil, "", repositories.ErrInvalidCursor
	}

	_, err := fixture.service.ListPlans(context.Background(), fixture.workspaceID, 20, "wrong-workspace")

	assert.ErrorIs(t, err, ErrInvalidPlanData)
}

func TestListPlansMapsInvalidRepositoryPageSize(t *testing.T) {
	fixture := newPlanServiceFixture(t)
	fixture.store.listPlans = func(context.Context, primitive.ObjectID, int, string) ([]models.Plan, string, error) {
		return nil, "", repositories.ErrInvalidPlanPageSize
	}

	_, err := fixture.service.ListPlans(context.Background(), fixture.workspaceID, 0, "")

	assert.ErrorIs(t, err, ErrInvalidPlanData)
	assert.Contains(t, err.Error(), "limit must be between 1 and 100")
}

type planServiceFixture struct {
	service       *PlanApprovalService
	store         *stubPlanApprovalStore
	actorID       primitive.ObjectID
	workspaceID   primitive.ObjectID
	applicationID primitive.ObjectID
	environmentID primitive.ObjectID
	planID        primitive.ObjectID
	now           time.Time
}

func newPlanServiceFixture(t *testing.T) *planServiceFixture {
	t.Helper()
	fixture := &planServiceFixture{
		actorID:       primitive.NewObjectID(),
		workspaceID:   primitive.NewObjectID(),
		applicationID: primitive.NewObjectID(),
		environmentID: primitive.NewObjectID(),
		planID:        primitive.NewObjectID(),
		now:           time.Date(2026, time.August, 24, 11, 0, 0, 0, time.UTC),
	}
	store := &stubPlanApprovalStore{}
	store.createPlan = func(context.Context, *models.Plan) error { return nil }
	store.getPlanByCreationKey = func(context.Context, primitive.ObjectID, primitive.ObjectID, string) (*models.Plan, error) {
		return nil, repositories.ErrPlanNotFound
	}
	store.listPlans = func(context.Context, primitive.ObjectID, int, string) ([]models.Plan, string, error) {
		return []models.Plan{}, "", nil
	}
	store.getPlan = func(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Plan, error) {
		return fixture.plan(), nil
	}
	store.getApplication = func(_ context.Context, workspaceID, applicationID primitive.ObjectID) (*models.Application, error) {
		return &models.Application{
			ID: applicationID, WorkspaceID: workspaceID, EnvironmentID: fixture.environmentID,
		}, nil
	}
	store.getEnvironment = func(_ context.Context, workspaceID, environmentID primitive.ObjectID) (*models.Environment, error) {
		return &models.Environment{ID: environmentID, WorkspaceID: workspaceID}, nil
	}
	store.requestApproval = func(context.Context, primitive.ObjectID, primitive.ObjectID, models.PlanApprovalRequest, time.Time) (*models.Plan, bool, error) {
		return nil, false, errors.New("unexpected approval request")
	}
	store.recordDecision = func(context.Context, primitive.ObjectID, primitive.ObjectID, models.PlanDecision, models.PlanStatus, time.Time) (*models.Plan, bool, error) {
		return nil, false, errors.New("unexpected decision")
	}
	fixture.store = store
	fixture.service = NewPlanApprovalService(store)
	fixture.service.now = func() time.Time { return fixture.now }
	fixture.service.newCorrelationID = func() string { return "plan-correlation" }
	return fixture
}

func (f *planServiceFixture) plan() *models.Plan {
	return &models.Plan{
		ID:                 f.planID,
		WorkspaceID:        f.workspaceID,
		ApplicationID:      f.applicationID,
		EnvironmentID:      f.environmentID,
		DesiredRevision:    "checkout-v2",
		Source:             models.PlanSourceManual,
		DiffSummary:        "Update checkout image",
		Validation:         models.PlanResultReference{Status: models.PlanCheckStatusPassed},
		Cost:               models.PlanCostReference{Status: models.PlanCostStatusUnknown},
		Policy:             models.PlanPolicyResult{Status: models.PlanCheckStatusPassed},
		Status:             models.PlanStatusProposed,
		CreatedBy:          f.actorID,
		AuditCorrelationID: "plan-correlation",
		CreatedAt:          f.now,
		UpdatedAt:          f.now,
	}
}

func validCreatePlanRequest(applicationID, environmentID primitive.ObjectID) models.CreatePlanRequest {
	return models.CreatePlanRequest{
		ApplicationID:   applicationID.Hex(),
		EnvironmentID:   environmentID.Hex(),
		DesiredRevision: "checkout-v2",
		Source:          models.PlanSourceManual,
		DiffSummary:     "Update checkout image",
		EvidenceSummary: "CI run 482 passed",
		Validation: models.PlanResultReference{
			Status: models.PlanCheckStatusPassed, Reference: "https://ci.example.com/validation/482",
		},
		Cost: models.PlanCostReference{
			Status: models.PlanCostStatusAvailable, Reference: "https://cost.example.com/estimates/482",
		},
		Policy: models.PlanPolicyResult{
			Status: models.PlanCheckStatusPassed, Reference: "https://policy.example.com/results/482",
			SelfApprovalForbidden: true,
		},
	}
}
