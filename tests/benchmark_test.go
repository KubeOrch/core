package tests

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KubeOrch/core/middleware"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func setupBenchmarkRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.MetricsMiddleware())

	r.GET("/v1/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Hello KubeOrchestra!",
			"status":  "success",
		})
	})
	return r
}

func BenchmarkHelloEndpoint(b *testing.B) {
	router := setupBenchmarkRouter()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/v1/", nil)
			router.ServeHTTP(w, req)
		}
	})
}

func BenchmarkHelloEndpointSerial(b *testing.B) {
	router := setupBenchmarkRouter()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/v1/", nil)
		router.ServeHTTP(w, req)
	}
}

func BenchmarkMetricsMiddleware(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(middleware.MetricsMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)
	}
}

func BenchmarkRateLimiterMiddleware(b *testing.B) {
	viper.Set("RATE_LIMIT_RPS", 1000000)
	viper.Set("RATE_LIMIT_BURST", 1000000)
	defer viper.Reset()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(middleware.RateLimiterMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		r.ServeHTTP(w, req)
	}
}

func BenchmarkFullMiddlewareStack(b *testing.B) {
	viper.Set("RATE_LIMIT_RPS", 1000000)
	viper.Set("RATE_LIMIT_BURST", 1000000)
	defer viper.Reset()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.MetricsMiddleware())
	r.Use(middleware.RateLimiterMiddleware())
	r.GET("/v1/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/v1/", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			r.ServeHTTP(w, req)
		}
	})
}
