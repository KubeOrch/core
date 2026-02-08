package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"golang.org/x/oauth2"
)

// OIDCDiscovery holds discovered OIDC endpoints
type OIDCDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JwksURI               string `json:"jwks_uri"`
}

var (
	discoveryCache   = make(map[string]*OIDCDiscovery)
	discoveryCacheMu sync.RWMutex
)

// DiscoverOIDC fetches and caches the OIDC discovery document for an issuer
func DiscoverOIDC(issuerURL string) (*OIDCDiscovery, error) {
	discoveryCacheMu.RLock()
	if cached, ok := discoveryCache[issuerURL]; ok {
		discoveryCacheMu.RUnlock()
		return cached, nil
	}
	discoveryCacheMu.RUnlock()

	wellKnownURL := strings.TrimRight(issuerURL, "/") + "/.well-known/openid-configuration"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(wellKnownURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OIDC discovery: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery returned status %d", resp.StatusCode)
	}

	var discovery OIDCDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("failed to decode OIDC discovery: %w", err)
	}

	discoveryCacheMu.Lock()
	discoveryCache[issuerURL] = &discovery
	discoveryCacheMu.Unlock()

	return &discovery, nil
}

// GenerateOAuthState creates a cryptographically random state and stores it in MongoDB
func GenerateOAuthState(provider string, redirectURL string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random state: %w", err)
	}
	state := base64.URLEncoding.EncodeToString(b)

	nb := make([]byte, 32)
	if _, err := rand.Read(nb); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	nonce := base64.URLEncoding.EncodeToString(nb)

	oauthState := models.OAuthState{
		State:       state,
		Provider:    provider,
		RedirectURL: redirectURL,
		Nonce:       nonce,
		CreatedAt:   time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := database.OAuthStateColl.InsertOne(ctx, oauthState); err != nil {
		return "", fmt.Errorf("failed to store OAuth state: %w", err)
	}

	return state, nil
}

// ValidateOAuthState validates and consumes a state parameter (single use)
func ValidateOAuthState(state string) (*models.OAuthState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var oauthState models.OAuthState
	err := database.OAuthStateColl.FindOneAndDelete(ctx, bson.M{"state": state}).Decode(&oauthState)
	if err != nil {
		return nil, errors.New("invalid or expired OAuth state")
	}
	return &oauthState, nil
}

// GetOAuth2Config builds an oauth2.Config for a given provider
func GetOAuth2Config(provider *models.OAuthProvider, callbackURL string) (*oauth2.Config, error) {
	authURL := provider.AuthorizationURL
	tokenURL := provider.TokenURL

	if provider.Type == "oidc" && provider.IssuerURL != "" {
		discovery, err := DiscoverOIDC(provider.IssuerURL)
		if err != nil {
			return nil, fmt.Errorf("OIDC discovery failed for %s: %w", provider.Name, err)
		}
		authURL = discovery.AuthorizationEndpoint
		tokenURL = discovery.TokenEndpoint
	}

	if authURL == "" || tokenURL == "" {
		return nil, fmt.Errorf("provider %s: authorization_url and token_url are required", provider.Name)
	}

	scopes := provider.Scopes
	if len(scopes) == 0 {
		if provider.Type == "oidc" {
			scopes = []string{"openid", "profile", "email"}
		} else {
			scopes = []string{"email"}
		}
	}

	return &oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  authURL,
			TokenURL: tokenURL,
		},
		RedirectURL: callbackURL,
		Scopes:      scopes,
	}, nil
}

// FetchOAuthUserInfo retrieves user information from the provider after token exchange
func FetchOAuthUserInfo(provider *models.OAuthProvider, token *oauth2.Token) (email, name, providerUserID string, err error) {
	// Try ID token first for OIDC providers
	if idTokenRaw, ok := token.Extra("id_token").(string); ok && idTokenRaw != "" {
		claims, parseErr := parseJWTPayload(idTokenRaw)
		if parseErr == nil {
			email = getClaimValue(claims, provider.ClaimMappings, "EMAIL", "email")
			name = getClaimValue(claims, provider.ClaimMappings, "NAME", "name")
			if sub, ok := claims["sub"].(string); ok {
				providerUserID = sub
			}
			if email != "" {
				return email, name, providerUserID, nil
			}
		}
	}

	// Fallback: call userinfo endpoint
	userinfoURL := provider.UserinfoURL
	if provider.Type == "oidc" && userinfoURL == "" && provider.IssuerURL != "" {
		discovery, discErr := DiscoverOIDC(provider.IssuerURL)
		if discErr != nil {
			return "", "", "", discErr
		}
		userinfoURL = discovery.UserinfoEndpoint
	}

	if userinfoURL == "" {
		return "", "", "", errors.New("no userinfo URL available for provider " + provider.Name)
	}

	client := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(token))
	req, err := http.NewRequest("GET", userinfoURL, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to fetch userinfo: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read userinfo response: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(body, &claims); err != nil {
		return "", "", "", fmt.Errorf("failed to parse userinfo response: %w", err)
	}

	email = getClaimValue(claims, provider.ClaimMappings, "EMAIL", "email")
	name = getClaimValue(claims, provider.ClaimMappings, "NAME", "name")

	// Provider user ID: try "sub" first, then "id" (GitHub uses numeric id)
	if sub, ok := claims["sub"].(string); ok {
		providerUserID = sub
	} else if id, ok := claims["id"]; ok {
		providerUserID = fmt.Sprintf("%v", id)
	}

	// For GitHub: email might be empty in userinfo, need to call /user/emails endpoint
	if email == "" && strings.Contains(userinfoURL, "github.com") {
		email, _ = fetchGitHubEmail(client)
	}

	return email, name, providerUserID, nil
}

// ValidateEmailDomain checks if the email's domain is in the provider's allowed domains list.
// If no allowed domains are configured, all domains are permitted.
func ValidateEmailDomain(provider *models.OAuthProvider, email string) error {
	if len(provider.AllowedDomains) == 0 {
		return nil
	}

	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid email address: %s", email)
	}
	domain := strings.ToLower(parts[1])

	for _, allowed := range provider.AllowedDomains {
		if strings.ToLower(allowed) == domain {
			return nil
		}
	}

	return fmt.Errorf("email domain %q is not allowed for provider %s", domain, provider.Name)
}

// FindOrCreateOAuthUser finds an existing user or creates a new one for an OAuth login
func FindOrCreateOAuthUser(email, name, providerName, providerUserID string) (*models.User, bool, error) {
	// 1. Try to find by provider + provider_user_id
	user, err := GetUserByProviderID(providerName, providerUserID)
	if err == nil {
		return user, false, nil
	}

	// 2. Try to find by email (account linking)
	user, err = GetUserByEmail(email)
	if err == nil {
		// Link the OAuth provider to the existing account
		user.AuthProvider = providerName
		user.ProviderUserID = providerUserID
		if updateErr := UpdateUser(user); updateErr != nil {
			logrus.Warnf("Failed to link OAuth provider to existing user %s: %v", email, updateErr)
		}
		return user, false, nil
	}

	// 3. Create new user
	role := models.RoleUser

	// First user becomes admin
	userCount, countErr := GetUserCount()
	if countErr != nil {
		return nil, false, fmt.Errorf("failed to check user count: %w", countErr)
	}
	if userCount == 0 {
		role = models.RoleAdmin
	}

	newUser := &models.User{
		Email:          email,
		Name:           name,
		Password:       "",
		Role:           role,
		AuthProvider:   providerName,
		ProviderUserID: providerUserID,
	}

	if err := CreateUser(newUser); err != nil {
		return nil, false, fmt.Errorf("failed to create OAuth user: %w", err)
	}

	return newUser, true, nil
}

// StoreExchangeCode stores a one-time code that the frontend can exchange for a JWT
func StoreExchangeCode(token string, user models.OAuthExchangeUser) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate exchange code: %w", err)
	}
	code := base64.URLEncoding.EncodeToString(b)

	exchangeCode := models.OAuthExchangeCode{
		Code:      code,
		Token:     token,
		User:      user,
		CreatedAt: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := database.OAuthCodeColl.InsertOne(ctx, exchangeCode); err != nil {
		return "", fmt.Errorf("failed to store exchange code: %w", err)
	}

	return code, nil
}

// RedeemExchangeCode validates and consumes a one-time exchange code
func RedeemExchangeCode(code string) (*models.OAuthExchangeCode, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var exchangeCode models.OAuthExchangeCode
	err := database.OAuthCodeColl.FindOneAndDelete(ctx, bson.M{"code": code}).Decode(&exchangeCode)
	if err != nil {
		return nil, errors.New("invalid or expired exchange code")
	}
	return &exchangeCode, nil
}

// parseJWTPayload decodes the payload section of a JWT without verifying the signature.
// Used for extracting claims from ID tokens where we trust the token was received directly from the provider.
func parseJWTPayload(tokenString string) (map[string]interface{}, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid JWT format")
	}

	// Decode the payload (second part)
	payload := parts[1]
	// Add padding if needed
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	return claims, nil
}

// getClaimValue resolves a claim value using custom mappings, falling back to the default key
func getClaimValue(claims map[string]interface{}, mappings map[string]string, mappingKey, defaultKey string) string {
	// Check custom mapping first
	if mappings != nil {
		if customKey, ok := mappings[mappingKey]; ok {
			if val, ok := claims[customKey].(string); ok && val != "" {
				return val
			}
		}
	}

	// Try the default key
	if val, ok := claims[defaultKey].(string); ok {
		return val
	}

	// Common alternatives
	alternatives := map[string][]string{
		"email": {"email", "mail", "preferred_username"},
		"name":  {"name", "display_name", "preferred_username", "given_name"},
	}
	if alts, ok := alternatives[defaultKey]; ok {
		for _, alt := range alts {
			if val, ok := claims[alt].(string); ok && val != "" {
				return val
			}
		}
	}

	return ""
}

// fetchGitHubEmail fetches the primary verified email from GitHub's /user/emails endpoint
func fetchGitHubEmail(client *http.Client) (string, error) {
	resp, err := client.Get("https://api.github.com/user/emails")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if err := json.Unmarshal(body, &emails); err != nil {
		return "", err
	}

	// Return the primary verified email
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	// Fallback: first verified email
	for _, e := range emails {
		if e.Verified {
			return e.Email, nil
		}
	}

	return "", errors.New("no verified email found")
}
