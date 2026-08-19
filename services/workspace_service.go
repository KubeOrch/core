package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	ErrWorkspaceNotFound    = errors.New("workspace not found")
	ErrMembershipNotFound   = errors.New("membership not found")
	ErrWorkspaceForbidden   = errors.New("workspace operation forbidden")
	ErrLastWorkspaceOwner   = errors.New("last workspace owner cannot be changed")
	ErrMembershipExists     = errors.New("membership already exists")
	ErrTargetUserNotFound   = errors.New("target user not found")
	ErrIdempotencyConflict  = errors.New("idempotency key reused with different input")
	ErrWorkspaceConflict    = errors.New("workspace changed concurrently")
	ErrInvalidWorkspaceData = errors.New("invalid workspace data")
)

type WorkspaceStore interface {
	Create(context.Context, *models.Workspace) error
	GetByCreationKey(context.Context, primitive.ObjectID, string) (*models.Workspace, error)
	GetForUser(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Workspace, error)
	ListForUser(context.Context, primitive.ObjectID, int, string) ([]models.Workspace, string, error)
	UpdateMetadata(context.Context, primitive.ObjectID, primitive.ObjectID, bson.M) (*models.Workspace, error)
	AddMembership(context.Context, primitive.ObjectID, primitive.ObjectID, models.Membership, bool) (*models.Workspace, error)
	UpdateMembershipRole(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.MembershipRole, models.MembershipRole, bool) (*models.Workspace, error)
	RemoveMembership(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, models.MembershipRole, bool) error
	ListMembershipsForUser(context.Context, primitive.ObjectID, primitive.ObjectID, int, string) ([]models.Membership, string, error)
}

type UserDirectory interface {
	Exists(context.Context, primitive.ObjectID) (bool, error)
}

type WorkspaceService struct {
	store WorkspaceStore
	users UserDirectory
	now   func() time.Time
}

func NewWorkspaceService(store WorkspaceStore, users UserDirectory) *WorkspaceService {
	return &WorkspaceService{
		store: store,
		users: users,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func (s *WorkspaceService) CreateWorkspace(
	ctx context.Context,
	actorID primitive.ObjectID,
	request models.CreateWorkspaceRequest,
	idempotencyKey string,
) (models.WorkspaceResponse, bool, error) {
	name, description, err := validateWorkspaceFields(request.Name, request.Description)
	if err != nil {
		return models.WorkspaceResponse{}, false, err
	}

	requestHash := workspaceRequestHash(name, description)
	now := s.now()
	workspaceID := primitive.NewObjectIDFromTimestamp(now)
	membership := models.Membership{
		ID:          primitive.NewObjectID(),
		WorkspaceID: workspaceID,
		UserID:      actorID,
		Role:        models.MembershipRoleOwner,
		Status:      models.MembershipStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	workspace := &models.Workspace{
		ID:           workspaceID,
		Name:         name,
		Description:  description,
		Memberships:  []models.Membership{membership},
		OwnerCount:   1,
		CreatedBy:    actorID,
		CreationKey:  idempotencyKey,
		CreationHash: requestHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.store.Create(ctx, workspace); err != nil {
		if !errors.Is(err, repositories.ErrDuplicateCreationKey) {
			return models.WorkspaceResponse{}, false, err
		}
		existing, getErr := s.store.GetByCreationKey(ctx, actorID, idempotencyKey)
		if getErr != nil {
			return models.WorkspaceResponse{}, false, getErr
		}
		if existing.CreationHash != requestHash {
			return models.WorkspaceResponse{}, false, ErrIdempotencyConflict
		}
		return workspaceResponse(*existing, actorID), true, nil
	}

	return workspaceResponse(*workspace, actorID), false, nil
}

func (s *WorkspaceService) ListWorkspaces(
	ctx context.Context,
	actorID primitive.ObjectID,
	limit int,
	cursor string,
) (models.WorkspaceListResponse, error) {
	workspaces, nextCursor, err := s.store.ListForUser(ctx, actorID, limit, cursor)
	if err != nil {
		return models.WorkspaceListResponse{}, mapRepositoryError(err)
	}

	items := make([]models.WorkspaceResponse, 0, len(workspaces))
	for _, workspace := range workspaces {
		items = append(items, workspaceResponse(workspace, actorID))
	}
	return models.WorkspaceListResponse{Items: items, PageInfo: pageInfo(nextCursor)}, nil
}

func (s *WorkspaceService) GetWorkspace(
	ctx context.Context,
	actorID, workspaceID primitive.ObjectID,
) (models.WorkspaceResponse, error) {
	workspace, err := s.store.GetForUser(ctx, workspaceID, actorID)
	if err != nil {
		return models.WorkspaceResponse{}, mapRepositoryError(err)
	}
	return workspaceResponse(*workspace, actorID), nil
}

func (s *WorkspaceService) UpdateWorkspace(
	ctx context.Context,
	actorID, workspaceID primitive.ObjectID,
	request models.UpdateWorkspaceRequest,
) (models.WorkspaceResponse, error) {
	workspace, err := s.store.GetForUser(ctx, workspaceID, actorID)
	if err != nil {
		return models.WorkspaceResponse{}, mapRepositoryError(err)
	}
	actorMembership, ok := findMembershipByUser(workspace.Memberships, actorID)
	if !ok || !actorMembership.Role.CanManageMembers() {
		return models.WorkspaceResponse{}, ErrWorkspaceForbidden
	}
	if request.Name == nil && request.Description == nil {
		return models.WorkspaceResponse{}, fmt.Errorf("%w: at least one field is required", ErrInvalidWorkspaceData)
	}

	updates := bson.M{}
	if request.Name != nil {
		name := strings.TrimSpace(*request.Name)
		if len(name) < 1 || len(name) > 100 {
			return models.WorkspaceResponse{}, fmt.Errorf("%w: name must contain 1 to 100 characters", ErrInvalidWorkspaceData)
		}
		updates["name"] = name
	}
	if request.Description != nil {
		description := strings.TrimSpace(*request.Description)
		if len(description) > 1000 {
			return models.WorkspaceResponse{}, fmt.Errorf("%w: description must contain at most 1000 characters", ErrInvalidWorkspaceData)
		}
		updates["description"] = description
	}

	updated, err := s.store.UpdateMetadata(ctx, workspaceID, actorID, updates)
	if err != nil {
		return models.WorkspaceResponse{}, mapRepositoryError(err)
	}
	return workspaceResponse(*updated, actorID), nil
}

func (s *WorkspaceService) ListMemberships(
	ctx context.Context,
	actorID, workspaceID primitive.ObjectID,
	limit int,
	cursor string,
) (models.MembershipListResponse, error) {
	memberships, nextCursor, err := s.store.ListMembershipsForUser(ctx, workspaceID, actorID, limit, cursor)
	if err != nil {
		return models.MembershipListResponse{}, mapRepositoryError(err)
	}
	items := make([]models.MembershipResponse, 0, len(memberships))
	for _, membership := range memberships {
		items = append(items, membershipResponse(membership))
	}
	return models.MembershipListResponse{Items: items, PageInfo: pageInfo(nextCursor)}, nil
}

func (s *WorkspaceService) AddMembership(
	ctx context.Context,
	actorID, workspaceID, targetUserID primitive.ObjectID,
	role models.MembershipRole,
) (models.MembershipResponse, bool, error) {
	if !role.IsValid() {
		return models.MembershipResponse{}, false, fmt.Errorf("%w: unsupported membership role", ErrInvalidWorkspaceData)
	}
	workspace, actorMembership, err := s.authorizedManager(ctx, actorID, workspaceID)
	if err != nil {
		return models.MembershipResponse{}, false, err
	}
	if role == models.MembershipRoleOwner && actorMembership.Role != models.MembershipRoleOwner {
		return models.MembershipResponse{}, false, ErrWorkspaceForbidden
	}
	if existing, ok := findMembershipByUser(workspace.Memberships, targetUserID); ok {
		if existing.Role == role {
			return membershipResponse(existing), true, nil
		}
		return models.MembershipResponse{}, false, ErrMembershipExists
	}

	exists, err := s.users.Exists(ctx, targetUserID)
	if err != nil {
		return models.MembershipResponse{}, false, err
	}
	if !exists {
		return models.MembershipResponse{}, false, ErrTargetUserNotFound
	}

	now := s.now()
	membership := models.Membership{
		ID:          primitive.NewObjectID(),
		WorkspaceID: workspaceID,
		UserID:      targetUserID,
		Role:        role,
		Status:      models.MembershipStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	updated, err := s.store.AddMembership(ctx, workspaceID, actorID, membership, role == models.MembershipRoleOwner)
	if errors.Is(err, repositories.ErrConcurrentUpdate) {
		latest, getErr := s.store.GetForUser(ctx, workspaceID, actorID)
		if getErr != nil {
			return models.MembershipResponse{}, false, mapRepositoryError(getErr)
		}
		if existing, ok := findMembershipByUser(latest.Memberships, targetUserID); ok {
			if existing.Role == role {
				return membershipResponse(existing), true, nil
			}
			return models.MembershipResponse{}, false, ErrMembershipExists
		}
	}
	if err != nil {
		return models.MembershipResponse{}, false, mapRepositoryError(err)
	}
	created, ok := findMembershipByID(updated.Memberships, membership.ID)
	if !ok {
		return models.MembershipResponse{}, false, ErrWorkspaceConflict
	}
	return membershipResponse(created), false, nil
}

func (s *WorkspaceService) UpdateMembership(
	ctx context.Context,
	actorID, workspaceID, membershipID primitive.ObjectID,
	newRole models.MembershipRole,
) (models.MembershipResponse, error) {
	if !newRole.IsValid() {
		return models.MembershipResponse{}, fmt.Errorf("%w: unsupported membership role", ErrInvalidWorkspaceData)
	}
	workspace, actorMembership, err := s.authorizedManager(ctx, actorID, workspaceID)
	if err != nil {
		return models.MembershipResponse{}, err
	}
	target, ok := findMembershipByID(workspace.Memberships, membershipID)
	if !ok {
		return models.MembershipResponse{}, ErrMembershipNotFound
	}
	ownerOnly := target.Role == models.MembershipRoleOwner || newRole == models.MembershipRoleOwner
	if ownerOnly && actorMembership.Role != models.MembershipRoleOwner {
		return models.MembershipResponse{}, ErrWorkspaceForbidden
	}
	if target.Role == newRole {
		return membershipResponse(target), nil
	}
	if target.Role == models.MembershipRoleOwner && workspace.OwnerCount <= 1 {
		return models.MembershipResponse{}, ErrLastWorkspaceOwner
	}

	updated, err := s.store.UpdateMembershipRole(
		ctx,
		workspaceID,
		actorID,
		membershipID,
		target.Role,
		newRole,
		ownerOnly,
	)
	if err != nil {
		if errors.Is(err, repositories.ErrConcurrentUpdate) {
			latest, getErr := s.store.GetForUser(ctx, workspaceID, actorID)
			if getErr != nil {
				return models.MembershipResponse{}, mapRepositoryError(getErr)
			}
			latestTarget, found := findMembershipByID(latest.Memberships, membershipID)
			if !found {
				return models.MembershipResponse{}, ErrMembershipNotFound
			}
			if latestTarget.Role == newRole {
				return membershipResponse(latestTarget), nil
			}
			if latestTarget.Role == models.MembershipRoleOwner && newRole != models.MembershipRoleOwner && latest.OwnerCount <= 1 {
				return models.MembershipResponse{}, ErrLastWorkspaceOwner
			}
			return models.MembershipResponse{}, ErrWorkspaceConflict
		}
		return models.MembershipResponse{}, mapRepositoryError(err)
	}
	result, ok := findMembershipByID(updated.Memberships, membershipID)
	if !ok {
		return models.MembershipResponse{}, ErrWorkspaceConflict
	}
	return membershipResponse(result), nil
}

func (s *WorkspaceService) RemoveMembership(
	ctx context.Context,
	actorID, workspaceID, membershipID primitive.ObjectID,
) error {
	workspace, actorMembership, err := s.authorizedManager(ctx, actorID, workspaceID)
	if err != nil {
		return err
	}
	target, ok := findMembershipByID(workspace.Memberships, membershipID)
	if !ok {
		return ErrMembershipNotFound
	}
	ownerOnly := target.Role == models.MembershipRoleOwner
	if ownerOnly && actorMembership.Role != models.MembershipRoleOwner {
		return ErrWorkspaceForbidden
	}
	if ownerOnly && workspace.OwnerCount <= 1 {
		return ErrLastWorkspaceOwner
	}

	err = s.store.RemoveMembership(ctx, workspaceID, actorID, membershipID, target.Role, ownerOnly)
	if errors.Is(err, repositories.ErrConcurrentUpdate) {
		latest, getErr := s.store.GetForUser(ctx, workspaceID, actorID)
		if getErr != nil {
			return mapRepositoryError(getErr)
		}
		latestTarget, found := findMembershipByID(latest.Memberships, membershipID)
		if !found {
			return nil
		}
		if latestTarget.Role == models.MembershipRoleOwner && latest.OwnerCount <= 1 {
			return ErrLastWorkspaceOwner
		}
		return ErrWorkspaceConflict
	}
	return mapRepositoryError(err)
}

func (s *WorkspaceService) authorizedManager(
	ctx context.Context,
	actorID, workspaceID primitive.ObjectID,
) (*models.Workspace, models.Membership, error) {
	workspace, err := s.store.GetForUser(ctx, workspaceID, actorID)
	if err != nil {
		return nil, models.Membership{}, mapRepositoryError(err)
	}
	membership, ok := findMembershipByUser(workspace.Memberships, actorID)
	if !ok {
		return nil, models.Membership{}, ErrWorkspaceNotFound
	}
	if !membership.Role.CanManageMembers() {
		return nil, models.Membership{}, ErrWorkspaceForbidden
	}
	return workspace, membership, nil
}

func validateWorkspaceFields(name, description string) (string, string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if len(name) < 1 || len(name) > 100 {
		return "", "", fmt.Errorf("%w: name must contain 1 to 100 characters", ErrInvalidWorkspaceData)
	}
	if len(description) > 1000 {
		return "", "", fmt.Errorf("%w: description must contain at most 1000 characters", ErrInvalidWorkspaceData)
	}
	return name, description, nil
}

func workspaceRequestHash(name, description string) string {
	digest := sha256.Sum256([]byte(name + "\x00" + description))
	return hex.EncodeToString(digest[:])
}

func workspaceResponse(workspace models.Workspace, userID primitive.ObjectID) models.WorkspaceResponse {
	role := models.MembershipRoleMember
	if membership, ok := findMembershipByUser(workspace.Memberships, userID); ok {
		role = membership.Role
	}
	return models.WorkspaceResponse{
		ID:          workspace.ID.Hex(),
		Name:        workspace.Name,
		Description: workspace.Description,
		Role:        role,
		CreatedAt:   workspace.CreatedAt,
		UpdatedAt:   workspace.UpdatedAt,
	}
}

func membershipResponse(membership models.Membership) models.MembershipResponse {
	return models.MembershipResponse{
		ID:          membership.ID.Hex(),
		WorkspaceID: membership.WorkspaceID.Hex(),
		UserID:      membership.UserID.Hex(),
		Role:        membership.Role,
		Status:      membership.Status,
		CreatedAt:   membership.CreatedAt,
		UpdatedAt:   membership.UpdatedAt,
	}
}

func findMembershipByUser(memberships []models.Membership, userID primitive.ObjectID) (models.Membership, bool) {
	for _, membership := range memberships {
		if membership.UserID == userID && membership.Status == models.MembershipStatusActive {
			return membership, true
		}
	}
	return models.Membership{}, false
}

func findMembershipByID(memberships []models.Membership, membershipID primitive.ObjectID) (models.Membership, bool) {
	for _, membership := range memberships {
		if membership.ID == membershipID && membership.Status == models.MembershipStatusActive {
			return membership, true
		}
	}
	return models.Membership{}, false
}

func pageInfo(nextCursor string) models.PageInfo {
	if nextCursor == "" {
		return models.PageInfo{HasMore: false}
	}
	return models.PageInfo{NextCursor: &nextCursor, HasMore: true}
}

func mapRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repositories.ErrWorkspaceNotFound):
		return ErrWorkspaceNotFound
	case errors.Is(err, repositories.ErrMembershipNotFound):
		return ErrMembershipNotFound
	case errors.Is(err, repositories.ErrMembershipExists):
		return ErrMembershipExists
	case errors.Is(err, repositories.ErrInvalidCursor):
		return fmt.Errorf("%w: invalid pagination cursor", ErrInvalidWorkspaceData)
	case errors.Is(err, repositories.ErrConcurrentUpdate):
		return ErrWorkspaceConflict
	default:
		return err
	}
}
