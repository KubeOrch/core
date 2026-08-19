package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/pkg/api"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

type WorkspaceApplication interface {
	CreateWorkspace(context.Context, primitive.ObjectID, models.CreateWorkspaceRequest, string) (models.WorkspaceResponse, bool, error)
	ListWorkspaces(context.Context, primitive.ObjectID, int, string) (models.WorkspaceListResponse, error)
	GetWorkspace(context.Context, primitive.ObjectID, primitive.ObjectID) (models.WorkspaceResponse, error)
	UpdateWorkspace(context.Context, primitive.ObjectID, primitive.ObjectID, models.UpdateWorkspaceRequest) (models.WorkspaceResponse, error)
	ListMemberships(context.Context, primitive.ObjectID, primitive.ObjectID, int, string) (models.MembershipListResponse, error)
	AddMembership(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.MembershipRole) (models.MembershipResponse, bool, error)
	UpdateMembership(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.MembershipRole) (models.MembershipResponse, error)
	RemoveMembership(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID) error
}

type WorkspaceHandler struct {
	application WorkspaceApplication
}

func NewWorkspaceHandler(application WorkspaceApplication) *WorkspaceHandler {
	return &WorkspaceHandler{application: application}
}

func (h *WorkspaceHandler) Create(c *gin.Context) {
	actorID, ok := authenticatedUserID(c)
	if !ok {
		return
	}
	idempotencyKey := c.GetHeader("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		api.WriteProblem(c, http.StatusBadRequest, "invalid_idempotency_key", "Invalid idempotency key", "Idempotency-Key must contain 8 to 128 safe characters.")
		return
	}

	var request models.CreateWorkspaceRequest
	if !decodeRequest(c, &request) {
		return
	}
	workspace, replayed, err := h.application.CreateWorkspace(c.Request.Context(), actorID, request, idempotencyKey)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	logrus.WithFields(logrus.Fields{"workspace_id": workspace.ID, "actor_id": actorID.Hex(), "replayed": replayed}).Info("Workspace created")
	c.JSON(http.StatusCreated, workspace)
}

func (h *WorkspaceHandler) List(c *gin.Context) {
	actorID, ok := authenticatedUserID(c)
	if !ok {
		return
	}
	limit, cursor, ok := pagination(c)
	if !ok {
		return
	}
	response, err := h.application.ListWorkspaces(c.Request.Context(), actorID, limit, cursor)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *WorkspaceHandler) Get(c *gin.Context) {
	actorID, workspaceID, ok := requestIdentities(c)
	if !ok {
		return
	}
	response, err := h.application.GetWorkspace(c.Request.Context(), actorID, workspaceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *WorkspaceHandler) Update(c *gin.Context) {
	actorID, workspaceID, ok := requestIdentities(c)
	if !ok {
		return
	}
	var request models.UpdateWorkspaceRequest
	if !decodeRequest(c, &request) {
		return
	}
	response, err := h.application.UpdateWorkspace(c.Request.Context(), actorID, workspaceID, request)
	if err != nil {
		h.writeError(c, err)
		return
	}
	logrus.WithFields(logrus.Fields{"workspace_id": response.ID, "actor_id": actorID.Hex()}).Info("Workspace updated")
	c.JSON(http.StatusOK, response)
}

func (h *WorkspaceHandler) ListMembers(c *gin.Context) {
	actorID, workspaceID, ok := requestIdentities(c)
	if !ok {
		return
	}
	limit, cursor, ok := pagination(c)
	if !ok {
		return
	}
	response, err := h.application.ListMemberships(c.Request.Context(), actorID, workspaceID, limit, cursor)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *WorkspaceHandler) AddMember(c *gin.Context) {
	actorID, workspaceID, ok := requestIdentities(c)
	if !ok {
		return
	}
	var request models.AddMembershipRequest
	if !decodeRequest(c, &request) {
		return
	}
	targetUserID, err := primitive.ObjectIDFromHex(request.UserID)
	if err != nil {
		api.WriteProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", "userId is not a valid resource identifier.")
		return
	}

	response, replayed, err := h.application.AddMembership(c.Request.Context(), actorID, workspaceID, targetUserID, request.Role)
	if err != nil {
		h.writeError(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	logrus.WithFields(logrus.Fields{"workspace_id": workspaceID.Hex(), "membership_id": response.ID, "actor_id": actorID.Hex(), "replayed": replayed}).Info("Workspace membership added")
	c.JSON(status, response)
}

func (h *WorkspaceHandler) UpdateMember(c *gin.Context) {
	actorID, workspaceID, membershipID, ok := membershipRequestIdentities(c)
	if !ok {
		return
	}
	var request models.UpdateMembershipRequest
	if !decodeRequest(c, &request) {
		return
	}
	response, err := h.application.UpdateMembership(c.Request.Context(), actorID, workspaceID, membershipID, request.Role)
	if err != nil {
		h.writeError(c, err)
		return
	}
	logrus.WithFields(logrus.Fields{"workspace_id": workspaceID.Hex(), "membership_id": membershipID.Hex(), "actor_id": actorID.Hex()}).Info("Workspace membership updated")
	c.JSON(http.StatusOK, response)
}

func (h *WorkspaceHandler) RemoveMember(c *gin.Context) {
	actorID, workspaceID, membershipID, ok := membershipRequestIdentities(c)
	if !ok {
		return
	}
	if err := h.application.RemoveMembership(c.Request.Context(), actorID, workspaceID, membershipID); err != nil {
		h.writeError(c, err)
		return
	}
	logrus.WithFields(logrus.Fields{"workspace_id": workspaceID.Hex(), "membership_id": membershipID.Hex(), "actor_id": actorID.Hex()}).Info("Workspace membership removed")
	c.Status(http.StatusNoContent)
}

func (h *WorkspaceHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidWorkspaceData):
		detail := strings.TrimPrefix(err.Error(), services.ErrInvalidWorkspaceData.Error()+": ")
		api.WriteProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", detail)
	case errors.Is(err, services.ErrIdempotencyConflict):
		api.WriteProblem(c, http.StatusConflict, "idempotency_conflict", "Idempotency conflict", "The idempotency key was already used with a different request.")
	case errors.Is(err, services.ErrWorkspaceNotFound), errors.Is(err, services.ErrMembershipNotFound):
		api.WriteProblem(c, http.StatusNotFound, "resource_not_found", "Resource not found", "The requested resource was not found.")
	case errors.Is(err, services.ErrWorkspaceForbidden):
		api.WriteProblem(c, http.StatusForbidden, "insufficient_permissions", "Insufficient permissions", "The caller cannot perform this workspace operation.")
	case errors.Is(err, services.ErrLastWorkspaceOwner):
		api.WriteProblem(c, http.StatusConflict, "last_owner_required", "Last owner required", "A workspace must retain at least one owner.")
	case errors.Is(err, services.ErrMembershipExists):
		api.WriteProblem(c, http.StatusConflict, "membership_exists", "Membership already exists", "The user already has a membership in this workspace.")
	case errors.Is(err, services.ErrTargetUserNotFound):
		api.WriteProblem(c, http.StatusNotFound, "user_not_found", "User not found", "The requested user was not found.")
	case errors.Is(err, services.ErrWorkspaceConflict):
		api.WriteProblem(c, http.StatusConflict, "concurrent_update", "Concurrent update", "The workspace changed while the request was being processed; retry with fresh state.")
	default:
		logrus.WithError(err).WithField("request_id", api.RequestID(c)).Error("Workspace request failed")
		api.WriteProblem(c, http.StatusInternalServerError, "internal_error", "Internal server error", "The request could not be completed.")
	}
}

func authenticatedUserID(c *gin.Context) (primitive.ObjectID, bool) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		api.WriteProblem(c, http.StatusUnauthorized, "authentication_required", "Authentication required", "A valid authenticated identity is required.")
		return primitive.NilObjectID, false
	}
	return userID, true
}

func requestIdentities(c *gin.Context) (primitive.ObjectID, primitive.ObjectID, bool) {
	actorID, ok := authenticatedUserID(c)
	if !ok {
		return primitive.NilObjectID, primitive.NilObjectID, false
	}
	workspaceID, err := primitive.ObjectIDFromHex(c.Param("workspaceId"))
	if err != nil {
		api.WriteProblem(c, http.StatusNotFound, "resource_not_found", "Resource not found", "The requested resource was not found.")
		return primitive.NilObjectID, primitive.NilObjectID, false
	}
	return actorID, workspaceID, true
}

func membershipRequestIdentities(c *gin.Context) (primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, bool) {
	actorID, workspaceID, ok := requestIdentities(c)
	if !ok {
		return primitive.NilObjectID, primitive.NilObjectID, primitive.NilObjectID, false
	}
	membershipID, err := primitive.ObjectIDFromHex(c.Param("memberId"))
	if err != nil {
		api.WriteProblem(c, http.StatusNotFound, "resource_not_found", "Resource not found", "The requested resource was not found.")
		return primitive.NilObjectID, primitive.NilObjectID, primitive.NilObjectID, false
	}
	return actorID, workspaceID, membershipID, true
}

func pagination(c *gin.Context) (int, string, bool) {
	limit := 20
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 100 {
			api.WriteProblem(c, http.StatusBadRequest, "invalid_pagination", "Invalid pagination", "limit must be an integer from 1 to 100.")
			return 0, "", false
		}
		limit = parsed
	}
	cursor := c.Query("cursor")
	if len(cursor) > 1024 {
		api.WriteProblem(c, http.StatusBadRequest, "invalid_pagination", "Invalid pagination", "cursor must contain at most 1024 characters.")
		return 0, "", false
	}
	return limit, cursor, true
}

func decodeRequest(c *gin.Context, destination any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		api.WriteProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", safeJSONError(err))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		api.WriteProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", "The request body must contain one JSON object.")
		return false
	}
	return true
}

func safeJSONError(err error) string {
	var syntaxError *json.SyntaxError
	var typeError *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntaxError):
		return "The request body contains invalid JSON."
	case errors.As(err, &typeError):
		return fmt.Sprintf("The value for %s has the wrong type.", typeError.Field)
	case errors.Is(err, io.EOF):
		return "The request body is required."
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return err.Error()
	default:
		return "The request body is invalid."
	}
}
