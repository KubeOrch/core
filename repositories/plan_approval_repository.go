package repositories

import (
	"context"
	"errors"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ErrPlanNotFound                 = errors.New("plan not found")
	ErrDuplicatePlanCreationKey     = errors.New("plan creation key already exists")
	ErrPlanApprovalAlreadyRequested = errors.New("plan approval already requested")
	ErrPlanApprovalRequired         = errors.New("plan approval request required")
	ErrPlanDecisionTerminal         = errors.New("plan decision is terminal")
	ErrPlanStateConflict            = errors.New("plan state conflict")
)

const planCursorKind byte = 3

type PlanApprovalRepository struct {
	plans        *mongo.Collection
	applications *mongo.Collection
	environments *mongo.Collection
}

func NewPlanApprovalRepository() *PlanApprovalRepository {
	return &PlanApprovalRepository{
		plans:        database.PlanColl,
		applications: database.ApplicationColl,
		environments: database.EnvironmentColl,
	}
}

func (r *PlanApprovalRepository) CreatePlan(ctx context.Context, plan *models.Plan) error {
	_, err := r.plans.InsertOne(ctx, plan)
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicatePlanCreationKey
	}
	return err
}

func (r *PlanApprovalRepository) GetPlanByCreationKey(
	ctx context.Context,
	workspaceID, actorID primitive.ObjectID,
	key string,
) (*models.Plan, error) {
	var plan models.Plan
	err := r.plans.FindOne(ctx, bson.M{
		"workspace_id": workspaceID,
		"created_by":   actorID,
		"creation_key": key,
	}).Decode(&plan)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrPlanNotFound
	}
	return &plan, err
}

func (r *PlanApprovalRepository) ListPlans(
	ctx context.Context,
	workspaceID primitive.ObjectID,
	limit int,
	cursor string,
) ([]models.Plan, string, error) {
	filter := bson.M{"workspace_id": workspaceID}
	if cursor != "" {
		createdAt, planID, err := decodeDomainCursor(cursor, planCursorKind, workspaceID, [8]byte{})
		if err != nil {
			return nil, "", err
		}
		filter["$or"] = descendingCursorFilter(createdAt, planID)
	}

	result, err := r.plans.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(limit+1)))
	if err != nil {
		return nil, "", err
	}
	defer closeMongoCursor(ctx, result, "plan")

	var plans []models.Plan
	if err := result.All(ctx, &plans); err != nil {
		return nil, "", err
	}
	nextCursor := ""
	if len(plans) > limit {
		plans = plans[:limit]
		last := plans[len(plans)-1]
		nextCursor = encodeDomainCursor(planCursorKind, workspaceID, [8]byte{}, last.CreatedAt, last.ID)
	}
	return plans, nextCursor, nil
}

func (r *PlanApprovalRepository) GetPlan(
	ctx context.Context,
	workspaceID, planID primitive.ObjectID,
) (*models.Plan, error) {
	var plan models.Plan
	err := r.plans.FindOne(ctx, bson.M{"_id": planID, "workspace_id": workspaceID}).Decode(&plan)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrPlanNotFound
	}
	return &plan, err
}

func (r *PlanApprovalRepository) GetApplication(
	ctx context.Context,
	workspaceID, applicationID primitive.ObjectID,
) (*models.Application, error) {
	var application models.Application
	err := r.applications.FindOne(ctx, bson.M{"_id": applicationID, "workspace_id": workspaceID}).Decode(&application)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrApplicationNotFound
	}
	return &application, err
}

func (r *PlanApprovalRepository) GetEnvironment(
	ctx context.Context,
	workspaceID, environmentID primitive.ObjectID,
) (*models.Environment, error) {
	var environment models.Environment
	err := r.environments.FindOne(ctx, bson.M{"_id": environmentID, "workspace_id": workspaceID}).Decode(&environment)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrEnvironmentNotFound
	}
	return &environment, err
}

func (r *PlanApprovalRepository) RequestApproval(
	ctx context.Context,
	workspaceID, planID primitive.ObjectID,
	request models.PlanApprovalRequest,
	updatedAt time.Time,
) (*models.Plan, bool, error) {
	var plan models.Plan
	err := r.plans.FindOneAndUpdate(
		ctx,
		approvalRequestTransitionFilter(workspaceID, planID),
		bson.M{"$set": bson.M{
			"approval_request": request,
			"status":           models.PlanStatusApprovalRequested,
			"updated_at":       updatedAt,
		}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&plan)
	if err == nil {
		return &plan, false, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, err
	}

	existing, getErr := r.GetPlan(ctx, workspaceID, planID)
	if getErr != nil {
		return nil, false, getErr
	}
	if existing.ApprovalRequest != nil {
		if sameApprovalRequest(*existing.ApprovalRequest, request) {
			return existing, true, nil
		}
		return nil, false, ErrPlanApprovalAlreadyRequested
	}
	return nil, false, ErrPlanStateConflict
}

func (r *PlanApprovalRepository) RecordDecision(
	ctx context.Context,
	workspaceID, planID primitive.ObjectID,
	decision models.PlanDecision,
	status models.PlanStatus,
	updatedAt time.Time,
) (*models.Plan, bool, error) {
	var plan models.Plan
	err := r.plans.FindOneAndUpdate(
		ctx,
		decisionTransitionFilter(workspaceID, planID),
		bson.M{"$set": bson.M{
			"decision":   decision,
			"status":     status,
			"updated_at": updatedAt,
		}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&plan)
	if err == nil {
		return &plan, false, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, err
	}

	existing, getErr := r.GetPlan(ctx, workspaceID, planID)
	if getErr != nil {
		return nil, false, getErr
	}
	if existing.Decision != nil {
		if samePlanDecision(*existing.Decision, decision) {
			return existing, true, nil
		}
		return nil, false, ErrPlanDecisionTerminal
	}
	if existing.Status == models.PlanStatusProposed {
		return nil, false, ErrPlanApprovalRequired
	}
	if existing.Status == models.PlanStatusApproved || existing.Status == models.PlanStatusRejected {
		return nil, false, ErrPlanDecisionTerminal
	}
	return nil, false, ErrPlanStateConflict
}

func approvalRequestTransitionFilter(workspaceID, planID primitive.ObjectID) bson.M {
	return bson.M{
		"_id":              planID,
		"workspace_id":     workspaceID,
		"status":           models.PlanStatusProposed,
		"approval_request": bson.M{"$exists": false},
		"decision":         bson.M{"$exists": false},
	}
}

func decisionTransitionFilter(workspaceID, planID primitive.ObjectID) bson.M {
	return bson.M{
		"_id":          planID,
		"workspace_id": workspaceID,
		"status":       models.PlanStatusApprovalRequested,
		"decision":     bson.M{"$exists": false},
	}
}

func sameApprovalRequest(left, right models.PlanApprovalRequest) bool {
	return left.RequestedBy == right.RequestedBy &&
		left.IdempotencyKey == right.IdempotencyKey &&
		left.RequestHash == right.RequestHash
}

func samePlanDecision(left, right models.PlanDecision) bool {
	return left.DecidedBy == right.DecidedBy &&
		left.IdempotencyKey == right.IdempotencyKey &&
		left.RequestHash == right.RequestHash
}
