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

type stubEnvironmentApplication struct {
	createApplication  func(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreateApplicationRequest, string) (models.ApplicationResponse, bool, error)
	listApplications   func(context.Context, primitive.ObjectID, *primitive.ObjectID, bool, int, string) (models.ApplicationListResponse, error)
	getEnvironment     func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.EnvironmentResponse, error)
	updateApplication  func(context.Context, primitive.ObjectID, primitive.ObjectID, models.UpdateApplicationRequest) (models.ApplicationResponse, error)
	archiveApplication func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.ApplicationResponse, error)
}

func (s stubEnvironmentApplication) CreateEnvironment(context.Context, primitive.ObjectID, primitive.ObjectID, models.CreateEnvironmentRequest, string) (models.EnvironmentResponse, bool, error) {
	return models.EnvironmentResponse{}, false, nil
}

func (s stubEnvironmentApplication) ListEnvironments(context.Context, primitive.ObjectID, int, string) (models.EnvironmentListResponse, error) {
	return models.EnvironmentListResponse{}, nil
}

func (s stubEnvironmentApplication) GetEnvironment(ctx context.Context, workspaceID, environmentID primitive.ObjectID) (models.EnvironmentResponse, error) {
	if s.getEnvironment == nil {
		return models.EnvironmentResponse{}, nil
	}
	return s.getEnvironment(ctx, workspaceID, environmentID)
}

func (s stubEnvironmentApplication) UpdateEnvironment(context.Context, primitive.ObjectID, primitive.ObjectID, models.UpdateEnvironmentRequest) (models.EnvironmentResponse, error) {
	return models.EnvironmentResponse{}, nil
}

func (s stubEnvironmentApplication) CreateApplication(ctx context.Context, actorID, workspaceID, environmentID primitive.ObjectID, request models.CreateApplicationRequest, key string) (models.ApplicationResponse, bool, error) {
	return s.createApplication(ctx, actorID, workspaceID, environmentID, request, key)
}

func (s stubEnvironmentApplication) ListApplications(ctx context.Context, workspaceID primitive.ObjectID, environmentID *primitive.ObjectID, includeArchived bool, limit int, cursor string) (models.ApplicationListResponse, error) {
	return s.listApplications(ctx, workspaceID, environmentID, includeArchived, limit, cursor)
}

func (s stubEnvironmentApplication) GetApplication(context.Context, primitive.ObjectID, primitive.ObjectID) (models.ApplicationResponse, error) {
	return models.ApplicationResponse{}, nil
}

func (s stubEnvironmentApplication) UpdateApplication(ctx context.Context, workspaceID, applicationID primitive.ObjectID, request models.UpdateApplicationRequest) (models.ApplicationResponse, error) {
	if s.updateApplication != nil {
		return s.updateApplication(ctx, workspaceID, applicationID, request)
	}
	return models.ApplicationResponse{}, nil
}

func (s stubEnvironmentApplication) ArchiveApplication(ctx context.Context, workspaceID, applicationID primitive.ObjectID) (models.ApplicationResponse, error) {
	if s.archiveApplication != nil {
		return s.archiveApplication(ctx, workspaceID, applicationID)
	}
	return models.ApplicationResponse{}, nil
}

func TestCreateApplicationPreservesUnknownDesiredStateFields(t *testing.T) {
	environmentID := primitive.NewObjectID()
	application := stubEnvironmentApplication{
		createApplication: func(_ context.Context, _, workspaceID, gotEnvironmentID primitive.ObjectID, request models.CreateApplicationRequest, key string) (models.ApplicationResponse, bool, error) {
			assert.Equal(t, environmentID, gotEnvironmentID)
			assert.Equal(t, "create-checkout", key)
			assert.Equal(t, models.DesiredState{"futureField": map[string]any{"enabled": true}}, request.DesiredState)
			return models.ApplicationResponse{
				ID:            primitive.NewObjectID().Hex(),
				WorkspaceID:   workspaceID.Hex(),
				EnvironmentID: environmentID.Hex(),
				Name:          request.Name,
				DesiredState:  request.DesiredState,
				Status:        models.ApplicationStatusDraft,
			}, false, nil
		},
	}
	router, workspaceID := environmentApplicationTestRouter(NewEnvironmentApplicationHandler(application))
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/applications",
		bytes.NewBufferString(`{"environmentId":"`+environmentID.Hex()+`","name":"checkout","desiredState":{"futureField":{"enabled":true}}}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "create-checkout")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusCreated, response.Code)
	var created models.ApplicationResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &created))
	assert.Equal(t, map[string]any{"futureField": map[string]any{"enabled": true}}, created.DesiredState)
}

func TestApplicationCreateRejectsUnknownTopLevelFields(t *testing.T) {
	called := false
	application := stubEnvironmentApplication{
		createApplication: func(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreateApplicationRequest, string) (models.ApplicationResponse, bool, error) {
			called = true
			return models.ApplicationResponse{}, false, nil
		},
	}
	router, workspaceID := environmentApplicationTestRouter(NewEnvironmentApplicationHandler(application))
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/applications",
		bytes.NewBufferString(`{"environmentId":"`+primitive.NewObjectID().Hex()+`","name":"checkout","secret":"ignored"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "create-checkout")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.False(t, called)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Equal(t, "invalid_request", problemCode(t, response))
}

func TestApplicationCreateRequiresIdempotencyKey(t *testing.T) {
	called := false
	application := stubEnvironmentApplication{
		createApplication: func(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreateApplicationRequest, string) (models.ApplicationResponse, bool, error) {
			called = true
			return models.ApplicationResponse{}, false, nil
		},
	}
	router, workspaceID := environmentApplicationTestRouter(NewEnvironmentApplicationHandler(application))
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/applications",
		bytes.NewBufferString(`{"environmentId":"`+primitive.NewObjectID().Hex()+`","name":"checkout"}`),
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.False(t, called)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Equal(t, "invalid_idempotency_key", problemCode(t, response))
}

func TestApplicationCreateSetsReplayHeader(t *testing.T) {
	environmentID := primitive.NewObjectID()
	application := stubEnvironmentApplication{
		createApplication: func(_ context.Context, _, workspaceID, _ primitive.ObjectID, request models.CreateApplicationRequest, _ string) (models.ApplicationResponse, bool, error) {
			return models.ApplicationResponse{
				ID:            primitive.NewObjectID().Hex(),
				WorkspaceID:   workspaceID.Hex(),
				EnvironmentID: environmentID.Hex(),
				Name:          request.Name,
				DesiredState:  map[string]any{},
				Status:        models.ApplicationStatusDraft,
			}, true, nil
		},
	}
	router, workspaceID := environmentApplicationTestRouter(NewEnvironmentApplicationHandler(application))
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/applications",
		bytes.NewBufferString(`{"environmentId":"`+environmentID.Hex()+`","name":"checkout"}`),
	)
	request.Header.Set("Idempotency-Key", "create-checkout")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusCreated, response.Code)
	assert.Equal(t, "true", response.Header().Get("Idempotency-Replayed"))
}

func TestApplicationUpdateAndArchiveHandlers(t *testing.T) {
	applicationID := primitive.NewObjectID()
	updatedCalled := false
	archivedCalled := false
	application := stubEnvironmentApplication{
		updateApplication: func(_ context.Context, _, gotApplicationID primitive.ObjectID, request models.UpdateApplicationRequest) (models.ApplicationResponse, error) {
			updatedCalled = true
			assert.Equal(t, applicationID, gotApplicationID)
			require.NotNil(t, request.Name)
			assert.Equal(t, "payments", *request.Name)
			return models.ApplicationResponse{ID: applicationID.Hex(), Name: *request.Name, DesiredState: map[string]any{}}, nil
		},
		archiveApplication: func(_ context.Context, _, gotApplicationID primitive.ObjectID) (models.ApplicationResponse, error) {
			archivedCalled = true
			assert.Equal(t, applicationID, gotApplicationID)
			return models.ApplicationResponse{ID: applicationID.Hex(), DesiredState: map[string]any{}, Status: models.ApplicationStatusArchived}, nil
		},
	}
	router, workspaceID := environmentApplicationTestRouter(NewEnvironmentApplicationHandler(application))

	update := httptest.NewRequest(
		http.MethodPatch,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/applications/"+applicationID.Hex(),
		bytes.NewBufferString(`{"name":"payments"}`),
	)
	updateResponse := httptest.NewRecorder()
	router.ServeHTTP(updateResponse, update)
	assert.Equal(t, http.StatusOK, updateResponse.Code)
	assert.True(t, updatedCalled)

	archiveResponse := httptest.NewRecorder()
	router.ServeHTTP(archiveResponse, httptest.NewRequest(
		http.MethodDelete,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/applications/"+applicationID.Hex(),
		nil,
	))
	assert.Equal(t, http.StatusOK, archiveResponse.Code)
	assert.True(t, archivedCalled)
	var archived models.ApplicationResponse
	require.NoError(t, json.Unmarshal(archiveResponse.Body.Bytes(), &archived))
	assert.Equal(t, models.ApplicationStatusArchived, archived.Status)
}

func TestApplicationListDefaultsToActiveAndValidatesFilters(t *testing.T) {
	application := stubEnvironmentApplication{
		listApplications: func(_ context.Context, _ primitive.ObjectID, environmentID *primitive.ObjectID, includeArchived bool, limit int, cursor string) (models.ApplicationListResponse, error) {
			assert.Nil(t, environmentID)
			assert.False(t, includeArchived)
			assert.Equal(t, 20, limit)
			assert.Empty(t, cursor)
			return models.ApplicationListResponse{Items: []models.ApplicationResponse{}}, nil
		},
	}
	router, workspaceID := environmentApplicationTestRouter(NewEnvironmentApplicationHandler(application))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/api/workspaces/"+workspaceID.Hex()+"/applications", nil))
	assert.Equal(t, http.StatusOK, response.Code)

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/v1/api/workspaces/"+workspaceID.Hex()+"/applications?includeArchived=maybe", nil))
	assert.Equal(t, http.StatusBadRequest, invalid.Code)
	assert.Equal(t, "invalid_request", problemCode(t, invalid))
}

func TestEnvironmentLookupHidesCrossWorkspaceResources(t *testing.T) {
	application := stubEnvironmentApplication{
		getEnvironment: func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.EnvironmentResponse, error) {
			return models.EnvironmentResponse{}, services.ErrEnvironmentNotFound
		},
	}
	router, workspaceID := environmentApplicationTestRouter(NewEnvironmentApplicationHandler(application))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/v1/api/workspaces/"+workspaceID.Hex()+"/environments/"+primitive.NewObjectID().Hex(),
		nil,
	))

	assert.Equal(t, http.StatusNotFound, response.Code)
	assert.Equal(t, "resource_not_found", problemCode(t, response))
}

func environmentApplicationTestRouter(handler *EnvironmentApplicationHandler) (*gin.Engine, primitive.ObjectID) {
	gin.SetMode(gin.TestMode)
	actorID := primitive.NewObjectID()
	workspaceID := primitive.NewObjectID()
	resolver := stubWorkspaceMembershipResolver{find: func(_ context.Context, gotWorkspaceID, gotUserID primitive.ObjectID) (models.Membership, bool, error) {
		return models.Membership{
			ID: primitive.NewObjectID(), WorkspaceID: gotWorkspaceID, UserID: gotUserID,
			Role: models.MembershipRoleMember, Status: models.MembershipStatusActive,
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
	workspaceScoped.POST("/applications", handler.CreateApplication)
	workspaceScoped.GET("/applications", handler.ListApplications)
	workspaceScoped.PATCH("/applications/:applicationId", handler.UpdateApplication)
	workspaceScoped.DELETE("/applications/:applicationId", handler.ArchiveApplication)
	workspaceScoped.GET("/environments/:environmentId", handler.GetEnvironment)
	return router, workspaceID
}
