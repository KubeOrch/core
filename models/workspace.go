package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type MembershipRole string

const (
	MembershipRoleOwner  MembershipRole = "owner"
	MembershipRoleAdmin  MembershipRole = "admin"
	MembershipRoleMember MembershipRole = "member"
)

type MembershipStatus string

const MembershipStatusActive MembershipStatus = "active"

type Membership struct {
	ID          primitive.ObjectID `bson:"_id" json:"-"`
	WorkspaceID primitive.ObjectID `bson:"workspace_id" json:"-"`
	UserID      primitive.ObjectID `bson:"user_id" json:"-"`
	Role        MembershipRole     `bson:"role" json:"-"`
	Status      MembershipStatus   `bson:"status" json:"-"`
	CreatedAt   time.Time          `bson:"created_at" json:"-"`
	UpdatedAt   time.Time          `bson:"updated_at" json:"-"`
}

type Workspace struct {
	ID           primitive.ObjectID `bson:"_id" json:"-"`
	Name         string             `bson:"name" json:"-"`
	Description  string             `bson:"description,omitempty" json:"-"`
	Memberships  []Membership       `bson:"memberships" json:"-"`
	OwnerCount   int                `bson:"owner_count" json:"-"`
	CreatedBy    primitive.ObjectID `bson:"created_by" json:"-"`
	CreationKey  string             `bson:"creation_key" json:"-"`
	CreationHash string             `bson:"creation_hash" json:"-"`
	CreatedAt    time.Time          `bson:"created_at" json:"-"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"-"`
}

type CreateWorkspaceRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type UpdateWorkspaceRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type AddMembershipRequest struct {
	UserID string         `json:"userId"`
	Role   MembershipRole `json:"role"`
}

type UpdateMembershipRequest struct {
	Role MembershipRole `json:"role"`
}

type WorkspaceResponse struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Role        MembershipRole `json:"role"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type MembershipResponse struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspaceId"`
	UserID      string           `json:"userId"`
	Role        MembershipRole   `json:"role"`
	Status      MembershipStatus `json:"status"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
}

type PageInfo struct {
	NextCursor *string `json:"nextCursor"`
	HasMore    bool    `json:"hasMore"`
}

type WorkspaceListResponse struct {
	Items    []WorkspaceResponse `json:"items"`
	PageInfo PageInfo            `json:"pageInfo"`
}

type MembershipListResponse struct {
	Items    []MembershipResponse `json:"items"`
	PageInfo PageInfo             `json:"pageInfo"`
}

func (r MembershipRole) IsValid() bool {
	switch r {
	case MembershipRoleOwner, MembershipRoleAdmin, MembershipRoleMember:
		return true
	default:
		return false
	}
}

func (r MembershipRole) CanManageMembers() bool {
	return r == MembershipRoleOwner || r == MembershipRoleAdmin
}
