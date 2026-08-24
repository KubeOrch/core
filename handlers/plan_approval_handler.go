package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/pkg/api"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var planDecisions = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "kubeorch_plan_decisions_total",
		Help: "Total number of accepted plan decision requests by decision and result.",
	},
	[]string{"decision", "result"},
)

type PlanApproval interface {
	CreatePlan(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreatePlanRequest, string) (models.PlanResponse, bool, error)
	ListPlans(context.Context, primitive.ObjectID, int, string) (models.PlanListResponse, error)
	GetPlan(context.Context, primitive.ObjectID, primitive.ObjectID) (models.PlanResponse, error)
	RequestApproval(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.CreateApprovalRequest, string) (models.PlanResponse, bool, error)
	DecidePlan(context.Context, primitive.ObjectID, models.MembershipRole, primitive.ObjectID, primitive.ObjectID, models.CreatePlanDecisionRequest, string) (models.PlanResponse, bool, error)
}

type PlanApprovalHandler struct {
	application PlanApproval
}

func NewPlanApprovalHandler(application PlanApproval) *PlanApprovalHandler {
	return &PlanApprovalHandler{application: application}
}

func (h *PlanApprovalHandler) CreatePlan(c *gin.Context) {
	authorization, ok := workspaceRequestAuthorization(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(c)
	if !ok {
		return
	}
	var request models.CreatePlanRequest
	if !decodeRequest(c, &request) {
		return
	}
	applicationID, err := primitive.ObjectIDFromHex(request.ApplicationID)
	if err != nil {
		api.WriteProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", "applicationId is not a valid resource identifier.")
		return
	}
	environmentID, err := primitive.ObjectIDFromHex(request.EnvironmentID)
	if err != nil {
		api.WriteProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", "environmentId is not a valid resource identifier.")
		return
	}
	response, replayed, err := h.application.CreatePlan(
		c.Request.Context(), authorization.UserID(), authorization.WorkspaceID(), applicationID, environmentID, request, idempotencyKey,
	)
	if err != nil {
		h.writeError(c, err)
		return
	}
	setPlanReplayHeader(c, replayed)
	logPlanTransition("Plan created", authorization, response, replayed)
	c.JSON(http.StatusCreated, response)
}

func (h *PlanApprovalHandler) ListPlans(c *gin.Context) {
	authorization, ok := workspaceRequestAuthorization(c)
	if !ok {
		return
	}
	limit, cursor, ok := pagination(c)
	if !ok {
		return
	}
	response, err := h.application.ListPlans(c.Request.Context(), authorization.WorkspaceID(), limit, cursor)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *PlanApprovalHandler) GetPlan(c *gin.Context) {
	authorization, planID, ok := domainResourceAuthorization(c, "planId")
	if !ok {
		return
	}
	response, err := h.application.GetPlan(c.Request.Context(), authorization.WorkspaceID(), planID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *PlanApprovalHandler) RequestApproval(c *gin.Context) {
	authorization, planID, ok := domainResourceAuthorization(c, "planId")
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(c)
	if !ok {
		return
	}
	var request models.CreateApprovalRequest
	if !decodeRequest(c, &request) {
		return
	}
	response, replayed, err := h.application.RequestApproval(
		c.Request.Context(), authorization.UserID(), authorization.WorkspaceID(), planID, request, idempotencyKey,
	)
	if err != nil {
		h.writeError(c, err)
		return
	}
	setPlanReplayHeader(c, replayed)
	logPlanTransition("Plan approval requested", authorization, response, replayed)
	c.JSON(http.StatusCreated, response)
}

func (h *PlanApprovalHandler) DecidePlan(c *gin.Context) {
	authorization, planID, ok := domainResourceAuthorization(c, "planId")
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(c)
	if !ok {
		return
	}
	var request models.CreatePlanDecisionRequest
	if !decodeRequest(c, &request) {
		return
	}
	response, replayed, err := h.application.DecidePlan(
		c.Request.Context(), authorization.UserID(), authorization.Role(), authorization.WorkspaceID(), planID, request, idempotencyKey,
	)
	if err != nil {
		h.writeError(c, err)
		return
	}
	setPlanReplayHeader(c, replayed)
	result := "created"
	if replayed {
		result = "replayed"
	}
	planDecisions.WithLabelValues(string(request.Decision), result).Inc()
	logPlanTransition("Plan decision recorded", authorization, response, replayed)
	c.JSON(http.StatusCreated, response)
}

func (h *PlanApprovalHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidPlanData):
		api.WriteProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", invalidPlanDetail(err))
	case errors.Is(err, services.ErrDomainIdempotencyConflict):
		api.WriteProblem(c, http.StatusConflict, "idempotency_conflict", "Idempotency conflict", "The idempotency key was already used with a different request.")
	case errors.Is(err, services.ErrPlanNotFound), errors.Is(err, services.ErrPlanReferenceNotFound):
		api.WriteProblem(c, http.StatusNotFound, "resource_not_found", "Resource not found", "The requested resource was not found.")
	case errors.Is(err, services.ErrPlanApprovalAlreadyExists):
		api.WriteProblem(c, http.StatusConflict, "approval_already_requested", "Approval already requested", "This Plan already has an approval request.")
	case errors.Is(err, services.ErrPlanApprovalRequired):
		api.WriteProblem(c, http.StatusConflict, "approval_request_required", "Approval request required", "Request approval before recording a decision.")
	case errors.Is(err, services.ErrPlanDecisionTerminal):
		api.WriteProblem(c, http.StatusConflict, "plan_decision_terminal", "Plan decision is terminal", "An approved or rejected Plan cannot receive another decision.")
	case errors.Is(err, services.ErrPlanStateConflict):
		api.WriteProblem(c, http.StatusConflict, "plan_state_conflict", "Plan state conflict", "The Plan state changed before this request completed.")
	case errors.Is(err, services.ErrPlanDecisionForbidden):
		api.WriteProblem(c, http.StatusForbidden, "plan_decision_forbidden", "Plan decision forbidden", "Only workspace owners and administrators can approve or reject Plans.")
	case errors.Is(err, services.ErrPlanSelfApprovalForbidden):
		api.WriteProblem(c, http.StatusForbidden, "self_approval_forbidden", "Self-approval forbidden", "Policy requires a different authorized actor to approve this Plan.")
	default:
		logrus.WithError(err).WithField("request_id", api.RequestID(c)).Error("Plan or approval request failed")
		api.WriteProblem(c, http.StatusInternalServerError, "internal_error", "Internal server error", "The request could not be completed.")
	}
}

func setPlanReplayHeader(c *gin.Context, replayed bool) {
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
}

func logPlanTransition(message string, authorization middleware.WorkspaceAuthorization, response models.PlanResponse, replayed bool) {
	fields := logrus.Fields{
		"workspace_id":         authorization.WorkspaceID().Hex(),
		"plan_id":              response.ID,
		"application_id":       response.ApplicationID,
		"environment_id":       response.EnvironmentID,
		"actor_id":             authorization.UserID().Hex(),
		"plan_status":          response.Status,
		"audit_correlation_id": response.AuditCorrelationID,
		"replayed":             replayed,
	}
	if response.Decision != nil {
		fields["decision"] = response.Decision.Decision
		fields["audit_correlation_id"] = response.Decision.AuditCorrelationID
	} else if response.ApprovalRequest != nil {
		fields["audit_correlation_id"] = response.ApprovalRequest.AuditCorrelationID
	}
	logrus.WithFields(fields).Info(message)
}

func invalidPlanDetail(err error) string {
	prefix := services.ErrInvalidPlanData.Error() + ": "
	if strings.HasPrefix(err.Error(), prefix) {
		return strings.TrimPrefix(err.Error(), prefix)
	}
	return "The request is invalid."
}
