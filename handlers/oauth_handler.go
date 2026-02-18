package handlers

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/services"
	"github.com/KubeOrch/core/utils"
	"github.com/KubeOrch/core/utils/config"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

// GetAuthMethodsHandler returns the enabled authentication methods (public, no auth required)
func GetAuthMethodsHandler(c *gin.Context) {
	providers := config.GetEnabledOAuthProviders()
	publicProviders := make([]models.PublicProviderInfo, 0, len(providers))
	for _, p := range providers {
		publicProviders = append(publicProviders, models.PublicProviderInfo{
			Name:        p.Name,
			DisplayName: p.DisplayName,
			Icon:        p.Icon,
		})
	}

	c.JSON(http.StatusOK, models.AuthMethodsResponse{
		Builtin: models.BuiltinAuthConfig{
			Enabled:       config.GetAuthBuiltinEnabled(),
			SignupEnabled: config.GetAuthSignupEnabled(),
		},
		Providers: publicProviders,
	})
}

// OAuthAuthorizeHandler initiates the OAuth flow by redirecting to the provider
func OAuthAuthorizeHandler(c *gin.Context) {
	providerName := c.Param("provider")

	provider, err := config.GetOAuthProviderByName(providerName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Unknown or disabled provider"})
		return
	}

	callbackURL := fmt.Sprintf("%s/v1/api/auth/oauth/%s/callback", config.GetBaseURL(), providerName)

	oauthConfig, err := services.GetOAuth2Config(provider, callbackURL)
	if err != nil {
		logrus.Errorf("Failed to build OAuth config for %s: %v", providerName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Provider configuration error"})
		return
	}

	// Where to redirect the user after auth completes
	frontendRedirect := config.GetFrontendURL() + "/auth/callback"

	state, err := services.GenerateOAuthState(providerName, frontendRedirect)
	if err != nil {
		logrus.Errorf("Failed to generate OAuth state: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initiate authentication"})
		return
	}

	authURL := oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	c.Redirect(http.StatusTemporaryRedirect, authURL)
}

// OAuthCallbackHandler handles the redirect from the OAuth provider
func OAuthCallbackHandler(c *gin.Context) {
	providerName := c.Param("provider")
	frontendURL := config.GetFrontendURL()

	// Validate state parameter
	stateParam := c.Query("state")
	if stateParam == "" {
		redirectWithError(c, frontendURL, "Missing state parameter")
		return
	}

	oauthState, err := services.ValidateOAuthState(stateParam)
	if err != nil {
		logrus.Warnf("Invalid OAuth state from %s: %v", providerName, err)
		redirectWithError(c, frontendURL, "Invalid or expired authentication state")
		return
	}

	// Check for error from provider
	if errParam := c.Query("error"); errParam != "" {
		errDesc := c.Query("error_description")
		logrus.Warnf("OAuth error from %s: %s - %s", providerName, errParam, errDesc)
		redirectWithError(c, frontendURL, "Authentication denied by provider")
		return
	}

	// Get authorization code
	code := c.Query("code")
	if code == "" {
		redirectWithError(c, frontendURL, "No authorization code received")
		return
	}

	provider, err := config.GetOAuthProviderByName(providerName)
	if err != nil {
		redirectWithError(c, frontendURL, "Provider not found")
		return
	}

	callbackURL := fmt.Sprintf("%s/v1/api/auth/oauth/%s/callback", config.GetBaseURL(), providerName)

	oauthConfig, err := services.GetOAuth2Config(provider, callbackURL)
	if err != nil {
		logrus.Errorf("Failed to build OAuth config for %s: %v", providerName, err)
		redirectWithError(c, frontendURL, "Provider configuration error")
		return
	}

	// Exchange authorization code for tokens
	token, err := oauthConfig.Exchange(c.Request.Context(), code)
	if err != nil {
		logrus.Errorf("Token exchange failed for %s: %v", providerName, err)
		redirectWithError(c, frontendURL, "Authentication failed")
		return
	}

	// Fetch user info from provider
	email, name, providerUserID, err := services.FetchOAuthUserInfo(provider, token)
	if err != nil || email == "" {
		logrus.Errorf("Failed to get user info from %s: %v", providerName, err)
		redirectWithError(c, frontendURL, "Failed to retrieve user information")
		return
	}

	// Validate email domain restriction
	if domainErr := services.ValidateEmailDomain(provider, email); domainErr != nil {
		logrus.Warnf("OAuth domain restriction for %s: %v", providerName, domainErr)
		redirectWithError(c, frontendURL, "Your email domain is not authorized to access this application")
		return
	}

	// Find or create user in our database
	user, isNew, err := services.FindOrCreateOAuthUser(email, name, providerName, providerUserID)
	if err != nil {
		logrus.Errorf("Failed to find/create user for %s: %v", providerName, err)
		redirectWithError(c, frontendURL, "Failed to create user account")
		return
	}

	if isNew {
		logrus.Infof("New OAuth user registered via %s: %s", providerName, email)
	}

	// Generate internal JWT
	jwtToken, err := services.GenerateJWTToken(user.ID, user.Email, user.Role)
	if err != nil {
		logrus.Errorf("Failed to generate JWT for OAuth user: %v", err)
		redirectWithError(c, frontendURL, "Authentication failed")
		return
	}

	// Store a one-time exchange code (the frontend will exchange it for the JWT)
	exchangeUser := models.OAuthExchangeUser{
		ID:           user.ID.Hex(),
		Email:        user.Email,
		Name:         user.Name,
		Role:         string(user.Role),
		AvatarURL:    utils.GetGravatarURL(user.Email, 200),
		CreatedAt:    user.CreatedAt.Format("2006-01-02T15:04:05Z"),
		AuthProvider: providerName,
	}

	exchangeCode, err := services.StoreExchangeCode(jwtToken, exchangeUser)
	if err != nil {
		logrus.Errorf("Failed to store exchange code: %v", err)
		redirectWithError(c, frontendURL, "Authentication failed")
		return
	}

	// Redirect to frontend callback page with the one-time code
	redirectURL := fmt.Sprintf("%s?code=%s", oauthState.RedirectURL, url.QueryEscape(exchangeCode))
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

// OAuthExchangeHandler exchanges a one-time code for a JWT token
func OAuthExchangeHandler(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Code is required"})
		return
	}

	exchangeCode, err := services.RedeemExchangeCode(req.Code)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired code"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": exchangeCode.Token,
		"user":  exchangeCode.User,
	})
}

// redirectWithError redirects to the frontend login page with an error message
func redirectWithError(c *gin.Context, frontendURL, message string) {
	redirectURL := fmt.Sprintf("%s/login?error=%s", frontendURL, url.QueryEscape(message))
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}
