package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// OAuthProvider represents a configured OAuth2/OIDC provider from config.yaml
type OAuthProvider struct {
	Name             string            `mapstructure:"NAME" json:"name"`
	DisplayName      string            `mapstructure:"DISPLAY_NAME" json:"displayName"`
	Type             string            `mapstructure:"TYPE" json:"type"` // "oidc" or "oauth2"
	Enabled          bool              `mapstructure:"ENABLED" json:"enabled"`
	ClientID         string            `mapstructure:"CLIENT_ID" json:"-"`
	ClientSecret     string            `mapstructure:"CLIENT_SECRET" json:"-"`
	IssuerURL        string            `mapstructure:"ISSUER_URL" json:"-"`
	AuthorizationURL string            `mapstructure:"AUTHORIZATION_URL" json:"-"`
	TokenURL         string            `mapstructure:"TOKEN_URL" json:"-"`
	UserinfoURL      string            `mapstructure:"USERINFO_URL" json:"-"`
	Scopes           []string          `mapstructure:"SCOPES" json:"-"`
	ClaimMappings    map[string]string `mapstructure:"CLAIM_MAPPINGS" json:"-"`
	AllowedDomains   []string          `mapstructure:"ALLOWED_DOMAINS" json:"-"`
	DefaultRole      string            `mapstructure:"DEFAULT_ROLE" json:"-"`
	Icon             string            `mapstructure:"ICON" json:"icon"`
}

// OAuthState stores the state parameter for CSRF protection during OAuth flow
type OAuthState struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	State       string             `bson:"state"`
	Provider    string             `bson:"provider"`
	RedirectURL string             `bson:"redirect_url"`
	Nonce       string             `bson:"nonce,omitempty"`
	CreatedAt   time.Time          `bson:"created_at"`
}

// OAuthExchangeCode stores a one-time code that the frontend exchanges for a JWT
type OAuthExchangeCode struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	Code      string             `bson:"code"`
	Token     string             `bson:"token"`
	User      OAuthExchangeUser  `bson:"user"`
	CreatedAt time.Time          `bson:"created_at"`
}

// OAuthExchangeUser holds user data stored alongside the exchange code
type OAuthExchangeUser struct {
	ID           string `bson:"id" json:"id"`
	Email        string `bson:"email" json:"email"`
	Name         string `bson:"name" json:"name"`
	Role         string `bson:"role" json:"role"`
	AvatarURL    string `bson:"avatar_url" json:"avatarUrl"`
	CreatedAt    string `bson:"created_at" json:"createdAt"`
	AuthProvider string `bson:"auth_provider" json:"authProvider"`
}

// AuthMethodsResponse is the public API response for /auth/methods
type AuthMethodsResponse struct {
	Builtin   BuiltinAuthConfig  `json:"builtin"`
	Providers []PublicProviderInfo `json:"providers"`
}

// BuiltinAuthConfig describes the state of built-in email/password auth
type BuiltinAuthConfig struct {
	Enabled       bool `json:"enabled"`
	SignupEnabled bool `json:"signupEnabled"`
}

// PublicProviderInfo is the safe-to-expose subset of an OAuthProvider
type PublicProviderInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Icon        string `json:"icon"`
}
