package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/pkg/api"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type stubWorkspaceApplication struct {
	create           func(context.Context, primitive.ObjectID, models.CreateWorkspaceRequest, string) (models.WorkspaceResponse, bool, error)
	get              func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.WorkspaceResponse, error)
	update           func(context.Context, primitive.ObjectID, primitive.ObjectID, models.UpdateWorkspaceRequest) (models.WorkspaceResponse, error)
	removeMembership func(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID) error
}

type stubWorkspaceMembershipResolver struct {
	find func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.Membership, bool, error)
}

func (s stubWorkspaceMembershipResolver) FindMembership(ctx context.Context, workspaceID, userID primitive.ObjectID) (models.Membership, bool, error) {
	return s.find(ctx, workspaceID, userID)
}

func (s stubWorkspaceApplication) CreateWorkspace(ctx context.Context, actorID primitive.ObjectID, request models.CreateWorkspaceRequest, key string) (models.WorkspaceResponse, bool, error) {
	return s.create(ctx, actorID, request, key)
}

func (s stubWorkspaceApplication) ListWorkspaces(context.Context, primitive.ObjectID, int, string) (models.WorkspaceListResponse, error) {
	return models.WorkspaceListResponse{}, nil
}

func (s stubWorkspaceApplication) GetWorkspace(ctx context.Context, actorID, workspaceID primitive.ObjectID) (models.WorkspaceResponse, error) {
	return s.get(ctx, actorID, workspaceID)
}

func (s stubWorkspaceApplication) UpdateWorkspace(ctx context.Context, actorID, workspaceID primitive.ObjectID, request models.UpdateWorkspaceRequest) (models.WorkspaceResponse, error) {
	return s.update(ctx, actorID, workspaceID, request)
}

func (s stubWorkspaceApplication) ListMemberships(context.Context, primitive.ObjectID, primitive.ObjectID, int, string) (models.MembershipListResponse, error) {
	return models.MembershipListResponse{}, nil
}

func (s stubWorkspaceApplication) AddMembership(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.MembershipRole) (models.MembershipResponse, bool, error) {
	return models.MembershipResponse{}, false, nil
}

func (s stubWorkspaceApplication) UpdateMembership(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.MembershipRole) (models.MembershipResponse, error) {
	return models.MembershipResponse{}, nil
}

func (s stubWorkspaceApplication) RemoveMembership(ctx context.Context, actorID, workspaceID, membershipID primitive.ObjectID) error {
	return s.removeMembership(ctx, actorID, workspaceID, membershipID)
}

func TestWorkspaceCreateRejectsUnknownFields(t *testing.T) {
	called := false
	application := stubWorkspaceApplication{
		create: func(context.Context, primitive.ObjectID, models.CreateWorkspaceRequest, string) (models.WorkspaceResponse, bool, error) {
			called = true
			return models.WorkspaceResponse{}, false, nil
		},
	}
	router, actorID := workspaceTestRouter(NewWorkspaceHandler(application))
	request := httptest.NewRequest(http.MethodPost, "/v1/api/workspaces", bytes.NewBufferString(`{"name":"Platform","secret":"must-not-be-ignored"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "create-platform")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.False(t, called)
	assert.NotEqual(t, primitive.NilObjectID, actorID)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
	assert.NotEmpty(t, response.Header().Get("X-Request-Id"))
	assert.Equal(t, "invalid_request", problemCode(t, response))
}

func TestWorkspaceCreateReplaysOriginalResponse(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	application := stubWorkspaceApplication{
		create: func(_ context.Context, _ primitive.ObjectID, request models.CreateWorkspaceRequest, key string) (models.WorkspaceResponse, bool, error) {
			assert.Equal(t, "Platform", request.Name)
			assert.Equal(t, "create-platform", key)
			return models.WorkspaceResponse{ID: workspaceID.Hex(), Name: request.Name, Role: models.MembershipRoleOwner}, true, nil
		},
	}
	router, _ := workspaceTestRouter(NewWorkspaceHandler(application))
	request := httptest.NewRequest(http.MethodPost, "/v1/api/workspaces", bytes.NewBufferString(`{"name":"Platform"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "create-platform")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusCreated, response.Code)
	assert.Equal(t, "true", response.Header().Get("Idempotency-Replayed"))
	var workspace models.WorkspaceResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &workspace))
	assert.Equal(t, workspaceID.Hex(), workspace.ID)
	assert.Equal(t, "Platform", workspace.Name)
	assert.Equal(t, models.MembershipRoleOwner, workspace.Role)
}

func TestWorkspaceCreateLimitsRequestBody(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "oversized object", body: `{"name":"` + strings.Repeat("x", int(maxWorkspaceRequestBodyBytes)) + `"}`},
		{name: "oversized trailing content", body: `{"name":"Platform"}` + strings.Repeat(" ", int(maxWorkspaceRequestBodyBytes))},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			application := stubWorkspaceApplication{
				create: func(context.Context, primitive.ObjectID, models.CreateWorkspaceRequest, string) (models.WorkspaceResponse, bool, error) {
					called = true
					return models.WorkspaceResponse{}, false, nil
				},
			}
			router, _ := workspaceTestRouter(NewWorkspaceHandler(application))
			request := httptest.NewRequest(http.MethodPost, "/v1/api/workspaces", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "create-platform")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assert.False(t, called)
			assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
			assert.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
			assert.Equal(t, "request_too_large", problemCode(t, response))
		})
	}
}

func TestWorkspaceGetUsesSameNotFoundResponseForInaccessibleResources(t *testing.T) {
	application := stubWorkspaceApplication{
		get: func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.WorkspaceResponse, error) {
			return models.WorkspaceResponse{}, services.ErrWorkspaceNotFound
		},
	}
	router, _ := workspaceTestRouter(NewWorkspaceHandler(application))

	for _, workspaceID := range []primitive.ObjectID{primitive.NewObjectID(), primitive.NewObjectID()} {
		request := httptest.NewRequest(http.MethodGet, "/v1/api/workspaces/"+workspaceID.Hex(), nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		assert.Equal(t, http.StatusNotFound, response.Code)
		assert.Equal(t, "resource_not_found", problemCode(t, response))
		assert.Contains(t, response.Body.String(), "The requested resource was not found.")
	}
}

func TestWorkspaceUpdateReturnsProblemForUnauthorizedMember(t *testing.T) {
	application := stubWorkspaceApplication{
		update: func(context.Context, primitive.ObjectID, primitive.ObjectID, models.UpdateWorkspaceRequest) (models.WorkspaceResponse, error) {
			return models.WorkspaceResponse{}, services.ErrWorkspaceForbidden
		},
	}
	router, _ := workspaceTestRouter(NewWorkspaceHandler(application))
	request := httptest.NewRequest(http.MethodPatch, "/v1/api/workspaces/"+primitive.NewObjectID().Hex(), bytes.NewBufferString(`{"name":"Renamed"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Equal(t, "insufficient_permissions", problemCode(t, response))
}

func TestWorkspaceRemoveProtectsLastOwner(t *testing.T) {
	application := stubWorkspaceApplication{
		removeMembership: func(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID) error {
			return services.ErrLastWorkspaceOwner
		},
	}
	router, _ := workspaceTestRouter(NewWorkspaceHandler(application))
	request := httptest.NewRequest(
		http.MethodDelete,
		"/v1/api/workspaces/"+primitive.NewObjectID().Hex()+"/members/"+primitive.NewObjectID().Hex(),
		nil,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusConflict, response.Code)
	assert.Equal(t, "last_owner_required", problemCode(t, response))
}

func TestWorkspaceScopedHandlersRequireValidatedMembership(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	actorID := primitive.NewObjectID()
	membershipID := primitive.NewObjectID()

	for _, test := range []struct {
		name       string
		membership models.Membership
		found      bool
		wantStatus int
		wantCalled bool
	}{
		{
			name: "active member",
			membership: models.Membership{
				ID: membershipID, WorkspaceID: workspaceID, UserID: actorID,
				Role: models.MembershipRoleMember, Status: models.MembershipStatusActive,
			},
			found: true, wantStatus: http.StatusOK, wantCalled: true,
		},
		{name: "non-member", found: false, wantStatus: http.StatusNotFound},
		{
			name: "disabled member",
			membership: models.Membership{
				ID: membershipID, WorkspaceID: workspaceID, UserID: actorID,
				Role: models.MembershipRoleMember, Status: models.MembershipStatusDisabled,
			},
			found: true, wantStatus: http.StatusNotFound,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			application := stubWorkspaceApplication{
				get: func(_ context.Context, gotActorID, gotWorkspaceID primitive.ObjectID) (models.WorkspaceResponse, error) {
					called = true
					assert.Equal(t, actorID, gotActorID)
					assert.Equal(t, workspaceID, gotWorkspaceID)
					return models.WorkspaceResponse{ID: gotWorkspaceID.Hex()}, nil
				},
			}
			resolver := stubWorkspaceMembershipResolver{find: func(_ context.Context, gotWorkspaceID, gotUserID primitive.ObjectID) (models.Membership, bool, error) {
				assert.Equal(t, workspaceID, gotWorkspaceID)
				assert.Equal(t, actorID, gotUserID)
				return test.membership, test.found, nil
			}}
			router := workspaceTestRouterWithResolver(NewWorkspaceHandler(application), actorID, resolver)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/v1/api/workspaces/"+workspaceID.Hex(), nil)

			router.ServeHTTP(response, request)

			assert.Equal(t, test.wantStatus, response.Code)
			assert.Equal(t, test.wantCalled, called)
			if !test.wantCalled {
				assert.Equal(t, "resource_not_found", problemCode(t, response))
			}
		})
	}
}

func TestWorkspaceScopedHandlerUsesRouteIdentityInsteadOfHeader(t *testing.T) {
	routeWorkspaceID := primitive.NewObjectID()
	headerWorkspaceID := primitive.NewObjectID()
	actorID := primitive.NewObjectID()
	application := stubWorkspaceApplication{
		get: func(_ context.Context, gotActorID, gotWorkspaceID primitive.ObjectID) (models.WorkspaceResponse, error) {
			assert.Equal(t, actorID, gotActorID)
			assert.Equal(t, routeWorkspaceID, gotWorkspaceID)
			return models.WorkspaceResponse{ID: gotWorkspaceID.Hex()}, nil
		},
	}
	resolver := stubWorkspaceMembershipResolver{find: func(_ context.Context, workspaceID, userID primitive.ObjectID) (models.Membership, bool, error) {
		return models.Membership{
			ID: primitive.NewObjectID(), WorkspaceID: workspaceID, UserID: userID,
			Role: models.MembershipRoleMember, Status: models.MembershipStatusActive,
		}, true, nil
	}}
	router := workspaceTestRouterWithResolver(NewWorkspaceHandler(application), actorID, resolver)
	request := httptest.NewRequest(http.MethodGet, "/v1/api/workspaces/"+routeWorkspaceID.Hex(), nil)
	request.Header.Set("X-Workspace-Id", headerWorkspaceID.Hex())
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
}

func TestWorkspaceScopedHandlerCannotExecuteWithoutAuthorizationContext(t *testing.T) {
	application := stubWorkspaceApplication{
		get: func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.WorkspaceResponse, error) {
			return models.WorkspaceResponse{}, nil
		},
	}
	router := gin.New()
	registerWorkspaceScopedTestRoutes(router.Group("/v1/api/workspaces/:workspaceId"), NewWorkspaceHandler(application))

	workspaceID := primitive.NewObjectID().Hex()
	membershipID := primitive.NewObjectID().Hex()
	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/v1/api/workspaces/" + workspaceID},
		{method: http.MethodPatch, path: "/v1/api/workspaces/" + workspaceID, body: `{}`},
		{method: http.MethodGet, path: "/v1/api/workspaces/" + workspaceID + "/members"},
		{method: http.MethodPost, path: "/v1/api/workspaces/" + workspaceID + "/members", body: `{}`},
		{method: http.MethodPatch, path: "/v1/api/workspaces/" + workspaceID + "/members/" + membershipID, body: `{}`},
		{method: http.MethodDelete, path: "/v1/api/workspaces/" + workspaceID + "/members/" + membershipID},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))

			router.ServeHTTP(response, request)

			assert.Equal(t, http.StatusInternalServerError, response.Code)
			assert.Equal(t, "workspace_context_required", problemCode(t, response))
		})
	}
}

func workspaceTestRouter(handler *WorkspaceHandler) (*gin.Engine, primitive.ObjectID) {
	gin.SetMode(gin.TestMode)
	actorID := primitive.NewObjectID()
	resolver := stubWorkspaceMembershipResolver{find: func(_ context.Context, workspaceID, userID primitive.ObjectID) (models.Membership, bool, error) {
		return models.Membership{
			ID:          primitive.NewObjectID(),
			WorkspaceID: workspaceID,
			UserID:      userID,
			Role:        models.MembershipRoleOwner,
			Status:      models.MembershipStatusActive,
		}, true, nil
	}}
	return workspaceTestRouterWithResolver(handler, actorID, resolver), actorID
}

func workspaceTestRouterWithResolver(handler *WorkspaceHandler, actorID primitive.ObjectID, resolver middleware.WorkspaceMembershipResolver) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.RequestIDMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set("userID", actorID.Hex())
		c.Next()
	})
	router.POST("/v1/api/workspaces", handler.Create)
	workspaceScoped := router.Group("/v1/api/workspaces/:workspaceId")
	workspaceScoped.Use(middleware.WorkspaceAuthorizationMiddleware(resolver))
	registerWorkspaceScopedTestRoutes(workspaceScoped, handler)
	return router
}

func registerWorkspaceScopedTestRoutes(workspaceScoped *gin.RouterGroup, handler *WorkspaceHandler) {
	workspaceScoped.GET("", handler.Get)
	workspaceScoped.PATCH("", handler.Update)
	workspaceScoped.GET("/members", handler.ListMembers)
	workspaceScoped.POST("/members", handler.AddMember)
	workspaceScoped.PATCH("/members/:memberId", handler.UpdateMember)
	workspaceScoped.DELETE("/members/:memberId", handler.RemoveMember)
}

func problemCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var problem api.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	return problem.Code
}
