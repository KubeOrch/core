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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type stubArtifactReleaseApplication struct {
	createArtifact func(context.Context, primitive.ObjectID, primitive.ObjectID, models.CreateArtifactRequest, string) (models.ArtifactResponse, bool, error)
	listArtifacts  func(context.Context, primitive.ObjectID, int, string) (models.ArtifactListResponse, error)
	getArtifact    func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.ArtifactResponse, error)
	createRelease  func(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreateReleaseRequest, string) (models.ReleaseResponse, bool, error)
	listReleases   func(context.Context, primitive.ObjectID, primitive.ObjectID, int, string) (models.ReleaseListResponse, error)
	getRelease     func(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID) (models.ReleaseResponse, error)
}

func (s stubArtifactReleaseApplication) CreateArtifact(ctx context.Context, actorID, workspaceID primitive.ObjectID, request models.CreateArtifactRequest, key string) (models.ArtifactResponse, bool, error) {
	return s.createArtifact(ctx, actorID, workspaceID, request, key)
}

func (s stubArtifactReleaseApplication) ListArtifacts(ctx context.Context, workspaceID primitive.ObjectID, limit int, cursor string) (models.ArtifactListResponse, error) {
	if s.listArtifacts != nil {
		return s.listArtifacts(ctx, workspaceID, limit, cursor)
	}
	return models.ArtifactListResponse{Items: []models.ArtifactResponse{}}, nil
}

func (s stubArtifactReleaseApplication) GetArtifact(ctx context.Context, workspaceID, artifactID primitive.ObjectID) (models.ArtifactResponse, error) {
	if s.getArtifact != nil {
		return s.getArtifact(ctx, workspaceID, artifactID)
	}
	return models.ArtifactResponse{}, nil
}

func (s stubArtifactReleaseApplication) CreateRelease(ctx context.Context, actorID, workspaceID, applicationID primitive.ObjectID, request models.CreateReleaseRequest, key string) (models.ReleaseResponse, bool, error) {
	return s.createRelease(ctx, actorID, workspaceID, applicationID, request, key)
}

func (s stubArtifactReleaseApplication) ListReleases(ctx context.Context, workspaceID, applicationID primitive.ObjectID, limit int, cursor string) (models.ReleaseListResponse, error) {
	if s.listReleases != nil {
		return s.listReleases(ctx, workspaceID, applicationID, limit, cursor)
	}
	return models.ReleaseListResponse{Items: []models.ReleaseResponse{}}, nil
}

func (s stubArtifactReleaseApplication) GetRelease(ctx context.Context, workspaceID, applicationID, releaseID primitive.ObjectID) (models.ReleaseResponse, error) {
	if s.getRelease != nil {
		return s.getRelease(ctx, workspaceID, applicationID, releaseID)
	}
	return models.ReleaseResponse{}, nil
}

func TestCreateArtifactRequiresIdempotencyAndRejectsUnknownFields(t *testing.T) {
	called := false
	application := stubArtifactReleaseApplication{createArtifact: func(context.Context, primitive.ObjectID, primitive.ObjectID, models.CreateArtifactRequest, string) (models.ArtifactResponse, bool, error) {
		called = true
		return models.ArtifactResponse{}, false, nil
	}}
	router, workspaceID, _ := artifactReleaseTestRouter(NewArtifactReleaseHandler(application))
	body := artifactRequestJSON()

	missingKey := httptest.NewRecorder()
	router.ServeHTTP(missingKey, httptest.NewRequest(http.MethodPost, "/v1/api/workspaces/"+workspaceID.Hex()+"/artifacts", bytes.NewBufferString(body)))
	assert.Equal(t, http.StatusBadRequest, missingKey.Code)
	assert.Equal(t, "invalid_idempotency_key", problemCode(t, missingKey))

	unknown := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/api/workspaces/"+workspaceID.Hex()+"/artifacts", bytes.NewBufferString(strings.TrimSuffix(body, "}")+`,"token":"secret"}`))
	request.Header.Set("Idempotency-Key", "register-image-0001")
	router.ServeHTTP(unknown, request)
	assert.Equal(t, http.StatusBadRequest, unknown.Code)
	assert.Equal(t, "invalid_request", problemCode(t, unknown))
	assert.False(t, called)
}

func TestCreateArtifactReturnsResourceAndReplayHeader(t *testing.T) {
	artifactID := primitive.NewObjectID()
	application := stubArtifactReleaseApplication{createArtifact: func(_ context.Context, actorID, workspaceID primitive.ObjectID, request models.CreateArtifactRequest, key string) (models.ArtifactResponse, bool, error) {
		assert.NotEqual(t, primitive.NilObjectID, actorID)
		assert.Equal(t, "register-image-0001", key)
		assert.Equal(t, "ghcr.io/kubeorch/core@sha256:"+strings.Repeat("a", 64), request.Image)
		return models.ArtifactResponse{ID: artifactID.Hex(), WorkspaceID: workspaceID.Hex(), Image: request.Image, Evidence: models.ArtifactEvidence{}, CreatedBy: actorID.Hex()}, true, nil
	}}
	router, workspaceID, _ := artifactReleaseTestRouter(NewArtifactReleaseHandler(application))
	request := httptest.NewRequest(http.MethodPost, "/v1/api/workspaces/"+workspaceID.Hex()+"/artifacts", bytes.NewBufferString(artifactRequestJSON()))
	request.Header.Set("Idempotency-Key", "register-image-0001")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusCreated, response.Code)
	assert.Equal(t, "true", response.Header().Get("Idempotency-Replayed"))
	var artifact models.ArtifactResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &artifact))
	assert.Equal(t, artifactID.Hex(), artifact.ID)
}

func TestCreateAndGetReleaseUseApplicationScopedRoutes(t *testing.T) {
	applicationID, releaseID, artifactID := primitive.NewObjectID(), primitive.NewObjectID(), primitive.NewObjectID()
	createdCalled, fetchedCalled := false, false
	application := stubArtifactReleaseApplication{
		createArtifact: func(context.Context, primitive.ObjectID, primitive.ObjectID, models.CreateArtifactRequest, string) (models.ArtifactResponse, bool, error) {
			return models.ArtifactResponse{}, false, nil
		},
		createRelease: func(_ context.Context, actorID, workspaceID, gotApplicationID primitive.ObjectID, request models.CreateReleaseRequest, key string) (models.ReleaseResponse, bool, error) {
			createdCalled = true
			assert.Equal(t, applicationID, gotApplicationID)
			assert.Equal(t, "release-image-0001", key)
			assert.Equal(t, []string{artifactID.Hex()}, request.ArtifactIDs)
			return models.ReleaseResponse{ID: releaseID.Hex(), WorkspaceID: workspaceID.Hex(), ApplicationID: applicationID.Hex(), ArtifactIDs: request.ArtifactIDs, Source: request.Source, CreatedBy: actorID.Hex()}, false, nil
		},
		getRelease: func(_ context.Context, _, gotApplicationID, gotReleaseID primitive.ObjectID) (models.ReleaseResponse, error) {
			fetchedCalled = true
			assert.Equal(t, applicationID, gotApplicationID)
			assert.Equal(t, releaseID, gotReleaseID)
			return models.ReleaseResponse{ID: releaseID.Hex(), ApplicationID: applicationID.Hex(), ArtifactIDs: []string{artifactID.Hex()}}, nil
		},
	}
	router, workspaceID, _ := artifactReleaseTestRouter(NewArtifactReleaseHandler(application))
	path := "/v1/api/workspaces/" + workspaceID.Hex() + "/applications/" + applicationID.Hex() + "/releases"
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"applicationRevision":"revision-1","artifactIds":["`+artifactID.Hex()+`"],"source":"manual"}`))
	request.Header.Set("Idempotency-Key", "release-image-0001")
	created := httptest.NewRecorder()
	router.ServeHTTP(created, request)
	assert.Equal(t, http.StatusCreated, created.Code)
	assert.True(t, createdCalled)

	fetched := httptest.NewRecorder()
	router.ServeHTTP(fetched, httptest.NewRequest(http.MethodGet, path+"/"+releaseID.Hex(), nil))
	assert.Equal(t, http.StatusOK, fetched.Code)
	assert.True(t, fetchedCalled)
}

func TestArtifactReleaseRoutesRejectDifferentWorkspace(t *testing.T) {
	application := stubArtifactReleaseApplication{createArtifact: func(context.Context, primitive.ObjectID, primitive.ObjectID, models.CreateArtifactRequest, string) (models.ArtifactResponse, bool, error) {
		t.Fatal("application must not be called")
		return models.ArtifactResponse{}, false, nil
	}}
	router, _, _ := artifactReleaseTestRouter(NewArtifactReleaseHandler(application))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/api/workspaces/"+primitive.NewObjectID().Hex()+"/artifacts", nil))
	assert.Equal(t, http.StatusNotFound, response.Code)
	assert.Equal(t, "resource_not_found", problemCode(t, response))
}

func artifactReleaseTestRouter(handler *ArtifactReleaseHandler) (*gin.Engine, primitive.ObjectID, primitive.ObjectID) {
	gin.SetMode(gin.TestMode)
	actorID, workspaceID := primitive.NewObjectID(), primitive.NewObjectID()
	resolver := stubWorkspaceMembershipResolver{find: func(_ context.Context, gotWorkspaceID, gotUserID primitive.ObjectID) (models.Membership, bool, error) {
		if gotWorkspaceID != workspaceID || gotUserID != actorID {
			return models.Membership{}, false, nil
		}
		return models.Membership{
			ID: primitive.NewObjectID(), WorkspaceID: workspaceID, UserID: actorID,
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
	workspaceScoped.POST("/artifacts", handler.CreateArtifact)
	workspaceScoped.GET("/artifacts", handler.ListArtifacts)
	workspaceScoped.GET("/artifacts/:artifactId", handler.GetArtifact)
	workspaceScoped.POST("/applications/:applicationId/releases", handler.CreateRelease)
	workspaceScoped.GET("/applications/:applicationId/releases", handler.ListReleases)
	workspaceScoped.GET("/applications/:applicationId/releases/:releaseId", handler.GetRelease)
	return router, workspaceID, actorID
}

func artifactRequestJSON() string {
	return `{"image":"ghcr.io/kubeorch/core@sha256:` + strings.Repeat("a", 64) + `","source":{"repository":"https://github.com/kubeorch/core","ref":"refs/heads/main","sha":"` + strings.Repeat("b", 40) + `"}}`
}
