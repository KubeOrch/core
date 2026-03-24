package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func setupRateLimitRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimiterMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

func TestRateLimiterAllowsNormalTraffic(t *testing.T) {
	viper.Set("RATE_LIMIT_RPS", 100)
	viper.Set("RATE_LIMIT_BURST", 100)
	defer viper.Reset()

	router := setupRateLimitRouter()

	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}
}

func TestRateLimiterBlocksExcessiveTraffic(t *testing.T) {
	viper.Set("RATE_LIMIT_RPS", 1)
	viper.Set("RATE_LIMIT_BURST", 2)
	defer viper.Reset()

	// Reset visitors to avoid interference from other tests
	mu.Lock()
	visitors = make(map[string]*visitor)
	mu.Unlock()

	router := setupRateLimitRouter()

	blocked := false
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		router.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			blocked = true
			assert.Contains(t, w.Body.String(), "rate limit exceeded")
			assert.Equal(t, "1", w.Header().Get("Retry-After"))
			break
		}
	}
	assert.True(t, blocked, "rate limiter should have blocked at least one request")
}

func TestRateLimiterDefaultValues(t *testing.T) {
	viper.Reset()

	router := setupRateLimitRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "172.16.0.1:1234"
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetVisitorCreatesNewLimiter(t *testing.T) {
	mu.Lock()
	visitors = make(map[string]*visitor)
	mu.Unlock()

	limiter := getVisitor("1.2.3.4", 10, 20)
	assert.NotNil(t, limiter)

	// Same IP should return the same limiter
	limiter2 := getVisitor("1.2.3.4", 10, 20)
	assert.Equal(t, limiter, limiter2)
}

func TestCleanupVisitors(t *testing.T) {
	mu.Lock()
	visitors = make(map[string]*visitor)
	mu.Unlock()

	// Create a visitor
	getVisitor("5.6.7.8", 10, 20)

	mu.Lock()
	assert.Contains(t, visitors, "5.6.7.8")
	mu.Unlock()

	// Visitor should still be there after cleanup (recently seen)
	cleanupVisitors()

	mu.Lock()
	assert.Contains(t, visitors, "5.6.7.8")
	mu.Unlock()
}
