package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/pkg/api"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WorkspaceMembershipResolver loads a membership without hiding its status.
type WorkspaceMembershipResolver interface {
	FindMembership(context.Context, primitive.ObjectID, primitive.ObjectID) (models.Membership, bool, error)
}

// WorkspaceAuthorization is an immutable value describing the authenticated
// caller's validated workspace boundary. Its fields are intentionally private.
type WorkspaceAuthorization struct {
	workspaceID  primitive.ObjectID
	membershipID primitive.ObjectID
	userID       primitive.ObjectID
	role         models.MembershipRole
}

func (a WorkspaceAuthorization) WorkspaceID() primitive.ObjectID {
	return a.workspaceID
}

func (a WorkspaceAuthorization) MembershipID() primitive.ObjectID {
	return a.membershipID
}

func (a WorkspaceAuthorization) UserID() primitive.ObjectID {
	return a.userID
}

func (a WorkspaceAuthorization) Role() models.MembershipRole {
	return a.role
}

type workspaceAuthorizationContextKey struct{}

var workspaceAuthorizationDenials = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "workspace_authorization_denials_total",
		Help: "Total number of workspace authorization requests rejected by reason.",
	},
	[]string{"reason"},
)

// WorkspaceAuthorizationFromContext is the only supported accessor for
// workspace-scoped handlers and services.
func WorkspaceAuthorizationFromContext(ctx context.Context) (WorkspaceAuthorization, bool) {
	authorization, ok := ctx.Value(workspaceAuthorizationContextKey{}).(WorkspaceAuthorization)
	return authorization, ok
}

// WorkspaceAuthorizationMiddleware resolves and validates the canonical route
// workspace before a scoped handler executes.
func WorkspaceAuthorizationMiddleware(resolver WorkspaceMembershipResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawWorkspaceID := c.Param("workspaceId")
		if rawWorkspaceID == "" {
			abortWorkspaceNotFound(c, "missing_workspace_id", primitive.NilObjectID, primitive.NilObjectID)
			return
		}
		workspaceID, err := primitive.ObjectIDFromHex(rawWorkspaceID)
		if err != nil {
			abortWorkspaceNotFound(c, "malformed_workspace_id", primitive.NilObjectID, primitive.NilObjectID)
			return
		}

		userID, err := GetUserID(c)
		if err != nil {
			abortWorkspaceAuthorization(c, http.StatusUnauthorized, "invalid_identity", "authentication_required", "Authentication required", "A valid authenticated identity is required.", workspaceID, primitive.NilObjectID, err)
			return
		}

		membership, found, err := resolver.FindMembership(c.Request.Context(), workspaceID, userID)
		if err != nil {
			abortWorkspaceAuthorization(c, http.StatusInternalServerError, "lookup_error", "internal_error", "Internal server error", "The request could not be completed.", workspaceID, userID, err)
			return
		}
		if !found {
			abortWorkspaceNotFound(c, "membership_not_found", workspaceID, userID)
			return
		}
		if membership.ID == primitive.NilObjectID || membership.WorkspaceID != workspaceID || membership.UserID != userID {
			abortWorkspaceNotFound(c, "membership_mismatch", workspaceID, userID)
			return
		}
		if membership.Status != models.MembershipStatusActive {
			abortWorkspaceNotFound(c, "membership_inactive", workspaceID, userID)
			return
		}

		role := models.MembershipRole(strings.ToLower(strings.TrimSpace(string(membership.Role))))
		if !role.IsValid() {
			abortWorkspaceNotFound(c, "invalid_role", workspaceID, userID)
			return
		}

		authorization := WorkspaceAuthorization{
			workspaceID:  workspaceID,
			membershipID: membership.ID,
			userID:       userID,
			role:         role,
		}
		requestContext := context.WithValue(c.Request.Context(), workspaceAuthorizationContextKey{}, authorization)
		c.Request = c.Request.WithContext(requestContext)
		c.Next()
	}
}

func abortWorkspaceNotFound(c *gin.Context, reason string, workspaceID, userID primitive.ObjectID) {
	abortWorkspaceAuthorization(c, http.StatusNotFound, reason, "resource_not_found", "Resource not found", "The requested resource was not found.", workspaceID, userID, nil)
}

func abortWorkspaceAuthorization(
	c *gin.Context,
	status int,
	reason, code, title, detail string,
	workspaceID, userID primitive.ObjectID,
	err error,
) {
	workspaceAuthorizationDenials.WithLabelValues(reason).Inc()
	fields := logrus.Fields{
		"reason":     reason,
		"request_id": api.RequestID(c),
	}
	if workspaceID != primitive.NilObjectID {
		fields["workspace_id"] = workspaceID.Hex()
	}
	if userID != primitive.NilObjectID {
		fields["actor_id"] = userID.Hex()
	}
	entry := logrus.WithFields(fields)
	if err != nil {
		entry = entry.WithError(err)
	}
	entry.Warn("Workspace authorization rejected")
	api.AbortProblem(c, status, code, title, detail)
}
