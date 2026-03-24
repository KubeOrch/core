package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	visitors = make(map[string]*visitor)
	mu       sync.Mutex
)

func init() {
	// Clean up stale visitors every minute
	go func() {
		for {
			time.Sleep(time.Minute)
			cleanupVisitors()
		}
	}()
}

func cleanupVisitors() {
	mu.Lock()
	defer mu.Unlock()
	for ip, v := range visitors {
		if time.Since(v.lastSeen) > 3*time.Minute {
			delete(visitors, ip)
		}
	}
}

func getVisitor(ip string, rps float64, burst int) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()

	v, exists := visitors[ip]
	if !exists {
		limiter := rate.NewLimiter(rate.Limit(rps), burst)
		visitors[ip] = &visitor{limiter: limiter, lastSeen: time.Now()}
		return limiter
	}
	v.lastSeen = time.Now()
	return v.limiter
}

// RateLimiterMiddleware limits requests per IP using a token bucket.
// Configure via RATE_LIMIT_RPS (requests per second, default 10)
// and RATE_LIMIT_BURST (burst size, default 20).
func RateLimiterMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rps := viper.GetFloat64("RATE_LIMIT_RPS")
		if rps <= 0 {
			rps = 10
		}
		burst := viper.GetInt("RATE_LIMIT_BURST")
		if burst <= 0 {
			burst = 20
		}

		ip := c.ClientIP()
		limiter := getVisitor(ip, rps, burst)

		if !limiter.Allow() {
			c.Header("Retry-After", "1")
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded, please try again later",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
