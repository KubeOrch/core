package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type PlanSource string

const (
	PlanSourceManual       PlanSource = "manual"
	PlanSourceExternalCI   PlanSource = "external-ci"
	PlanSourceAI           PlanSource = "ai"
	PlanSourceManagedBuild PlanSource = "managed-build"
)

type PlanStatus string

const (
	PlanStatusProposed          PlanStatus = "proposed"
	PlanStatusApprovalRequested PlanStatus = "approval-requested"
	PlanStatusApproved          PlanStatus = "approved"
	PlanStatusRejected          PlanStatus = "rejected"
)

type PlanCheckStatus string

const (
	PlanCheckStatusUnknown PlanCheckStatus = "unknown"
	PlanCheckStatusPassed  PlanCheckStatus = "passed"
	PlanCheckStatusWarning PlanCheckStatus = "warning"
	PlanCheckStatusFailed  PlanCheckStatus = "failed"
)

type PlanCostStatus string

const (
	PlanCostStatusUnknown     PlanCostStatus = "unknown"
	PlanCostStatusAvailable   PlanCostStatus = "available"
	PlanCostStatusUnavailable PlanCostStatus = "unavailable"
)

type PlanDecisionType string

const (
	PlanDecisionApprove PlanDecisionType = "approve"
	PlanDecisionReject  PlanDecisionType = "reject"
)

type PlanResultReference struct {
	Status    PlanCheckStatus `bson:"status" json:"status"`
	Summary   string          `bson:"summary,omitempty" json:"summary,omitempty"`
	Reference string          `bson:"reference,omitempty" json:"reference,omitempty"`
}

type PlanCostReference struct {
	Status    PlanCostStatus `bson:"status" json:"status"`
	Summary   string         `bson:"summary,omitempty" json:"summary,omitempty"`
	Reference string         `bson:"reference,omitempty" json:"reference,omitempty"`
}

type PlanPolicyResult struct {
	Status                PlanCheckStatus `bson:"status" json:"status"`
	Summary               string          `bson:"summary,omitempty" json:"summary,omitempty"`
	Reference             string          `bson:"reference,omitempty" json:"reference,omitempty"`
	SelfApprovalForbidden bool            `bson:"self_approval_forbidden" json:"selfApprovalForbidden"`
}

type PlanApprovalRequest struct {
	RequestedBy        primitive.ObjectID `bson:"requested_by" json:"-"`
	Reason             string             `bson:"reason,omitempty" json:"-"`
	AuditCorrelationID string             `bson:"audit_correlation_id" json:"-"`
	IdempotencyKey     string             `bson:"idempotency_key" json:"-"`
	RequestHash        string             `bson:"request_hash" json:"-"`
	RequestedAt        time.Time          `bson:"requested_at" json:"-"`
}

type PlanDecision struct {
	Decision           PlanDecisionType   `bson:"decision" json:"-"`
	Reason             string             `bson:"reason" json:"-"`
	DecidedBy          primitive.ObjectID `bson:"decided_by" json:"-"`
	AuditCorrelationID string             `bson:"audit_correlation_id" json:"-"`
	IdempotencyKey     string             `bson:"idempotency_key" json:"-"`
	RequestHash        string             `bson:"request_hash" json:"-"`
	DecidedAt          time.Time          `bson:"decided_at" json:"-"`
}

type Plan struct {
	ID                 primitive.ObjectID   `bson:"_id" json:"-"`
	WorkspaceID        primitive.ObjectID   `bson:"workspace_id" json:"-"`
	ApplicationID      primitive.ObjectID   `bson:"application_id" json:"-"`
	EnvironmentID      primitive.ObjectID   `bson:"environment_id" json:"-"`
	DesiredRevision    string               `bson:"desired_revision" json:"-"`
	Source             PlanSource           `bson:"source" json:"-"`
	DiffSummary        string               `bson:"diff_summary" json:"-"`
	EvidenceSummary    string               `bson:"evidence_summary" json:"-"`
	Validation         PlanResultReference  `bson:"validation" json:"-"`
	Cost               PlanCostReference    `bson:"cost" json:"-"`
	Policy             PlanPolicyResult     `bson:"policy" json:"-"`
	Status             PlanStatus           `bson:"status" json:"-"`
	CreatedBy          primitive.ObjectID   `bson:"created_by" json:"-"`
	AuditCorrelationID string               `bson:"audit_correlation_id" json:"-"`
	CreationKey        string               `bson:"creation_key" json:"-"`
	CreationHash       string               `bson:"creation_hash" json:"-"`
	ApprovalRequest    *PlanApprovalRequest `bson:"approval_request,omitempty" json:"-"`
	Decision           *PlanDecision        `bson:"decision,omitempty" json:"-"`
	CreatedAt          time.Time            `bson:"created_at" json:"-"`
	UpdatedAt          time.Time            `bson:"updated_at" json:"-"`
}

type CreatePlanRequest struct {
	ApplicationID   string              `json:"applicationId"`
	EnvironmentID   string              `json:"environmentId"`
	DesiredRevision string              `json:"desiredRevision"`
	Source          PlanSource          `json:"source"`
	DiffSummary     string              `json:"diffSummary"`
	EvidenceSummary string              `json:"evidenceSummary,omitempty"`
	Validation      PlanResultReference `json:"validation"`
	Cost            PlanCostReference   `json:"cost"`
	Policy          PlanPolicyResult    `json:"policy"`
}

type CreateApprovalRequest struct {
	Reason string `json:"reason,omitempty"`
}

type CreatePlanDecisionRequest struct {
	Decision PlanDecisionType `json:"decision"`
	Reason   string           `json:"reason"`
}

type PlanApprovalRequestResponse struct {
	RequestedBy        string    `json:"requestedBy"`
	Reason             string    `json:"reason,omitempty"`
	AuditCorrelationID string    `json:"auditCorrelationId"`
	RequestedAt        time.Time `json:"requestedAt"`
}

type PlanDecisionResponse struct {
	Decision           PlanDecisionType `json:"decision"`
	Reason             string           `json:"reason"`
	DecidedBy          string           `json:"decidedBy"`
	AuditCorrelationID string           `json:"auditCorrelationId"`
	DecidedAt          time.Time        `json:"decidedAt"`
}

type PlanResponse struct {
	ID                 string                       `json:"id"`
	WorkspaceID        string                       `json:"workspaceId"`
	ApplicationID      string                       `json:"applicationId"`
	EnvironmentID      string                       `json:"environmentId"`
	DesiredRevision    string                       `json:"desiredRevision"`
	Source             PlanSource                   `json:"source"`
	DiffSummary        string                       `json:"diffSummary"`
	EvidenceSummary    string                       `json:"evidenceSummary,omitempty"`
	Validation         PlanResultReference          `json:"validation"`
	Cost               PlanCostReference            `json:"cost"`
	Policy             PlanPolicyResult             `json:"policy"`
	Status             PlanStatus                   `json:"status"`
	CreatedBy          string                       `json:"createdBy"`
	AuditCorrelationID string                       `json:"auditCorrelationId"`
	ApprovalRequest    *PlanApprovalRequestResponse `json:"approvalRequest,omitempty"`
	Decision           *PlanDecisionResponse        `json:"decision,omitempty"`
	CreatedAt          time.Time                    `json:"createdAt"`
	UpdatedAt          time.Time                    `json:"updatedAt"`
}

type PlanListResponse struct {
	Items    []PlanResponse `json:"items"`
	PageInfo PageInfo       `json:"pageInfo"`
}

func (s PlanSource) IsValid() bool {
	switch s {
	case PlanSourceManual, PlanSourceExternalCI, PlanSourceAI, PlanSourceManagedBuild:
		return true
	default:
		return false
	}
}

func (s PlanCheckStatus) IsValid() bool {
	switch s {
	case PlanCheckStatusUnknown, PlanCheckStatusPassed, PlanCheckStatusWarning, PlanCheckStatusFailed:
		return true
	default:
		return false
	}
}

func (s PlanCostStatus) IsValid() bool {
	switch s {
	case PlanCostStatusUnknown, PlanCostStatusAvailable, PlanCostStatusUnavailable:
		return true
	default:
		return false
	}
}

func (d PlanDecisionType) IsValid() bool {
	return d == PlanDecisionApprove || d == PlanDecisionReject
}
