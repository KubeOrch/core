package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KubeOrch/core/pkg/api"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBetaAuthMiddlewareReturnsProblemDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestIDMiddleware(), BetaAuthMiddleware())
	router.GET("/protected", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
	assert.NotEmpty(t, response.Header().Get("X-Request-Id"))
	var problem api.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, "authentication_required", problem.Code)
	assert.Equal(t, response.Header().Get("X-Request-Id"), problem.RequestID)
}

func TestBearerCredentialParsing(t *testing.T) {
	for _, test := range []struct {
		name       string
		header     string
		credential string
		valid      bool
	}{
		{name: "preferred case", header: "Bearer token", credential: "token", valid: true},
		{name: "lowercase scheme", header: "bearer token", credential: "token", valid: true},
		{name: "uppercase scheme", header: "BEARER token", credential: "token", valid: true},
		{name: "multiple spaces", header: "Bearer   token", credential: "token", valid: true},
		{name: "wrong scheme", header: "Basic token"},
		{name: "missing credential", header: "Bearer"},
		{name: "empty credential", header: "Bearer   "},
		{name: "extra credential", header: "Bearer token extra"},
		{name: "tab separator", header: "Bearer\ttoken"},
	} {
		t.Run(test.name, func(t *testing.T) {
			credential, valid := bearerCredential(test.header)
			assert.Equal(t, test.valid, valid)
			assert.Equal(t, test.credential, credential)
		})
	}
}
