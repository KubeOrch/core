package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/pkg/api"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ArtifactReleaseApplication interface {
	CreateArtifact(context.Context, primitive.ObjectID, primitive.ObjectID, models.CreateArtifactRequest, string) (models.ArtifactResponse, bool, error)
	ListArtifacts(context.Context, primitive.ObjectID, int, string) (models.ArtifactListResponse, error)
	GetArtifact(context.Context, primitive.ObjectID, primitive.ObjectID) (models.ArtifactResponse, error)
	CreateRelease(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreateReleaseRequest, string) (models.ReleaseResponse, bool, error)
	ListReleases(context.Context, primitive.ObjectID, primitive.ObjectID, int, string) (models.ReleaseListResponse, error)
	GetRelease(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID) (models.ReleaseResponse, error)
}

type ArtifactReleaseHandler struct {
	application ArtifactReleaseApplication
}

func NewArtifactReleaseHandler(application ArtifactReleaseApplication) *ArtifactReleaseHandler {
	return &ArtifactReleaseHandler{application: application}
}

func (h *ArtifactReleaseHandler) CreateArtifact(c *gin.Context) {
	authorization, ok := workspaceRequestAuthorization(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(c)
	if !ok {
		return
	}
	var request models.CreateArtifactRequest
	if !decodeRequest(c, &request) {
		return
	}
	response, replayed, err := h.application.CreateArtifact(
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
		"workspace_id": authorization.WorkspaceID().Hex(),
		"artifact_id":  response.ID,
		"actor_id":     authorization.UserID().Hex(),
		"replayed":     replayed,
	}).Info("Artifact registered")
	c.JSON(http.StatusCreated, response)
}

func (h *ArtifactReleaseHandler) ListArtifacts(c *gin.Context) {
	authorization, ok := workspaceRequestAuthorization(c)
	if !ok {
		return
	}
	limit, cursor, ok := pagination(c)
	if !ok {
		return
	}
	response, err := h.application.ListArtifacts(c.Request.Context(), authorization.WorkspaceID(), limit, cursor)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *ArtifactReleaseHandler) GetArtifact(c *gin.Context) {
	authorization, artifactID, ok := domainResourceAuthorization(c, "artifactId")
	if !ok {
		return
	}
	response, err := h.application.GetArtifact(c.Request.Context(), authorization.WorkspaceID(), artifactID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *ArtifactReleaseHandler) CreateRelease(c *gin.Context) {
	authorization, applicationID, ok := domainResourceAuthorization(c, "applicationId")
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(c)
	if !ok {
		return
	}
	var request models.CreateReleaseRequest
	if !decodeRequest(c, &request) {
		return
	}
	response, replayed, err := h.application.CreateRelease(
		c.Request.Context(), authorization.UserID(), authorization.WorkspaceID(), applicationID, request, idempotencyKey,
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
		"application_id": applicationID.Hex(),
		"release_id":     response.ID,
		"actor_id":       authorization.UserID().Hex(),
		"replayed":       replayed,
	}).Info("Release created")
	c.JSON(http.StatusCreated, response)
}

func (h *ArtifactReleaseHandler) ListReleases(c *gin.Context) {
	authorization, applicationID, ok := domainResourceAuthorization(c, "applicationId")
	if !ok {
		return
	}
	limit, cursor, ok := pagination(c)
	if !ok {
		return
	}
	response, err := h.application.ListReleases(
		c.Request.Context(), authorization.WorkspaceID(), applicationID, limit, cursor,
	)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *ArtifactReleaseHandler) GetRelease(c *gin.Context) {
	authorization, applicationID, ok := domainResourceAuthorization(c, "applicationId")
	if !ok {
		return
	}
	releaseID, err := primitive.ObjectIDFromHex(c.Param("releaseId"))
	if err != nil {
		api.WriteProblem(c, http.StatusNotFound, "resource_not_found", "Resource not found", "The requested resource was not found.")
		return
	}
	response, err := h.application.GetRelease(c.Request.Context(), authorization.WorkspaceID(), applicationID, releaseID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *ArtifactReleaseHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidArtifactData), errors.Is(err, services.ErrInvalidReleaseData):
		api.WriteProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", artifactReleaseInvalidDetail(err))
	case errors.Is(err, services.ErrDomainIdempotencyConflict):
		api.WriteProblem(c, http.StatusConflict, "idempotency_conflict", "Idempotency conflict", "The idempotency key was already used with a different request.")
	case errors.Is(err, services.ErrArtifactNotFound), errors.Is(err, services.ErrReleaseNotFound), errors.Is(err, services.ErrApplicationNotFound):
		api.WriteProblem(c, http.StatusNotFound, "resource_not_found", "Resource not found", "The requested resource was not found.")
	default:
		logrus.WithError(err).WithField("request_id", api.RequestID(c)).Error("Artifact or release request failed")
		api.WriteProblem(c, http.StatusInternalServerError, "internal_error", "Internal server error", "The request could not be completed.")
	}
}

func artifactReleaseInvalidDetail(err error) string {
	for _, sentinel := range []error{services.ErrInvalidArtifactData, services.ErrInvalidReleaseData} {
		prefix := sentinel.Error() + ": "
		if strings.HasPrefix(err.Error(), prefix) {
			return strings.TrimPrefix(err.Error(), prefix)
		}
	}
	return "The request is invalid."
}
