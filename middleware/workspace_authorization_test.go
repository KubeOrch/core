package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/pkg/api"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type stubMembershipResolver struct {
	find func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.Membership, bool, error)
}

func (s stubMembershipResolver) FindMembership(ctx context.Context, workspaceID, userID primitive.ObjectID) (models.Membership, bool, error) {
	return s.find(ctx, workspaceID, userID)
}

func TestWorkspaceAuthorizationMiddlewareAttachesNormalizedContext(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	userID := primitive.NewObjectID()
	membershipID := primitive.NewObjectID()
	resolver := stubMembershipResolver{find: func(_ context.Context, gotWorkspaceID, gotUserID primitive.ObjectID) (models.Membership, bool, error) {
		assert.Equal(t, workspaceID, gotWorkspaceID)
		assert.Equal(t, userID, gotUserID)
		return models.Membership{
			ID: membershipID, WorkspaceID: workspaceID, UserID: userID,
			Role: models.MembershipRole(" OWNER "), Status: models.MembershipStatusActive,
		}, true, nil
	}}
	router := authorizationTestRouter(userID, resolver, func(c *gin.Context) {
		authorization, ok := WorkspaceAuthorizationFromContext(c.Request.Context())
		require.True(t, ok)
		assert.Equal(t, workspaceID, authorization.WorkspaceID())
		assert.Equal(t, membershipID, authorization.MembershipID())
		assert.Equal(t, userID, authorization.UserID())
		assert.Equal(t, models.MembershipRoleOwner, authorization.Role())
		c.Status(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/workspaces/"+workspaceID.Hex(), nil)

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusNoContent, response.Code)
}

func TestWorkspaceAuthorizationMiddlewareUsesNonEnumeratingDenials(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	userID := primitive.NewObjectID()
	membershipID := primitive.NewObjectID()

	for _, test := range []struct {
		name       string
		path       string
		membership models.Membership
		found      bool
	}{
		{name: "malformed workspace", path: "/workspaces/not-an-object-id"},
		{name: "non-member", path: "/workspaces/" + workspaceID.Hex()},
		{
			name: "disabled membership", path: "/workspaces/" + workspaceID.Hex(), found: true,
			membership: models.Membership{
				ID: membershipID, WorkspaceID: workspaceID, UserID: userID,
				Role: models.MembershipRoleMember, Status: models.MembershipStatusDisabled,
			},
		},
		{
			name: "cross-workspace membership", path: "/workspaces/" + workspaceID.Hex(), found: true,
			membership: models.Membership{
				ID: membershipID, WorkspaceID: primitive.NewObjectID(), UserID: userID,
				Role: models.MembershipRoleMember, Status: models.MembershipStatusActive,
			},
		},
		{
			name: "invalid membership role", path: "/workspaces/" + workspaceID.Hex(), found: true,
			membership: models.Membership{
				ID: membershipID, WorkspaceID: workspaceID, UserID: userID,
				Role: models.MembershipRole("super-admin"), Status: models.MembershipStatusActive,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			resolver := stubMembershipResolver{find: func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.Membership, bool, error) {
				return test.membership, test.found, nil
			}}
			router := authorizationTestRouter(userID, resolver, func(c *gin.Context) {
				called = true
				c.Status(http.StatusNoContent)
			})
			response := httptest.NewRecorder()

			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))

			assert.False(t, called)
			assert.Equal(t, http.StatusNotFound, response.Code)
			var problem api.Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			assert.Equal(t, "resource_not_found", problem.Code)
			assert.Equal(t, "The requested resource was not found.", problem.Detail)
		})
	}
}

func TestWorkspaceAuthorizationMiddlewareRejectsRouteWithoutWorkspaceParameter(t *testing.T) {
	resolverCalled := false
	resolver := stubMembershipResolver{find: func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.Membership, bool, error) {
		resolverCalled = true
		return models.Membership{}, false, nil
	}}
	router := gin.New()
	router.Use(RequestIDMiddleware())
	router.GET("/workspaces", WorkspaceAuthorizationMiddleware(resolver), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	response := httptest.NewRecorder()

	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/workspaces", nil))

	assert.False(t, resolverCalled)
	assert.Equal(t, http.StatusNotFound, response.Code)
	var problem api.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, "resource_not_found", problem.Code)
}

func TestWorkspaceAuthorizationMiddlewareIgnoresCallerWorkspaceHeader(t *testing.T) {
	routeWorkspaceID := primitive.NewObjectID()
	headerWorkspaceID := primitive.NewObjectID()
	userID := primitive.NewObjectID()
	resolver := stubMembershipResolver{find: func(_ context.Context, workspaceID, gotUserID primitive.ObjectID) (models.Membership, bool, error) {
		assert.Equal(t, routeWorkspaceID, workspaceID)
		return models.Membership{
			ID: primitive.NewObjectID(), WorkspaceID: workspaceID, UserID: gotUserID,
			Role: models.MembershipRoleMember, Status: models.MembershipStatusActive,
		}, true, nil
	}}
	router := authorizationTestRouter(userID, resolver, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/workspaces/"+routeWorkspaceID.Hex(), nil)
	request.Header.Set("X-Workspace-Id", headerWorkspaceID.Hex())
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusNoContent, response.Code)
}

func TestWorkspaceAuthorizationMiddlewareRecordsDenialReason(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	userID := primitive.NewObjectID()
	resolver := stubMembershipResolver{find: func(context.Context, primitive.ObjectID, primitive.ObjectID) (models.Membership, bool, error) {
		return models.Membership{
			ID: primitive.NewObjectID(), WorkspaceID: workspaceID, UserID: userID,
			Role: models.MembershipRoleMember, Status: models.MembershipStatusDisabled,
		}, true, nil
	}}
	counter := workspaceAuthorizationDenials.WithLabelValues("membership_inactive")
	before := prometheusCounterValue(t, counter)
	router := authorizationTestRouter(userID, resolver, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	response := httptest.NewRecorder()

	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/workspaces/"+workspaceID.Hex(), nil))

	assert.Equal(t, before+1, prometheusCounterValue(t, counter))
}

func TestLogsMiddlewareIncludesSafeWorkspaceCorrelation(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	membershipID := primitive.NewObjectID()
	userID := primitive.NewObjectID()
	var output bytes.Buffer
	logger := logrus.StandardLogger()
	previousOutput := logger.Out
	previousFormatter := logger.Formatter
	logger.SetOutput(&output)
	logger.SetFormatter(&logrus.JSONFormatter{})
	t.Cleanup(func() {
		logger.SetOutput(previousOutput)
		logger.SetFormatter(previousFormatter)
	})

	router := gin.New()
	router.Use(LogsMiddleware())
	router.GET("/workspaces/:workspaceId", func(c *gin.Context) {
		authorization := WorkspaceAuthorization{
			workspaceID: workspaceID, membershipID: membershipID, userID: userID,
			role: models.MembershipRoleMember,
		}
		requestContext := context.WithValue(c.Request.Context(), workspaceAuthorizationContextKey{}, authorization)
		c.Request = c.Request.WithContext(requestContext)
		c.Status(http.StatusNoContent)
	})
	response := httptest.NewRecorder()

	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/workspaces/"+workspaceID.Hex(), nil))

	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	var entry map[string]any
	for {
		var current map[string]any
		err := decoder.Decode(&current)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		entry = current
	}
	require.NotNil(t, entry)
	assert.Equal(t, workspaceID.Hex(), entry["workspace_id"])
	assert.Equal(t, membershipID.Hex(), entry["membership_id"])
	assert.Equal(t, userID.Hex(), entry["actor_id"])
	assert.Equal(t, string(models.MembershipRoleMember), entry["workspace_role"])
}

func TestWorkspaceAuthorizationContextsDoNotLeakBetweenConcurrentRequests(t *testing.T) {
	resolver := stubMembershipResolver{find: func(_ context.Context, workspaceID, userID primitive.ObjectID) (models.Membership, bool, error) {
		return models.Membership{
			ID: workspaceID, WorkspaceID: workspaceID, UserID: userID,
			Role: models.MembershipRoleMember, Status: models.MembershipStatusActive,
		}, true, nil
	}}
	router := gin.New()
	router.Use(RequestIDMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set("userID", c.GetHeader("X-Test-User-Id"))
		c.Next()
	})
	workspaceScoped := router.Group("/workspaces/:workspaceId")
	workspaceScoped.Use(WorkspaceAuthorizationMiddleware(resolver))
	workspaceScoped.GET("", func(c *gin.Context) {
		authorization, ok := WorkspaceAuthorizationFromContext(c.Request.Context())
		if !ok {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"workspaceId": authorization.WorkspaceID().Hex(),
			"userId":      authorization.UserID().Hex(),
		})
	})

	const requestCount = 64
	errorsFound := make(chan error, requestCount)
	var waitGroup sync.WaitGroup
	for range requestCount {
		workspaceID := primitive.NewObjectID()
		userID := primitive.NewObjectID()
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			request := httptest.NewRequest(http.MethodGet, "/workspaces/"+workspaceID.Hex(), nil)
			request.Header.Set("X-Test-User-Id", userID.Hex())
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				errorsFound <- fmt.Errorf("unexpected status %d", response.Code)
				return
			}
			var body struct {
				WorkspaceID string `json:"workspaceId"`
				UserID      string `json:"userId"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				errorsFound <- err
				return
			}
			if body.WorkspaceID != workspaceID.Hex() || body.UserID != userID.Hex() {
				errorsFound <- errors.New("workspace authorization context leaked between requests")
			}
		}()
	}
	waitGroup.Wait()
	close(errorsFound)
	for err := range errorsFound {
		assert.NoError(t, err)
	}
}

func authorizationTestRouter(userID primitive.ObjectID, resolver WorkspaceMembershipResolver, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestIDMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set("userID", userID.Hex())
		c.Next()
	})
	workspaceScoped := router.Group("/workspaces/:workspaceId")
	workspaceScoped.Use(WorkspaceAuthorizationMiddleware(resolver))
	workspaceScoped.GET("", handler)
	return router
}

func prometheusCounterValue(t *testing.T, counter prometheus.Counter) float64 {
	t.Helper()
	metric := &dto.Metric{}
	require.NoError(t, counter.Write(metric))
	return metric.GetCounter().GetValue()
}
