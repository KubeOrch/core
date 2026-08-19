package services

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type memoryWorkspaceStore struct {
	workspaces map[primitive.ObjectID]*models.Workspace
}

func newMemoryWorkspaceStore() *memoryWorkspaceStore {
	return &memoryWorkspaceStore{workspaces: make(map[primitive.ObjectID]*models.Workspace)}
}

func (s *memoryWorkspaceStore) Create(_ context.Context, workspace *models.Workspace) error {
	for _, existing := range s.workspaces {
		if existing.CreatedBy == workspace.CreatedBy && existing.CreationKey == workspace.CreationKey {
			return repositories.ErrDuplicateCreationKey
		}
	}
	s.workspaces[workspace.ID] = cloneWorkspace(workspace)
	return nil
}

func (s *memoryWorkspaceStore) GetByCreationKey(_ context.Context, creatorID primitive.ObjectID, key string) (*models.Workspace, error) {
	for _, workspace := range s.workspaces {
		if workspace.CreatedBy == creatorID && workspace.CreationKey == key {
			return cloneWorkspace(workspace), nil
		}
	}
	return nil, repositories.ErrWorkspaceNotFound
}

func (s *memoryWorkspaceStore) GetForUser(_ context.Context, workspaceID, userID primitive.ObjectID) (*models.Workspace, error) {
	workspace, ok := s.workspaces[workspaceID]
	if !ok {
		return nil, repositories.ErrWorkspaceNotFound
	}
	if _, ok := findMembershipByUser(workspace.Memberships, userID); !ok {
		return nil, repositories.ErrWorkspaceNotFound
	}
	return cloneWorkspace(workspace), nil
}

func (s *memoryWorkspaceStore) ListForUser(_ context.Context, userID primitive.ObjectID, limit int, _ string) ([]models.Workspace, string, error) {
	var result []models.Workspace
	for _, workspace := range s.workspaces {
		if membership, ok := findMembershipByUser(workspace.Memberships, userID); ok {
			copy := *cloneWorkspace(workspace)
			copy.Memberships = []models.Membership{membership}
			result = append(result, copy)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID.Hex() > result[j].ID.Hex() })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, "", nil
}

func (s *memoryWorkspaceStore) UpdateMetadata(_ context.Context, workspaceID, actorID primitive.ObjectID, updates bson.M) (*models.Workspace, error) {
	workspace, err := s.GetForUser(context.Background(), workspaceID, actorID)
	if err != nil {
		return nil, err
	}
	actor, _ := findMembershipByUser(workspace.Memberships, actorID)
	if !actor.Role.CanManageMembers() {
		return nil, repositories.ErrWorkspaceNotFound
	}
	stored := s.workspaces[workspaceID]
	if name, ok := updates["name"].(string); ok {
		stored.Name = name
	}
	if description, ok := updates["description"].(string); ok {
		stored.Description = description
	}
	stored.UpdatedAt = time.Now().UTC()
	return cloneWorkspace(stored), nil
}

func (s *memoryWorkspaceStore) AddMembership(_ context.Context, workspaceID, actorID primitive.ObjectID, membership models.Membership, ownerOnly bool) (*models.Workspace, error) {
	workspace, err := s.GetForUser(context.Background(), workspaceID, actorID)
	if err != nil {
		return nil, repositories.ErrConcurrentUpdate
	}
	actor, _ := findMembershipByUser(workspace.Memberships, actorID)
	if !actor.Role.CanManageMembers() || (ownerOnly && actor.Role != models.MembershipRoleOwner) {
		return nil, repositories.ErrConcurrentUpdate
	}
	stored := s.workspaces[workspaceID]
	if _, exists := findMembershipByUser(stored.Memberships, membership.UserID); exists {
		return nil, repositories.ErrConcurrentUpdate
	}
	stored.Memberships = append(stored.Memberships, membership)
	if membership.Role == models.MembershipRoleOwner {
		stored.OwnerCount++
	}
	return cloneWorkspace(stored), nil
}

func (s *memoryWorkspaceStore) UpdateMembershipRole(
	_ context.Context,
	workspaceID, actorID, membershipID primitive.ObjectID,
	currentRole, newRole models.MembershipRole,
	ownerOnly bool,
) (*models.Workspace, error) {
	workspace, err := s.GetForUser(context.Background(), workspaceID, actorID)
	if err != nil {
		return nil, repositories.ErrConcurrentUpdate
	}
	actor, _ := findMembershipByUser(workspace.Memberships, actorID)
	if !actor.Role.CanManageMembers() || (ownerOnly && actor.Role != models.MembershipRoleOwner) {
		return nil, repositories.ErrConcurrentUpdate
	}
	stored := s.workspaces[workspaceID]
	if currentRole == models.MembershipRoleOwner && newRole != models.MembershipRoleOwner && stored.OwnerCount <= 1 {
		return nil, repositories.ErrConcurrentUpdate
	}
	for index := range stored.Memberships {
		if stored.Memberships[index].ID != membershipID || stored.Memberships[index].Role != currentRole {
			continue
		}
		stored.Memberships[index].Role = newRole
		stored.OwnerCount += testOwnerDelta(currentRole, newRole)
		return cloneWorkspace(stored), nil
	}
	return nil, repositories.ErrConcurrentUpdate
}

func (s *memoryWorkspaceStore) RemoveMembership(
	_ context.Context,
	workspaceID, actorID, membershipID primitive.ObjectID,
	currentRole models.MembershipRole,
	ownerOnly bool,
) error {
	workspace, err := s.GetForUser(context.Background(), workspaceID, actorID)
	if err != nil {
		return repositories.ErrConcurrentUpdate
	}
	actor, _ := findMembershipByUser(workspace.Memberships, actorID)
	if !actor.Role.CanManageMembers() || (ownerOnly && actor.Role != models.MembershipRoleOwner) {
		return repositories.ErrConcurrentUpdate
	}
	stored := s.workspaces[workspaceID]
	if currentRole == models.MembershipRoleOwner && stored.OwnerCount <= 1 {
		return repositories.ErrConcurrentUpdate
	}
	for index := range stored.Memberships {
		if stored.Memberships[index].ID == membershipID && stored.Memberships[index].Role == currentRole {
			stored.Memberships = append(stored.Memberships[:index], stored.Memberships[index+1:]...)
			if currentRole == models.MembershipRoleOwner {
				stored.OwnerCount--
			}
			return nil
		}
	}
	return repositories.ErrConcurrentUpdate
}

func (s *memoryWorkspaceStore) ListMembershipsForUser(
	ctx context.Context,
	workspaceID, userID primitive.ObjectID,
	limit int,
	_ string,
) ([]models.Membership, string, error) {
	workspace, err := s.GetForUser(ctx, workspaceID, userID)
	if err != nil {
		return nil, "", err
	}
	memberships := append([]models.Membership(nil), workspace.Memberships...)
	if len(memberships) > limit {
		memberships = memberships[:limit]
	}
	return memberships, "", nil
}

type memoryUserDirectory struct {
	users map[primitive.ObjectID]bool
}

type concurrentRoleStore struct {
	*memoryWorkspaceStore
	newRole models.MembershipRole
}

func (s *concurrentRoleStore) UpdateMembershipRole(
	_ context.Context,
	workspaceID, _ primitive.ObjectID,
	membershipID primitive.ObjectID,
	_, _ models.MembershipRole,
	_ bool,
) (*models.Workspace, error) {
	for index := range s.workspaces[workspaceID].Memberships {
		if s.workspaces[workspaceID].Memberships[index].ID == membershipID {
			s.workspaces[workspaceID].Memberships[index].Role = s.newRole
			break
		}
	}
	return nil, repositories.ErrConcurrentUpdate
}

func (d memoryUserDirectory) Exists(_ context.Context, userID primitive.ObjectID) (bool, error) {
	return d.users[userID], nil
}

func TestCreateWorkspaceBootstrapsOwnerAtomically(t *testing.T) {
	store := newMemoryWorkspaceStore()
	actorID := primitive.NewObjectID()
	service := newTestWorkspaceService(store, actorID)

	response, replayed, err := service.CreateWorkspace(context.Background(), actorID, models.CreateWorkspaceRequest{
		Name:        " Platform ",
		Description: " Design partner workspace ",
	}, "create-platform")

	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, "Platform", response.Name)
	assert.Equal(t, models.MembershipRoleOwner, response.Role)
	workspaceID, err := primitive.ObjectIDFromHex(response.ID)
	require.NoError(t, err)
	stored := store.workspaces[workspaceID]
	require.NotNil(t, stored)
	assert.Equal(t, 1, stored.OwnerCount)
	require.Len(t, stored.Memberships, 1)
	assert.Equal(t, actorID, stored.Memberships[0].UserID)
	assert.Equal(t, models.MembershipRoleOwner, stored.Memberships[0].Role)
}

func TestCreateWorkspaceReplaysAndProtectsIdempotencyKey(t *testing.T) {
	store := newMemoryWorkspaceStore()
	actorID := primitive.NewObjectID()
	service := newTestWorkspaceService(store, actorID)
	request := models.CreateWorkspaceRequest{Name: "Platform"}

	first, replayed, err := service.CreateWorkspace(context.Background(), actorID, request, "create-platform")
	require.NoError(t, err)
	assert.False(t, replayed)

	second, replayed, err := service.CreateWorkspace(context.Background(), actorID, request, "create-platform")
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, first.ID, second.ID)

	_, _, err = service.CreateWorkspace(context.Background(), actorID, models.CreateWorkspaceRequest{Name: "Different"}, "create-platform")
	assert.ErrorIs(t, err, ErrIdempotencyConflict)
}

func TestWorkspaceReadIsNonEnumeratingForNonMember(t *testing.T) {
	store := newMemoryWorkspaceStore()
	ownerID := primitive.NewObjectID()
	service := newTestWorkspaceService(store, ownerID)
	workspace, _, err := service.CreateWorkspace(context.Background(), ownerID, models.CreateWorkspaceRequest{Name: "Platform"}, "create-platform")
	require.NoError(t, err)
	workspaceID, _ := primitive.ObjectIDFromHex(workspace.ID)

	_, err = service.GetWorkspace(context.Background(), primitive.NewObjectID(), workspaceID)

	assert.ErrorIs(t, err, ErrWorkspaceNotFound)
}

func TestMembershipAuthorizationAndLastOwnerInvariant(t *testing.T) {
	store := newMemoryWorkspaceStore()
	ownerID := primitive.NewObjectID()
	adminID := primitive.NewObjectID()
	memberID := primitive.NewObjectID()
	secondOwnerID := primitive.NewObjectID()
	service := newTestWorkspaceService(store, ownerID, adminID, memberID, secondOwnerID)
	created, _, err := service.CreateWorkspace(context.Background(), ownerID, models.CreateWorkspaceRequest{Name: "Platform"}, "create-platform")
	require.NoError(t, err)
	workspaceID, _ := primitive.ObjectIDFromHex(created.ID)

	admin, _, err := service.AddMembership(context.Background(), ownerID, workspaceID, adminID, models.MembershipRoleAdmin)
	require.NoError(t, err)
	_, _, err = service.AddMembership(context.Background(), ownerID, workspaceID, memberID, models.MembershipRoleMember)
	require.NoError(t, err)

	_, _, err = service.AddMembership(context.Background(), memberID, workspaceID, primitive.NewObjectID(), models.MembershipRoleMember)
	assert.ErrorIs(t, err, ErrWorkspaceForbidden)

	adminMembershipID, _ := primitive.ObjectIDFromHex(admin.ID)
	_, err = service.UpdateMembership(context.Background(), adminID, workspaceID, adminMembershipID, models.MembershipRoleOwner)
	assert.ErrorIs(t, err, ErrWorkspaceForbidden)

	ownerMembership := store.workspaces[workspaceID].Memberships[0]
	_, err = service.UpdateMembership(context.Background(), ownerID, workspaceID, ownerMembership.ID, models.MembershipRoleAdmin)
	assert.ErrorIs(t, err, ErrLastWorkspaceOwner)
	assert.ErrorIs(t, service.RemoveMembership(context.Background(), ownerID, workspaceID, ownerMembership.ID), ErrLastWorkspaceOwner)

	_, _, err = service.AddMembership(context.Background(), ownerID, workspaceID, secondOwnerID, models.MembershipRoleOwner)
	require.NoError(t, err)
	updated, err := service.UpdateMembership(context.Background(), ownerID, workspaceID, ownerMembership.ID, models.MembershipRoleAdmin)
	require.NoError(t, err)
	assert.Equal(t, models.MembershipRoleAdmin, updated.Role)
	assert.Equal(t, 1, store.workspaces[workspaceID].OwnerCount)
}

func TestDuplicateMembershipIsReplayOrConflict(t *testing.T) {
	store := newMemoryWorkspaceStore()
	ownerID := primitive.NewObjectID()
	memberID := primitive.NewObjectID()
	service := newTestWorkspaceService(store, ownerID, memberID)
	created, _, err := service.CreateWorkspace(context.Background(), ownerID, models.CreateWorkspaceRequest{Name: "Platform"}, "create-platform")
	require.NoError(t, err)
	workspaceID, _ := primitive.ObjectIDFromHex(created.ID)

	first, replayed, err := service.AddMembership(context.Background(), ownerID, workspaceID, memberID, models.MembershipRoleMember)
	require.NoError(t, err)
	assert.False(t, replayed)
	second, replayed, err := service.AddMembership(context.Background(), ownerID, workspaceID, memberID, models.MembershipRoleMember)
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, first.ID, second.ID)

	_, _, err = service.AddMembership(context.Background(), ownerID, workspaceID, memberID, models.MembershipRoleAdmin)
	assert.ErrorIs(t, err, ErrMembershipExists)
}

func TestCrossWorkspaceMembershipIDIsRejected(t *testing.T) {
	store := newMemoryWorkspaceStore()
	ownerID := primitive.NewObjectID()
	service := newTestWorkspaceService(store, ownerID)
	first, _, err := service.CreateWorkspace(context.Background(), ownerID, models.CreateWorkspaceRequest{Name: "First"}, "create-first")
	require.NoError(t, err)
	second, _, err := service.CreateWorkspace(context.Background(), ownerID, models.CreateWorkspaceRequest{Name: "Second"}, "create-second")
	require.NoError(t, err)
	firstID, _ := primitive.ObjectIDFromHex(first.ID)
	secondID, _ := primitive.ObjectIDFromHex(second.ID)
	foreignMembershipID := store.workspaces[secondID].Memberships[0].ID

	_, err = service.UpdateMembership(context.Background(), ownerID, firstID, foreignMembershipID, models.MembershipRoleAdmin)

	assert.ErrorIs(t, err, ErrMembershipNotFound)
}

func TestConcurrentIdenticalRoleUpdateIsIdempotent(t *testing.T) {
	baseStore := newMemoryWorkspaceStore()
	ownerID := primitive.NewObjectID()
	memberID := primitive.NewObjectID()
	service := newTestWorkspaceService(baseStore, ownerID, memberID)
	created, _, err := service.CreateWorkspace(context.Background(), ownerID, models.CreateWorkspaceRequest{Name: "Platform"}, "create-platform")
	require.NoError(t, err)
	workspaceID, _ := primitive.ObjectIDFromHex(created.ID)
	membership, _, err := service.AddMembership(context.Background(), ownerID, workspaceID, memberID, models.MembershipRoleMember)
	require.NoError(t, err)
	membershipID, _ := primitive.ObjectIDFromHex(membership.ID)

	concurrentStore := &concurrentRoleStore{memoryWorkspaceStore: baseStore, newRole: models.MembershipRoleAdmin}
	service.store = concurrentStore
	updated, err := service.UpdateMembership(context.Background(), ownerID, workspaceID, membershipID, models.MembershipRoleAdmin)

	require.NoError(t, err)
	assert.Equal(t, models.MembershipRoleAdmin, updated.Role)
}

func newTestWorkspaceService(store *memoryWorkspaceStore, userIDs ...primitive.ObjectID) *WorkspaceService {
	users := make(map[primitive.ObjectID]bool, len(userIDs))
	for _, userID := range userIDs {
		users[userID] = true
	}
	service := NewWorkspaceService(store, memoryUserDirectory{users: users})
	service.now = func() time.Time { return time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC) }
	return service
}

func cloneWorkspace(workspace *models.Workspace) *models.Workspace {
	if workspace == nil {
		return nil
	}
	copy := *workspace
	copy.Memberships = append([]models.Membership(nil), workspace.Memberships...)
	return &copy
}

func testOwnerDelta(currentRole, newRole models.MembershipRole) int {
	if currentRole == models.MembershipRoleOwner && newRole != models.MembershipRoleOwner {
		return -1
	}
	if currentRole != models.MembershipRoleOwner && newRole == models.MembershipRoleOwner {
		return 1
	}
	return 0
}

func TestMapRepositoryErrorPreservesUnexpectedErrors(t *testing.T) {
	want := errors.New("database unavailable")
	assert.ErrorIs(t, mapRepositoryError(want), want)
}
