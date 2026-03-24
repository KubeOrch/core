package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KubeOrch/core/middleware"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestRouter creates a minimal router for integration testing
// without MongoDB or external service dependencies.
func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.MetricsMiddleware())
	r.Use(middleware.RateLimiterMiddleware())

	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.GET("/v1/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Hello KubeOrchestra!",
			"status":  "success",
		})
	})
	r.GET("/v1/api-docs", func(c *gin.Context) {
		c.String(http.StatusOK, "openapi: 3.0.3")
	})

	return r
}

func TestHelloEndpoint(t *testing.T) {
	router := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Equal(t, "success", body["status"])
	assert.Equal(t, "Hello KubeOrchestra!", body["message"])
}

func TestMetricsEndpoint(t *testing.T) {
	router := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/metrics", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "http_requests_total")
	assert.Contains(t, body, "http_request_duration_seconds")
}

func TestAPIDocsEndpoint(t *testing.T) {
	router := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/api-docs", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "openapi")
}

func TestRateLimitingIntegration(t *testing.T) {
	viper.Set("RATE_LIMIT_RPS", 1)
	viper.Set("RATE_LIMIT_BURST", 3)
	defer viper.Reset()

	router := setupTestRouter()

	rateLimited := false
	for i := 0; i < 20; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/v1/", nil)
		req.RemoteAddr = "192.0.2.1:9999"
		router.ServeHTTP(w, req)

		if w.Code == http.StatusTooManyRequests {
			rateLimited = true

			var body map[string]string
			err := json.Unmarshal(w.Body.Bytes(), &body)
			require.NoError(t, err)
			assert.Contains(t, body["error"], "rate limit exceeded")
			assert.Equal(t, "1", w.Header().Get("Retry-After"))
			break
		}
	}
	assert.True(t, rateLimited, "should have been rate limited")
}

func TestProtectedEndpointRequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.AuthMiddleware())
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/protected", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Contains(t, body["error"], "token is required")
}

func TestProtectedEndpointRejectsInvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.AuthMiddleware())
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestContentTypeIsJSON(t *testing.T) {
	router := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/", nil)
	router.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	assert.True(t, strings.HasPrefix(contentType, "application/json"))
}

func TestNotFoundReturns404(t *testing.T) {
	router := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/nonexistent", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestMethodNotAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.GET("/v1/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestMetricsAfterTraffic(t *testing.T) {
	router := setupTestRouter()

	// Generate some traffic
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/v1/", nil)
		req.RemoteAddr = "10.10.10.10:1234"
		router.ServeHTTP(w, req)
	}

	// Check metrics reflect the traffic
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/metrics", nil)
	router.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	metricsStr := string(body)
	assert.Contains(t, metricsStr, "http_requests_total")
	assert.Contains(t, metricsStr, `method="GET"`)
}
