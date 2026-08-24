package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	ErrPlanNotFound              = errors.New("plan not found")
	ErrPlanReferenceNotFound     = errors.New("plan reference not found")
	ErrInvalidPlanData           = errors.New("invalid plan data")
	ErrPlanApprovalAlreadyExists = errors.New("plan approval already requested")
	ErrPlanApprovalRequired      = errors.New("plan approval request required")
	ErrPlanDecisionTerminal      = errors.New("plan decision is terminal")
	ErrPlanStateConflict         = errors.New("plan state conflict")
	ErrPlanDecisionForbidden     = errors.New("plan decision forbidden")
	ErrPlanSelfApprovalForbidden = errors.New("plan self-approval forbidden")
)

var planRevisionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+\-]{0,127}$`)

type PlanApprovalStore interface {
	CreatePlan(context.Context, *models.Plan) error
	GetPlanByCreationKey(context.Context, primitive.ObjectID, primitive.ObjectID, string) (*models.Plan, error)
	ListPlans(context.Context, primitive.ObjectID, int, string) ([]models.Plan, string, error)
	GetPlan(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Plan, error)
	GetApplication(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Application, error)
	GetEnvironment(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Environment, error)
	RequestApproval(context.Context, primitive.ObjectID, primitive.ObjectID, models.PlanApprovalRequest, time.Time) (*models.Plan, bool, error)
	RecordDecision(context.Context, primitive.ObjectID, primitive.ObjectID, models.PlanDecision, models.PlanStatus, time.Time) (*models.Plan, bool, error)
}

type PlanApprovalService struct {
	store            PlanApprovalStore
	now              func() time.Time
	newCorrelationID func() string
}

func NewPlanApprovalService(store PlanApprovalStore) *PlanApprovalService {
	return &PlanApprovalService{
		store:            store,
		now:              func() time.Time { return time.Now().UTC() },
		newCorrelationID: uuid.NewString,
	}
}

func (s *PlanApprovalService) CreatePlan(
	ctx context.Context,
	actorID, workspaceID, applicationID, environmentID primitive.ObjectID,
	request models.CreatePlanRequest,
	idempotencyKey string,
) (models.PlanResponse, bool, error) {
	normalized, err := validateCreatePlanRequest(request)
	if err != nil {
		return models.PlanResponse{}, false, err
	}
	normalized.ApplicationID = applicationID.Hex()
	normalized.EnvironmentID = environmentID.Hex()
	requestHash, err := hashPlanPayload(normalized)
	if err != nil {
		return models.PlanResponse{}, false, err
	}
	existing, err := s.store.GetPlanByCreationKey(ctx, workspaceID, actorID, idempotencyKey)
	if err == nil {
		if existing.CreationHash != requestHash {
			return models.PlanResponse{}, false, ErrDomainIdempotencyConflict
		}
		return planResponse(planCreationReplay(*existing)), true, nil
	}
	if !errors.Is(err, repositories.ErrPlanNotFound) {
		return models.PlanResponse{}, false, mapPlanRepositoryError(err)
	}

	application, err := s.store.GetApplication(ctx, workspaceID, applicationID)
	if err != nil {
		return models.PlanResponse{}, false, mapPlanRepositoryError(err)
	}
	if application.EnvironmentID != environmentID {
		return models.PlanResponse{}, false, ErrPlanReferenceNotFound
	}
	if application.ArchivedAt != nil {
		return models.PlanResponse{}, false, fmt.Errorf("%w: application is archived", ErrInvalidPlanData)
	}
	if _, err := s.store.GetEnvironment(ctx, workspaceID, environmentID); err != nil {
		return models.PlanResponse{}, false, mapPlanRepositoryError(err)
	}

	now := s.now()
	plan := &models.Plan{
		ID:                 primitive.NewObjectIDFromTimestamp(now),
		WorkspaceID:        workspaceID,
		ApplicationID:      applicationID,
		EnvironmentID:      environmentID,
		DesiredRevision:    normalized.DesiredRevision,
		Source:             normalized.Source,
		DiffSummary:        normalized.DiffSummary,
		EvidenceSummary:    normalized.EvidenceSummary,
		Validation:         normalized.Validation,
		Cost:               normalized.Cost,
		Policy:             normalized.Policy,
		Status:             models.PlanStatusProposed,
		CreatedBy:          actorID,
		AuditCorrelationID: s.newCorrelationID(),
		CreationKey:        idempotencyKey,
		CreationHash:       requestHash,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.store.CreatePlan(ctx, plan); err != nil {
		if !errors.Is(err, repositories.ErrDuplicatePlanCreationKey) {
			return models.PlanResponse{}, false, mapPlanRepositoryError(err)
		}
		existing, getErr := s.store.GetPlanByCreationKey(ctx, workspaceID, actorID, idempotencyKey)
		if getErr != nil {
			return models.PlanResponse{}, false, mapPlanRepositoryError(getErr)
		}
		if existing.CreationHash != requestHash {
			return models.PlanResponse{}, false, ErrDomainIdempotencyConflict
		}
		return planResponse(planCreationReplay(*existing)), true, nil
	}
	return planResponse(*plan), false, nil
}

func (s *PlanApprovalService) ListPlans(
	ctx context.Context,
	workspaceID primitive.ObjectID,
	limit int,
	cursor string,
) (models.PlanListResponse, error) {
	plans, nextCursor, err := s.store.ListPlans(ctx, workspaceID, limit, cursor)
	if err != nil {
		if errors.Is(err, repositories.ErrInvalidCursor) {
			return models.PlanListResponse{}, fmt.Errorf("%w: invalid pagination cursor", ErrInvalidPlanData)
		}
		return models.PlanListResponse{}, mapPlanRepositoryError(err)
	}
	items := make([]models.PlanResponse, 0, len(plans))
	for _, plan := range plans {
		items = append(items, planResponse(plan))
	}
	return models.PlanListResponse{Items: items, PageInfo: pageInfo(nextCursor)}, nil
}

func (s *PlanApprovalService) GetPlan(
	ctx context.Context,
	workspaceID, planID primitive.ObjectID,
) (models.PlanResponse, error) {
	plan, err := s.store.GetPlan(ctx, workspaceID, planID)
	if err != nil {
		return models.PlanResponse{}, mapPlanRepositoryError(err)
	}
	return planResponse(*plan), nil
}

func (s *PlanApprovalService) RequestApproval(
	ctx context.Context,
	actorID, workspaceID, planID primitive.ObjectID,
	request models.CreateApprovalRequest,
	idempotencyKey string,
) (models.PlanResponse, bool, error) {
	reason, err := validateOptionalPlanText(request.Reason, 1000, "reason")
	if err != nil {
		return models.PlanResponse{}, false, err
	}
	requestHash, err := hashPlanPayload(struct {
		Reason string `json:"reason"`
	}{Reason: reason})
	if err != nil {
		return models.PlanResponse{}, false, err
	}
	now := s.now()
	approvalRequest := models.PlanApprovalRequest{
		RequestedBy:        actorID,
		Reason:             reason,
		AuditCorrelationID: s.newCorrelationID(),
		IdempotencyKey:     idempotencyKey,
		RequestHash:        requestHash,
		RequestedAt:        now,
	}
	plan, replayed, err := s.store.RequestApproval(ctx, workspaceID, planID, approvalRequest, now)
	if err != nil {
		return models.PlanResponse{}, false, mapPlanRepositoryError(err)
	}
	if replayed {
		return planResponse(approvalRequestReplay(*plan)), true, nil
	}
	return planResponse(*plan), false, nil
}

func (s *PlanApprovalService) DecidePlan(
	ctx context.Context,
	actorID primitive.ObjectID,
	actorRole models.MembershipRole,
	workspaceID, planID primitive.ObjectID,
	request models.CreatePlanDecisionRequest,
	idempotencyKey string,
) (models.PlanResponse, bool, error) {
	if actorRole != models.MembershipRoleOwner && actorRole != models.MembershipRoleAdmin {
		return models.PlanResponse{}, false, ErrPlanDecisionForbidden
	}
	decision, reason, err := validatePlanDecisionRequest(request)
	if err != nil {
		return models.PlanResponse{}, false, err
	}
	requestHash, err := hashPlanPayload(struct {
		Decision models.PlanDecisionType `json:"decision"`
		Reason   string                  `json:"reason"`
	}{Decision: decision, Reason: reason})
	if err != nil {
		return models.PlanResponse{}, false, err
	}

	existing, err := s.store.GetPlan(ctx, workspaceID, planID)
	if err != nil {
		return models.PlanResponse{}, false, mapPlanRepositoryError(err)
	}
	if existing.Decision != nil {
		if existing.Decision.DecidedBy == actorID &&
			existing.Decision.IdempotencyKey == idempotencyKey &&
			existing.Decision.RequestHash == requestHash {
			return planResponse(*existing), true, nil
		}
		return models.PlanResponse{}, false, ErrPlanDecisionTerminal
	}
	if existing.Status == models.PlanStatusProposed || existing.ApprovalRequest == nil {
		return models.PlanResponse{}, false, ErrPlanApprovalRequired
	}
	if existing.Status != models.PlanStatusApprovalRequested {
		return models.PlanResponse{}, false, ErrPlanStateConflict
	}
	if decision == models.PlanDecisionApprove && existing.Policy.SelfApprovalForbidden && existing.CreatedBy == actorID {
		return models.PlanResponse{}, false, ErrPlanSelfApprovalForbidden
	}

	now := s.now()
	recordedDecision := models.PlanDecision{
		Decision:           decision,
		Reason:             reason,
		DecidedBy:          actorID,
		AuditCorrelationID: s.newCorrelationID(),
		IdempotencyKey:     idempotencyKey,
		RequestHash:        requestHash,
		DecidedAt:          now,
	}
	status := models.PlanStatusApproved
	if decision == models.PlanDecisionReject {
		status = models.PlanStatusRejected
	}
	plan, replayed, err := s.store.RecordDecision(ctx, workspaceID, planID, recordedDecision, status, now)
	if err != nil {
		return models.PlanResponse{}, false, mapPlanRepositoryError(err)
	}
	return planResponse(*plan), replayed, nil
}

func validateCreatePlanRequest(request models.CreatePlanRequest) (models.CreatePlanRequest, error) {
	request.DesiredRevision = strings.TrimSpace(request.DesiredRevision)
	if !planRevisionPattern.MatchString(request.DesiredRevision) {
		return models.CreatePlanRequest{}, fmt.Errorf("%w: desiredRevision must contain 1 to 128 safe characters", ErrInvalidPlanData)
	}
	if !request.Source.IsValid() {
		return models.CreatePlanRequest{}, fmt.Errorf("%w: source is not supported", ErrInvalidPlanData)
	}
	var err error
	request.DiffSummary, err = validateRequiredPlanText(request.DiffSummary, 4000, "diffSummary")
	if err != nil {
		return models.CreatePlanRequest{}, err
	}
	request.EvidenceSummary, err = validateOptionalPlanText(request.EvidenceSummary, 2000, "evidenceSummary")
	if err != nil {
		return models.CreatePlanRequest{}, err
	}
	request.Validation, err = validatePlanResultReference(request.Validation, "validation")
	if err != nil {
		return models.CreatePlanRequest{}, err
	}
	request.Cost, err = validatePlanCostReference(request.Cost)
	if err != nil {
		return models.CreatePlanRequest{}, err
	}
	request.Policy, err = validatePlanPolicyResult(request.Policy)
	if err != nil {
		return models.CreatePlanRequest{}, err
	}
	return request, nil
}

func validatePlanResultReference(reference models.PlanResultReference, field string) (models.PlanResultReference, error) {
	if !reference.Status.IsValid() {
		return models.PlanResultReference{}, fmt.Errorf("%w: %s.status is not supported", ErrInvalidPlanData, field)
	}
	var err error
	reference.Summary, err = validateOptionalPlanText(reference.Summary, 1000, field+".summary")
	if err != nil {
		return models.PlanResultReference{}, err
	}
	reference.Reference, err = validatePlanReference(reference.Reference, field+".reference")
	if err != nil {
		return models.PlanResultReference{}, err
	}
	return reference, nil
}

func validatePlanCostReference(reference models.PlanCostReference) (models.PlanCostReference, error) {
	if !reference.Status.IsValid() {
		return models.PlanCostReference{}, fmt.Errorf("%w: cost.status is not supported", ErrInvalidPlanData)
	}
	var err error
	reference.Summary, err = validateOptionalPlanText(reference.Summary, 1000, "cost.summary")
	if err != nil {
		return models.PlanCostReference{}, err
	}
	reference.Reference, err = validatePlanReference(reference.Reference, "cost.reference")
	if err != nil {
		return models.PlanCostReference{}, err
	}
	return reference, nil
}

func validatePlanPolicyResult(result models.PlanPolicyResult) (models.PlanPolicyResult, error) {
	validated, err := validatePlanResultReference(models.PlanResultReference{
		Status: result.Status, Summary: result.Summary, Reference: result.Reference,
	}, "policy")
	if err != nil {
		return models.PlanPolicyResult{}, err
	}
	result.Status = validated.Status
	result.Summary = validated.Summary
	result.Reference = validated.Reference
	return result, nil
}

func validatePlanReference(reference, field string) (string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return "", nil
	}
	if !utf8.ValidString(reference) || len(reference) > 2048 {
		return "", fmt.Errorf("%w: %s must be a valid reference of at most 2048 bytes", ErrInvalidPlanData, field)
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: %s must be a credential-free HTTPS URL without a query or fragment", ErrInvalidPlanData, field)
	}
	return parsed.String(), nil
}

func validatePlanDecisionRequest(request models.CreatePlanDecisionRequest) (models.PlanDecisionType, string, error) {
	if !request.Decision.IsValid() {
		return "", "", fmt.Errorf("%w: decision must be approve or reject", ErrInvalidPlanData)
	}
	reason, err := validateRequiredPlanText(request.Reason, 1000, "reason")
	if err != nil {
		return "", "", err
	}
	return request.Decision, reason, nil
}

func validateRequiredPlanText(value string, limit int, field string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > limit {
		return "", fmt.Errorf("%w: %s must contain 1 to %d characters", ErrInvalidPlanData, field, limit)
	}
	return value, nil
}

func validateOptionalPlanText(value string, limit int, field string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return "", fmt.Errorf("%w: %s must contain at most %d characters", ErrInvalidPlanData, field, limit)
	}
	return value, nil
}

func hashPlanPayload(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("hash plan request: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func planResponse(plan models.Plan) models.PlanResponse {
	response := models.PlanResponse{
		ID:                 plan.ID.Hex(),
		WorkspaceID:        plan.WorkspaceID.Hex(),
		ApplicationID:      plan.ApplicationID.Hex(),
		EnvironmentID:      plan.EnvironmentID.Hex(),
		DesiredRevision:    plan.DesiredRevision,
		Source:             plan.Source,
		DiffSummary:        plan.DiffSummary,
		EvidenceSummary:    plan.EvidenceSummary,
		Validation:         plan.Validation,
		Cost:               plan.Cost,
		Policy:             plan.Policy,
		Status:             plan.Status,
		CreatedBy:          plan.CreatedBy.Hex(),
		AuditCorrelationID: plan.AuditCorrelationID,
		CreatedAt:          plan.CreatedAt,
		UpdatedAt:          plan.UpdatedAt,
	}
	if plan.ApprovalRequest != nil {
		response.ApprovalRequest = &models.PlanApprovalRequestResponse{
			RequestedBy:        plan.ApprovalRequest.RequestedBy.Hex(),
			Reason:             plan.ApprovalRequest.Reason,
			AuditCorrelationID: plan.ApprovalRequest.AuditCorrelationID,
			RequestedAt:        plan.ApprovalRequest.RequestedAt,
		}
	}
	if plan.Decision != nil {
		response.Decision = &models.PlanDecisionResponse{
			Decision:           plan.Decision.Decision,
			Reason:             plan.Decision.Reason,
			DecidedBy:          plan.Decision.DecidedBy.Hex(),
			AuditCorrelationID: plan.Decision.AuditCorrelationID,
			DecidedAt:          plan.Decision.DecidedAt,
		}
	}
	return response
}

func planCreationReplay(plan models.Plan) models.Plan {
	plan.Status = models.PlanStatusProposed
	plan.ApprovalRequest = nil
	plan.Decision = nil
	plan.UpdatedAt = plan.CreatedAt
	return plan
}

func approvalRequestReplay(plan models.Plan) models.Plan {
	plan.Status = models.PlanStatusApprovalRequested
	plan.Decision = nil
	if plan.ApprovalRequest != nil {
		plan.UpdatedAt = plan.ApprovalRequest.RequestedAt
	}
	return plan
}

func mapPlanRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repositories.ErrPlanNotFound):
		return ErrPlanNotFound
	case errors.Is(err, repositories.ErrApplicationNotFound), errors.Is(err, repositories.ErrEnvironmentNotFound):
		return ErrPlanReferenceNotFound
	case errors.Is(err, repositories.ErrPlanApprovalAlreadyRequested):
		return ErrPlanApprovalAlreadyExists
	case errors.Is(err, repositories.ErrPlanApprovalRequired):
		return ErrPlanApprovalRequired
	case errors.Is(err, repositories.ErrPlanDecisionTerminal):
		return ErrPlanDecisionTerminal
	case errors.Is(err, repositories.ErrPlanStateConflict):
		return ErrPlanStateConflict
	default:
		return err
	}
}
