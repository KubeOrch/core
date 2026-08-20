package handlers

import (
	"context"
	"errors"
	"net/http"
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

type EnvironmentApplication interface {
	CreateEnvironment(context.Context, primitive.ObjectID, primitive.ObjectID, models.CreateEnvironmentRequest, string) (models.EnvironmentResponse, bool, error)
	ListEnvironments(context.Context, primitive.ObjectID, int, string) (models.EnvironmentListResponse, error)
	GetEnvironment(context.Context, primitive.ObjectID, primitive.ObjectID) (models.EnvironmentResponse, error)
	UpdateEnvironment(context.Context, primitive.ObjectID, primitive.ObjectID, models.UpdateEnvironmentRequest) (models.EnvironmentResponse, error)
	CreateApplication(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreateApplicationRequest, string) (models.ApplicationResponse, bool, error)
	ListApplications(context.Context, primitive.ObjectID, *primitive.ObjectID, bool, int, string) (models.ApplicationListResponse, error)
	GetApplication(context.Context, primitive.ObjectID, primitive.ObjectID) (models.ApplicationResponse, error)
	UpdateApplication(context.Context, primitive.ObjectID, primitive.ObjectID, models.UpdateApplicationRequest) (models.ApplicationResponse, error)
	ArchiveApplication(context.Context, primitive.ObjectID, primitive.ObjectID) (models.ApplicationResponse, error)
}

type EnvironmentApplicationHandler struct {
	application EnvironmentApplication
}

func NewEnvironmentApplicationHandler(application EnvironmentApplication) *EnvironmentApplicationHandler {
	return &EnvironmentApplicationHandler{application: application}
}

func (h *EnvironmentApplicationHandler) CreateEnvironment(c *gin.Context) {
	authorization, ok := workspaceRequestAuthorization(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(c)
	if !ok {
		return
	}
	var request models.CreateEnvironmentRequest
	if !decodeRequest(c, &request) {
		return
	}
	response, replayed, err := h.application.CreateEnvironment(
		c.Request.Context(), authorization.UserID(), authorization.WorkspaceID(), request, idempotencyKey,
	)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	logrus.WithFields(logrus.Fields{
		"workspace_id":   authorization.WorkspaceID().Hex(),
		"environment_id": response.ID,
		"actor_id":       authorization.UserID().Hex(),
		"replayed":       replayed,
	}).Info("Environment created")
	c.JSON(http.StatusCreated, response)
}

func (h *EnvironmentApplicationHandler) ListEnvironments(c *gin.Context) {
	authorization, ok := workspaceRequestAuthorization(c)
	if !ok {
		return
	}
	limit, cursor, ok := pagination(c)
	if !ok {
		return
	}
	response, err := h.application.ListEnvironments(c.Request.Context(), authorization.WorkspaceID(), limit, cursor)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *EnvironmentApplicationHandler) GetEnvironment(c *gin.Context) {
	authorization, environmentID, ok := domainResourceAuthorization(c, "environmentId")
	if !ok {
		return
	}
	response, err := h.application.GetEnvironment(c.Request.Context(), authorization.WorkspaceID(), environmentID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *EnvironmentApplicationHandler) UpdateEnvironment(c *gin.Context) {
	authorization, environmentID, ok := domainResourceAuthorization(c, "environmentId")
	if !ok {
		return
	}
	var request models.UpdateEnvironmentRequest
	if !decodeRequest(c, &request) {
		return
	}
	response, err := h.application.UpdateEnvironment(c.Request.Context(), authorization.WorkspaceID(), environmentID, request)
	if err != nil {
		h.writeError(c, err)
		return
	}
	logrus.WithFields(logrus.Fields{
		"workspace_id":   authorization.WorkspaceID().Hex(),
		"environment_id": response.ID,
		"actor_id":       authorization.UserID().Hex(),
	}).Info("Environment updated")
	c.JSON(http.StatusOK, response)
}

func (h *EnvironmentApplicationHandler) CreateApplication(c *gin.Context) {
	authorization, ok := workspaceRequestAuthorization(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(c)
	if !ok {
		return
	}
	var request models.CreateApplicationRequest
	if !decodeRequest(c, &request) {
		return
	}
	environmentID, err := primitive.ObjectIDFromHex(request.EnvironmentID)
	if err != nil {
		api.WriteProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", "environmentId is not a valid resource identifier.")
		return
	}
	response, replayed, err := h.application.CreateApplication(
		c.Request.Context(), authorization.UserID(), authorization.WorkspaceID(), environmentID, request, idempotencyKey,
	)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	logrus.WithFields(logrus.Fields{
		"workspace_id":   authorization.WorkspaceID().Hex(),
		"environment_id": response.EnvironmentID,
		"application_id": response.ID,
		"actor_id":       authorization.UserID().Hex(),
		"replayed":       replayed,
	}).Info("Application created")
	c.JSON(http.StatusCreated, response)
}

func (h *EnvironmentApplicationHandler) ListApplications(c *gin.Context) {
	authorization, ok := workspaceRequestAuthorization(c)
	if !ok {
		return
	}
	limit, cursor, ok := pagination(c)
	if !ok {
		return
	}
	environmentID, ok := optionalResourceIDQuery(c, "environmentId")
	if !ok {
		return
	}
	includeArchived := false
	if rawValue := c.Query("includeArchived"); rawValue != "" {
		parsed, err := strconv.ParseBool(rawValue)
		if err != nil {
			api.WriteProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", "includeArchived must be true or false.")
			return
		}
		includeArchived = parsed
	}
	response, err := h.application.ListApplications(
		c.Request.Context(), authorization.WorkspaceID(), environmentID, includeArchived, limit, cursor,
	)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *EnvironmentApplicationHandler) GetApplication(c *gin.Context) {
	authorization, applicationID, ok := domainResourceAuthorization(c, "applicationId")
	if !ok {
		return
	}
	response, err := h.application.GetApplication(c.Request.Context(), authorization.WorkspaceID(), applicationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *EnvironmentApplicationHandler) UpdateApplication(c *gin.Context) {
	authorization, applicationID, ok := domainResourceAuthorization(c, "applicationId")
	if !ok {
		return
	}
	var request models.UpdateApplicationRequest
	if !decodeRequest(c, &request) {
		return
	}
	response, err := h.application.UpdateApplication(c.Request.Context(), authorization.WorkspaceID(), applicationID, request)
	if err != nil {
		h.writeError(c, err)
		return
	}
	logrus.WithFields(logrus.Fields{
		"workspace_id":   authorization.WorkspaceID().Hex(),
		"application_id": response.ID,
		"actor_id":       authorization.UserID().Hex(),
	}).Info("Application updated")
	c.JSON(http.StatusOK, response)
}

func (h *EnvironmentApplicationHandler) ArchiveApplication(c *gin.Context) {
	authorization, applicationID, ok := domainResourceAuthorization(c, "applicationId")
	if !ok {
		return
	}
	response, err := h.application.ArchiveApplication(c.Request.Context(), authorization.WorkspaceID(), applicationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	logrus.WithFields(logrus.Fields{
		"workspace_id":   authorization.WorkspaceID().Hex(),
		"application_id": response.ID,
		"actor_id":       authorization.UserID().Hex(),
	}).Info("Application archived")
	c.JSON(http.StatusOK, response)
}

func (h *EnvironmentApplicationHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidEnvironmentData), errors.Is(err, services.ErrInvalidApplicationData):
		detail := invalidDomainDetail(err)
		api.WriteProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", detail)
	case errors.Is(err, services.ErrDomainIdempotencyConflict):
		api.WriteProblem(c, http.StatusConflict, "idempotency_conflict", "Idempotency conflict", "The idempotency key was already used with a different request.")
	case errors.Is(err, services.ErrEnvironmentNameConflict):
		api.WriteProblem(c, http.StatusConflict, "environment_name_conflict", "Environment name conflict", "An environment with the same normalized name already exists in this workspace.")
	case errors.Is(err, services.ErrEnvironmentNotFound), errors.Is(err, services.ErrApplicationNotFound):
		api.WriteProblem(c, http.StatusNotFound, "resource_not_found", "Resource not found", "The requested resource was not found.")
	default:
		logrus.WithError(err).WithField("request_id", api.RequestID(c)).Error("Environment or application request failed")
		api.WriteProblem(c, http.StatusInternalServerError, "internal_error", "Internal server error", "The request could not be completed.")
	}
}

func requiredIdempotencyKey(c *gin.Context) (string, bool) {
	key := c.GetHeader("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(key) {
		api.WriteProblem(c, http.StatusBadRequest, "invalid_idempotency_key", "Invalid idempotency key", "Idempotency-Key must contain 8 to 128 safe characters.")
		return "", false
	}
	return key, true
}

func domainResourceAuthorization(c *gin.Context, parameter string) (middleware.WorkspaceAuthorization, primitive.ObjectID, bool) {
	authorization, ok := workspaceRequestAuthorization(c)
	if !ok {
		return middleware.WorkspaceAuthorization{}, primitive.NilObjectID, false
	}
	resourceID, err := primitive.ObjectIDFromHex(c.Param(parameter))
	if err != nil {
		api.WriteProblem(c, http.StatusNotFound, "resource_not_found", "Resource not found", "The requested resource was not found.")
		return middleware.WorkspaceAuthorization{}, primitive.NilObjectID, false
	}
	return authorization, resourceID, true
}

func optionalResourceIDQuery(c *gin.Context, parameter string) (*primitive.ObjectID, bool) {
	value := c.Query(parameter)
	if value == "" {
		return nil, true
	}
	resourceID, err := primitive.ObjectIDFromHex(value)
	if err != nil {
		api.WriteProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", parameter+" is not a valid resource identifier.")
		return nil, false
	}
	return &resourceID, true
}

func invalidDomainDetail(err error) string {
	for _, sentinel := range []error{services.ErrInvalidEnvironmentData, services.ErrInvalidApplicationData} {
		prefix := sentinel.Error() + ": "
		if strings.HasPrefix(err.Error(), prefix) {
			return strings.TrimPrefix(err.Error(), prefix)
		}
	}
	return "The request is invalid."
}
