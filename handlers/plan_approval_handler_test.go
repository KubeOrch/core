package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type stubPlanApproval struct {
	createPlan      func(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreatePlanRequest, string) (models.PlanResponse, bool, error)
	listPlans       func(context.Context, primitive.ObjectID, int, string) (models.PlanListResponse, error)
	getPlan         func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.PlanResponse, error)
	requestApproval func(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreateApprovalRequest, string) (models.PlanResponse, bool, error)
	decidePlan      func(context.Context, primitive.ObjectID, models.MembershipRole, primitive.ObjectID, primitive.ObjectID, models.CreatePlanDecisionRequest, string) (models.PlanResponse, bool, error)
}

func (s stubPlanApproval) CreatePlan(ctx context.Context, actorID, workspaceID, applicationID, environmentID primitive.ObjectID, request models.CreatePlanRequest, key string) (models.PlanResponse, bool, error) {
	return s.createPlan(ctx, actorID, workspaceID, applicationID, environmentID, request, key)
}

func (s stubPlanApproval) ListPlans(ctx context.Context, workspaceID primitive.ObjectID, limit int, cursor string) (models.PlanListResponse, error) {
	return s.listPlans(ctx, workspaceID, limit, cursor)
}

func (s stubPlanApproval) GetPlan(ctx context.Context, workspaceID, planID primitive.ObjectID) (models.PlanResponse, error) {
	return s.getPlan(ctx, workspaceID, planID)
}

func (s stubPlanApproval) RequestApproval(ctx context.Context, actorID, workspaceID, planID primitive.ObjectID, request models.CreateApprovalRequest, key string) (models.PlanResponse, bool, error) {
	return s.requestApproval(ctx, actorID, workspaceID, planID, request, key)
}

func (s stubPlanApproval) DecidePlan(ctx context.Context, actorID primitive.ObjectID, role models.MembershipRole, workspaceID, planID primitive.ObjectID, request models.CreatePlanDecisionRequest, key string) (models.PlanResponse, bool, error) {
	return s.decidePlan(ctx, actorID, role, workspaceID, planID, request, key)
}

func TestCreatePlanUsesWorkspaceContextAndSetsReplayHeader(t *testing.T) {
	applicationID := primitive.NewObjectID()
	environmentID := primitive.NewObjectID()
	planID := primitive.NewObjectID()
	application := defaultStubPlanApproval()
	application.createPlan = func(_ context.Context, actorID, workspaceID, gotApplicationID, gotEnvironmentID primitive.ObjectID, request models.CreatePlanRequest, key string) (models.PlanResponse, bool, error) {
		assert.NotEqual(t, primitive.NilObjectID, actorID)
		assert.NotEqual(t, primitive.NilObjectID, workspaceID)
		assert.Equal(t, applicationID, gotApplicationID)
		assert.Equal(t, environmentID, gotEnvironmentID)
		assert.Equal(t, models.PlanSourceManual, request.Source)
		assert.Equal(t, "create-plan-1", key)
		return models.PlanResponse{
			ID: planID.Hex(), WorkspaceID: workspaceID.Hex(), ApplicationID: applicationID.Hex(), EnvironmentID: environmentID.Hex(),
			Status: models.PlanStatusProposed, AuditCorrelationID: "plan-correlation",
		}, true, nil
	}
	router, workspaceID := planApprovalTestRouter(NewPlanApprovalHandler(application), models.MembershipRoleMember)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/plans",
		bytes.NewBufferString(validPlanJSON(applicationID, environmentID)),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "create-plan-1")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusCreated, response.Code)
	assert.Equal(t, "true", response.Header().Get("Idempotency-Replayed"))
	var created models.PlanResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &created))
	assert.Equal(t, planID.Hex(), created.ID)
}

func TestCreatePlanRejectsUnknownFieldsAndInvalidReferences(t *testing.T) {
	called := false
	application := defaultStubPlanApproval()
	application.createPlan = func(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreatePlanRequest, string) (models.PlanResponse, bool, error) {
		called = true
		return models.PlanResponse{}, false, nil
	}
	router, workspaceID := planApprovalTestRouter(NewPlanApprovalHandler(application), models.MembershipRoleMember)

	unknown := httptest.NewRequest(
		http.MethodPost,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/plans",
		bytes.NewBufferString(`{"applicationId":"`+primitive.NewObjectID().Hex()+`","environmentId":"`+primitive.NewObjectID().Hex()+`","desiredRevision":"v2","source":"manual","diffSummary":"change","validation":{"status":"passed"},"cost":{"status":"unknown"},"policy":{"status":"passed"},"apply":true}`),
	)
	unknown.Header.Set("Idempotency-Key", "create-plan-1")
	unknownResponse := httptest.NewRecorder()
	router.ServeHTTP(unknownResponse, unknown)
	assert.Equal(t, http.StatusBadRequest, unknownResponse.Code)
	assert.Equal(t, "invalid_request", problemCode(t, unknownResponse))
	assert.False(t, called)

	invalidID := httptest.NewRequest(
		http.MethodPost,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/plans",
		bytes.NewBufferString(`{"applicationId":"not-an-id"}`),
	)
	invalidID.Header.Set("Idempotency-Key", "create-plan-2")
	invalidResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidResponse, invalidID)
	assert.Equal(t, http.StatusBadRequest, invalidResponse.Code)
	assert.Equal(t, "invalid_request", problemCode(t, invalidResponse))
	assert.False(t, called)
}

func TestPlanApprovalRequestRequiresIdempotencyAndReturnsTransition(t *testing.T) {
	planID := primitive.NewObjectID()
	application := defaultStubPlanApproval()
	called := false
	application.requestApproval = func(_ context.Context, _, workspaceID, gotPlanID primitive.ObjectID, request models.CreateApprovalRequest, key string) (models.PlanResponse, bool, error) {
		called = true
		assert.Equal(t, planID, gotPlanID)
		assert.Equal(t, "review evidence", request.Reason)
		assert.Equal(t, "approval-request-1", key)
		return models.PlanResponse{ID: planID.Hex(), WorkspaceID: workspaceID.Hex(), Status: models.PlanStatusApprovalRequested}, false, nil
	}
	router, workspaceID := planApprovalTestRouter(NewPlanApprovalHandler(application), models.MembershipRoleMember)

	missingKey := httptest.NewRequest(
		http.MethodPost,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/plans/"+planID.Hex()+"/approval-requests",
		bytes.NewBufferString(`{"reason":"review evidence"}`),
	)
	missingResponse := httptest.NewRecorder()
	router.ServeHTTP(missingResponse, missingKey)
	assert.Equal(t, http.StatusBadRequest, missingResponse.Code)
	assert.False(t, called)

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/plans/"+planID.Hex()+"/approval-requests",
		bytes.NewBufferString(`{"reason":"review evidence"}`),
	)
	request.Header.Set("Idempotency-Key", "approval-request-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, http.StatusCreated, response.Code)
	assert.True(t, called)
}

func TestPlanDecisionReceivesRoleAndMapsAuthorizationFailures(t *testing.T) {
	planID := primitive.NewObjectID()
	application := defaultStubPlanApproval()
	application.decidePlan = func(_ context.Context, _ primitive.ObjectID, role models.MembershipRole, _, _ primitive.ObjectID, _ models.CreatePlanDecisionRequest, _ string) (models.PlanResponse, bool, error) {
		assert.Equal(t, models.MembershipRoleMember, role)
		return models.PlanResponse{}, false, services.ErrPlanDecisionForbidden
	}
	router, workspaceID := planApprovalTestRouter(NewPlanApprovalHandler(application), models.MembershipRoleMember)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/plans/"+planID.Hex()+"/decisions",
		bytes.NewBufferString(`{"decision":"approve","reason":"verified"}`),
	)
	request.Header.Set("Idempotency-Key", "decision-1")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Equal(t, "plan_decision_forbidden", problemCode(t, response))
}

func TestPlanDecisionReturnsTerminalStateAndReplayHeader(t *testing.T) {
	planID := primitive.NewObjectID()
	application := defaultStubPlanApproval()
	application.decidePlan = func(_ context.Context, _ primitive.ObjectID, role models.MembershipRole, workspaceID, gotPlanID primitive.ObjectID, request models.CreatePlanDecisionRequest, key string) (models.PlanResponse, bool, error) {
		assert.Equal(t, models.MembershipRoleOwner, role)
		assert.Equal(t, planID, gotPlanID)
		assert.Equal(t, models.PlanDecisionReject, request.Decision)
		assert.Equal(t, "decision-1", key)
		return models.PlanResponse{
			ID: planID.Hex(), WorkspaceID: workspaceID.Hex(), Status: models.PlanStatusRejected,
			Decision: &models.PlanDecisionResponse{Decision: models.PlanDecisionReject, AuditCorrelationID: "decision-correlation"},
		}, true, nil
	}
	router, workspaceID := planApprovalTestRouter(NewPlanApprovalHandler(application), models.MembershipRoleOwner)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/plans/"+planID.Hex()+"/decisions",
		bytes.NewBufferString(`{"decision":"reject","reason":"policy failed"}`),
	)
	request.Header.Set("Idempotency-Key", "decision-1")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusCreated, response.Code)
	assert.Equal(t, "true", response.Header().Get("Idempotency-Replayed"))
	var decided models.PlanResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &decided))
	assert.Equal(t, models.PlanStatusRejected, decided.Status)
}

func TestPlanLookupHidesCrossWorkspaceResource(t *testing.T) {
	application := defaultStubPlanApproval()
	application.getPlan = func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.PlanResponse, error) {
		return models.PlanResponse{}, services.ErrPlanNotFound
	}
	router, workspaceID := planApprovalTestRouter(NewPlanApprovalHandler(application), models.MembershipRoleMember)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/plans/"+primitive.NewObjectID().Hex(),
		nil,
	))

	assert.Equal(t, http.StatusNotFound, response.Code)
	assert.Equal(t, "resource_not_found", problemCode(t, response))
}

func defaultStubPlanApproval() stubPlanApproval {
	return stubPlanApproval{
		createPlan: func(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreatePlanRequest, string) (models.PlanResponse, bool, error) {
			return models.PlanResponse{}, false, nil
		},
		listPlans: func(context.Context, primitive.ObjectID, int, string) (models.PlanListResponse, error) {
			return models.PlanListResponse{Items: []models.PlanResponse{}}, nil
		},
		getPlan: func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.PlanResponse, error) {
			return models.PlanResponse{}, nil
		},
		requestApproval: func(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreateApprovalRequest, string) (models.PlanResponse, bool, error) {
			return models.PlanResponse{}, false, nil
		},
		decidePlan: func(context.Context, primitive.ObjectID, models.MembershipRole, primitive.ObjectID, primitive.ObjectID, models.CreatePlanDecisionRequest, string) (models.PlanResponse, bool, error) {
			return models.PlanResponse{}, false, nil
		},
	}
}

func planApprovalTestRouter(handler *PlanApprovalHandler, role models.MembershipRole) (*gin.Engine, primitive.ObjectID) {
	gin.SetMode(gin.TestMode)
	actorID := primitive.NewObjectID()
	workspaceID := primitive.NewObjectID()
	resolver := stubWorkspaceMembershipResolver{find: func(_ context.Context, gotWorkspaceID, gotUserID primitive.ObjectID) (models.Membership, bool, error) {
		if gotWorkspaceID != workspaceID || gotUserID != actorID {
			return models.Membership{}, false, nil
		}
		return models.Membership{
			ID: primitive.NewObjectID(), WorkspaceID: gotWorkspaceID, UserID: gotUserID,
			Role: role, Status: models.MembershipStatusActive,
		}, true, nil
	}}
	router := gin.New()
	router.Use(middleware.RequestIDMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set("userID", actorID.Hex())
		c.Next()
	})
	workspaceScoped := router.Group("/v1/api/workspaces/:workspaceId")
	workspaceScoped.Use(middleware.WorkspaceAuthorizationMiddleware(resolver))
	workspaceScoped.POST("/plans", handler.CreatePlan)
	workspaceScoped.GET("/plans", handler.ListPlans)
	workspaceScoped.GET("/plans/:planId", handler.GetPlan)
	workspaceScoped.POST("/plans/:planId/approval-requests", handler.RequestApproval)
	workspaceScoped.POST("/plans/:planId/decisions", handler.DecidePlan)
	return router, workspaceID
}

func validPlanJSON(applicationID, environmentID primitive.ObjectID) string {
	return `{"applicationId":"` + applicationID.Hex() + `","environmentId":"` + environmentID.Hex() + `","desiredRevision":"checkout-v2","source":"manual","diffSummary":"Update checkout image","evidenceSummary":"CI passed","validation":{"status":"passed","reference":"https://ci.example.com/validation/482"},"cost":{"status":"unknown"},"policy":{"status":"passed","selfApprovalForbidden":true}}`
}
